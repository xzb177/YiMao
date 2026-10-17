package services

import (
	"crypto/sha1"
	"database/sql"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/xzb177/yimao/pkg/logger"

	_ "modernc.org/sqlite"
)

// 本文件实现 Batch B #6「求片许愿池」的核心存储与状态机（SQLite）。
//
// 设计严格照 docs/BATCH_B_DESIGN.md「#6」+「附录 v2 #6 状态机落地」：
//   - 存储用 SQLite（复用 search_history_db.go 的 modernc.org/sqlite open/建表/查询风格）。
//   - 唯一索引 canonical key = (tmdb_id, media_type, season)，tmdb_id 优先，无 tmdb 用 imdb，都无拒绝入池。
//   - 状态机：PENDING/WISHED → SEARCHING → FOUND → NOTIFIED → FULFILLED；旁路 EXPIRED / ORPHANED。
//   - **所有状态跃迁都包在同一 SQL 事务里**（同时写 state + searching_at/notified_at），避免并发重复推。
//   - searching_at 锁 + 自愈（坑6）：调度只选 searching_at IS NULL OR searching_at<now-interval。
//   - 全部阈值走 config（禁 magic number），调度/通知等副作用由 WishScheduler/handler 负责，本服务只管数据。
//
// 结构化日志统一前缀 [wish]。

// 许愿池状态常量（与 docs 状态机一致）。
const (
	WishStatePending   = "PENDING"   // 入池初始态（= WISHED，等待进入调度）
	WishStateSearching = "SEARCHING" // 已纳入重搜调度
	WishStateFound     = "FOUND"     // 重搜命中、待通知
	WishStateNotified  = "NOTIFIED"  // 已推送出源喜报、等用户点「立即求片」
	WishStateFulfilled = "FULFILLED" // 用户已确认求片
	WishStateExpired   = "EXPIRED"   // 超期无源 / 手动放弃（旁路）
	WishStateOrphaned  = "ORPHANED"  // 用户退群/不可达（旁路，不通知不重试）
)

// 容量上限常量（防重搜任务无限膨胀，照 docs「每人≤20、全局≤500」）。
const (
	WishMaxPerUser = 20
	WishMaxGlobal  = 500
)

// WishItem 对应 wish_items 表一行。
type WishItem struct {
	ID          int64
	UserID      int64
	TmdbID      int
	ImdbID      string
	MediaType   string // "movie" / "tv"
	Title       string
	Year        int
	Season      int
	State       string
	FoundDetail string     // 命中详情（站点/标题/做种数/质量标注），通知时展示
	SearchingAt *time.Time // 重搜锁时间戳，NULL 表示未锁
	NotifiedAt  *time.Time // 通知时间戳，用于 TTL 倒计时
	// SearchOffsetMinute 错峰偏移分钟（坑2）：入池时定死 = hash(canonical key)%1440。
	// 调度只在「当前分钟 == 本列」时认领，全天散布、全量覆盖、无饿死。
	SearchOffsetMinute int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// WishService 管理许愿池数据（SQLite 后端）。
type WishService struct {
	db *sql.DB
}

// NewWishService 创建许愿池服务并建表。失败返回 error，调用方据此降级（不接入半成品）。
func NewWishService(dataDir string) (*WishService, error) {
	dbPath := fmt.Sprintf("%s/wishpool.db", dataDir)

	// WAL 模式：允许读写并发，大幅减少锁表概率。
	// busy_timeout=5000：写入锁定时最多等 5 秒再报错，而不是立刻死掉。
	// _txlock=immediate：审计第 15 条 —— DEFERRED 事务在 WAL 下从读升级到写会
	// 触发 BUSY_SNAPSHOT（忙等待救不了，立即失败）；IMMEDIATE 让事务一开始就拿
	// 写锁，配合 busy_timeout 排队等待，根治并发「database is locked」。
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open wishpool database: %w", err)
	}

	// 建表 + 唯一索引（canonical key）。
	// 唯一索引说明：tmdb_id 优先；tmdb_id=0（仅 imdb）的条目用 imdb_id 区分。
	// 这里建两个部分唯一索引分别覆盖「有 tmdb」与「无 tmdb 仅 imdb」两种情况，
	// 避免 tmdb_id=0 的多条 imdb 条目互相撞唯一键。
	_, err = db.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS wish_items (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id      INTEGER NOT NULL,
			tmdb_id      INTEGER NOT NULL DEFAULT 0,
			imdb_id      TEXT    NOT NULL DEFAULT '',
			media_type   TEXT    NOT NULL DEFAULT 'movie',
			title        TEXT    NOT NULL DEFAULT '',
			year         INTEGER NOT NULL DEFAULT 0,
			season       INTEGER NOT NULL DEFAULT 0,
			state        TEXT    NOT NULL DEFAULT 'PENDING',
			found_detail TEXT    NOT NULL DEFAULT '',
			searching_at DATETIME,
			notified_at  DATETIME,
			search_offset_minute INTEGER NOT NULL DEFAULT 0,
			created_at   DATETIME NOT NULL,
			updated_at   DATETIME NOT NULL
		);

		%s

		CREATE INDEX IF NOT EXISTS idx_wish_state ON wish_items(state);
		CREATE INDEX IF NOT EXISTS idx_wish_user ON wish_items(user_id);
	`, activeUniqueIndexSQL()))
	if err != nil {
		return nil, fmt.Errorf("failed to create wish_items table: %w", err)
	}

	// 审计第 13 条迁移：旧库的唯一索引是全量的（终态行也占键），先删掉再建
	// 状态条件的。旧库里同一 canonical 不可能有多行（旧全量索引保证过），
	// 所以新部分索引（更弱的约束）建起来不会炸。
	if err := ensureStateAwareUniqueIndex(db); err != nil {
		return nil, fmt.Errorf("failed to migrate state-aware unique index: %w", err)
	}

	// 向后兼容迁移（坑2 错峰列）：旧库 wish_items 没有 search_offset_minute 列，
	// 这里 ALTER TABLE ADD COLUMN 补上（SQLite 不支持 IF NOT EXISTS，靠探测列是否存在防重复/防崩）。
	if err := ensureSearchOffsetColumn(db); err != nil {
		return nil, fmt.Errorf("failed to migrate search_offset_minute: %w", err)
	}

	// 错峰列调度索引：按 (state, search_offset_minute) 命中本分钟该搜的那批。
	if _, err := db.Exec(
		`CREATE INDEX IF NOT EXISTS idx_wish_offset ON wish_items(state, search_offset_minute);`,
	); err != nil {
		return nil, fmt.Errorf("failed to create idx_wish_offset: %w", err)
	}

	// 元数据表（持久化调度状态，如上次过期扫描时间，用于重启 catch-up / 补跑漏扫）。
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS wish_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`); err != nil {
		return nil, fmt.Errorf("failed to create wish_meta table: %w", err)
	}

	// 「许愿众筹计数」join 表（wish_wishers）：记录每个 canonical 维度有哪些 user 在等。
	// wish_items 仍是「每个 canonical 全局一条」，本表不影响其状态机/重搜/调度；
	// 仅用于「累计等待人数」与「出源时通知所有 wisher」。
	//   - PRIMARY KEY(canonical_key, user_id) 即唯一约束，天然去重幂等（INSERT OR IGNORE）。
	//   - canonical_key 必须经 CanonicalKey 生成，与 wish_items 去重键完全对齐。
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS wish_wishers (
			canonical_key TEXT    NOT NULL,
			user_id       INTEGER NOT NULL,
			created_at    DATETIME NOT NULL,
			PRIMARY KEY (canonical_key, user_id)
		);
		CREATE INDEX IF NOT EXISTS idx_wish_wishers_key ON wish_wishers(canonical_key);`); err != nil {
		return nil, fmt.Errorf("failed to create wish_wishers table: %w", err)
	}

	svc := &WishService{db: db}

	// 向后兼容回填：把存量 wish_items 每条的 (canonical_key, user_id, created_at)
	// INSERT OR IGNORE 进 wish_wishers，保证每片至少 1 个 wisher（= 原发起人）。
	// INSERT OR IGNORE 保证幂等：重启多次不重复污染。
	if err := svc.backfillWishers(); err != nil {
		return nil, fmt.Errorf("failed to backfill wish_wishers: %w", err)
	}

	logger.Info("[wish] 许愿池数据库已初始化: %s", dbPath)
	return svc, nil
}

// activeUniqueIndexSQL 生成「只约束活跃状态」的部分唯一索引语句。
// 活跃状态 = PENDING / SEARCHING / FOUND / NOTIFIED；
// 终态（FULFILLED / EXPIRED / ORPHANED）的行不进索引，可重新许愿。
func activeUniqueIndexSQL() string {
	states := strings.Join([]string{
		"'" + WishStatePending + "'",
		"'" + WishStateSearching + "'",
		"'" + WishStateFound + "'",
		"'" + WishStateNotified + "'",
	}, ", ")
	return fmt.Sprintf(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_wish_canonical_tmdb
			ON wish_items(tmdb_id, media_type, season)
			WHERE tmdb_id != 0 AND state IN (%s);

		CREATE UNIQUE INDEX IF NOT EXISTS idx_wish_canonical_imdb
			ON wish_items(imdb_id, media_type, season)
			WHERE tmdb_id = 0 AND imdb_id != '' AND state IN (%s);
	`, states, states)
}

// ensureStateAwareUniqueIndex 把旧库的全量唯一索引替换成状态条件的。
// 旧库同一 canonical 至多一行（旧全量索引保证），所以先 DROP 再建部分索引
// 绝不会因为存量重复行炸掉；幂等，可重复执行。
func ensureStateAwareUniqueIndex(db *sql.DB) error {
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_wish_canonical_tmdb`); err != nil {
		return err
	}
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_wish_canonical_imdb`); err != nil {
		return err
	}
	if _, err := db.Exec(activeUniqueIndexSQL()); err != nil {
		return err
	}
	logger.Info("[wish] 已迁移：唯一索引改为只约束活跃状态（终态可重许）")
	return nil
}

// metaKeyLastExpirySweep 是 wish_meta 表里记录「上次过期扫描完成时刻」的 key。
const metaKeyLastExpirySweep = "last_expiry_sweep_at"

// GetLastExpirySweep 读取上次过期扫描完成时刻。无记录返回 (零值, false, nil)。
func (s *WishService) GetLastExpirySweep() (time.Time, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM wish_meta WHERE key = ?`, metaKeyLastExpirySweep).Scan(&v)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, perr := time.Parse(time.RFC3339, v)
	if perr != nil {
		// 解析失败按「无记录」处理，触发一次补跑而非崩溃。
		return time.Time{}, false, nil
	}
	return t, true, nil
}

// SetLastExpirySweep 持久化本次过期扫描完成时刻（UPSERT）。
func (s *WishService) SetLastExpirySweep(t time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO wish_meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		metaKeyLastExpirySweep, t.UTC().Format(time.RFC3339))
	return err
}

// ensureSearchOffsetColumn 探测并补齐 search_offset_minute 列（向后兼容旧库）。
// 已有列则跳过；缺列则 ALTER TABLE 添加，并把存量行的偏移按 id 回填一遍，避免旧条目全挤在 0 分钟。
func ensureSearchOffsetColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(wish_items)`)
	if err != nil {
		return err
	}
	has := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "search_offset_minute" {
			has = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if has {
		return nil
	}

	if _, err := db.Exec(
		`ALTER TABLE wish_items ADD COLUMN search_offset_minute INTEGER NOT NULL DEFAULT 0`,
	); err != nil {
		return err
	}
	// 回填存量行：按 id 算确定性偏移（与新插入逻辑一致），让旧条目错峰散布。
	if _, err := db.Exec(
		`UPDATE wish_items SET search_offset_minute = (
			(id * 8761 + 619) % 1440
		)`,
	); err != nil {
		return err
	}
	logger.Info("[wish] 已迁移：补充 search_offset_minute 列并回填存量错峰偏移")
	return nil
}

// Close 关闭数据库连接。
func (s *WishService) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// ---------------------------------------------------------------------------
// 入池
// ---------------------------------------------------------------------------

// AddWishResult 表示入池尝试的结果，供 handler 生成对应文案。
type AddWishResult struct {
	Created     bool      // 是否新建成功
	Duplicate   bool      // 池内已存在同 canonical key（坑7）
	Existing    *WishItem // Duplicate=true 时返回已有条目
	OverPerUser bool      // 超过每人上限
	OverGlobal  bool      // 超过全局上限
}

// canonicalCheck 校验 canonical key 是否合法（坑1/坑7）：tmdb 优先，无则 imdb，都无返回 false。
func canonicalCheck(tmdbID int, imdbID string) bool {
	return tmdbID != 0 || strings.TrimSpace(imdbID) != ""
}

// AddWish 尝试把一条心愿入池。
// 调用方（handler）须在调用前完成：TMDB 校验（tmdb_id!=0，坑1）+ FindExistingSubscription 去重（坑7）。
// 本方法只负责池内去重 + 容量上限 + 落库（PENDING）。所有写在一个事务里。
func (s *WishService) AddWish(item *WishItem) (*AddWishResult, error) {
	if !canonicalCheck(item.TmdbID, item.ImdbID) {
		// 防御：没有 canonical key 一律拒绝（去重/重搜都依赖 id）。
		return nil, fmt.Errorf("missing canonical key (tmdb_id/imdb_id)")
	}
	if item.MediaType == "" {
		item.MediaType = "movie"
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// 1) 池内去重（canonical key）。命中活跃条目则视为重复（坑7）。
	//    已 EXPIRED/ORPHANED/FULFILLED 的旧条目不算活跃，允许重新入池。
	existing, err := s.findActiveByCanonicalTx(tx, item.TmdbID, item.ImdbID, item.MediaType, item.Season)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// 众筹计数关键语义：重复许愿同一片时，仍把该 user 记进 wish_wishers（同事务），
		// 这样后来者也能累计进等待人数，再由 handler 给「N 人在等」提示。
		ck := CanonicalKey(item.TmdbID, item.ImdbID, item.MediaType, item.Season)
		if err := addWisherTx(tx, ck, item.UserID, time.Now()); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		committed = true
		return &AddWishResult{Duplicate: true, Existing: existing}, nil
	}

	// 2) 容量上限校验。
	var globalActive int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM wish_items WHERE state IN (?,?,?,?)`,
		WishStatePending, WishStateSearching, WishStateFound, WishStateNotified,
	).Scan(&globalActive); err != nil {
		return nil, err
	}
	if globalActive >= WishMaxGlobal {
		return &AddWishResult{OverGlobal: true}, nil
	}

	var userActive int
	if err := tx.QueryRow(
		`SELECT COUNT(DISTINCT w.canonical_key)
		 FROM wish_wishers w
		 JOIN wish_items i ON i.tmdb_id != 0 AND w.canonical_key = 'tmdb-' || i.tmdb_id || '-' || i.media_type || '-' || i.season
		 WHERE w.user_id = ? AND i.state IN (?,?,?,?)`,
		item.UserID, WishStatePending, WishStateSearching, WishStateFound, WishStateNotified,
	).Scan(&userActive); err != nil {
		return nil, err
	}
	if userActive >= WishMaxPerUser {
		return &AddWishResult{OverPerUser: true}, nil
	}

	// 3) 落库（PENDING）。错峰偏移（坑2）入池时定死，调度据此 SQL 直接过滤本分钟该搜的批次。
	now := time.Now()
	offsetMinute := searchOffsetForCanonical(item.TmdbID, item.ImdbID, item.MediaType, item.Season)
	res, err := tx.Exec(`
		INSERT INTO wish_items
			(user_id, tmdb_id, imdb_id, media_type, title, year, season, state, found_detail, search_offset_minute, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?)
	`, item.UserID, item.TmdbID, item.ImdbID, item.MediaType, item.Title, item.Year, item.Season,
		WishStatePending, offsetMinute, now, now)
	if err != nil {
		// 唯一键冲突等并发情况：当作重复处理（兜底，正常已被 step1 拦下）。
		return nil, fmt.Errorf("failed to insert wish item: %w", err)
	}
	id, _ := res.LastInsertId()

	// 众筹计数：新建 wish_items 的同时把发起人记进 wish_wishers（同事务，原子）。
	ck := CanonicalKey(item.TmdbID, item.ImdbID, item.MediaType, item.Season)
	if err := addWisherTx(tx, ck, item.UserID, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	item.ID = id
	item.State = WishStatePending
	item.SearchOffsetMinute = offsetMinute
	item.CreatedAt = now
	item.UpdatedAt = now
	logger.Info("[wish] 入池成功 id=%d user=%d tmdb=%d imdb=%s type=%s season=%d title=%q",
		id, item.UserID, item.TmdbID, item.ImdbID, item.MediaType, item.Season, item.Title)
	return &AddWishResult{Created: true}, nil
}

// findActiveByCanonicalTx 在事务内按 canonical key 查找活跃条目。
func (s *WishService) findActiveByCanonicalTx(tx *sql.Tx, tmdbID int, imdbID, mediaType string, season int) (*WishItem, error) {
	var row *sql.Row
	if tmdbID != 0 {
		row = tx.QueryRow(`
			SELECT id, user_id, tmdb_id, imdb_id, media_type, title, year, season, state, found_detail,
			       searching_at, notified_at, search_offset_minute, created_at, updated_at
			FROM wish_items
			WHERE tmdb_id = ? AND media_type = ? AND season = ? AND state IN (?,?,?,?)
			LIMIT 1`,
			tmdbID, mediaType, season,
			WishStatePending, WishStateSearching, WishStateFound, WishStateNotified)
	} else {
		row = tx.QueryRow(`
			SELECT id, user_id, tmdb_id, imdb_id, media_type, title, year, season, state, found_detail,
			       searching_at, notified_at, search_offset_minute, created_at, updated_at
			FROM wish_items
			WHERE tmdb_id = 0 AND imdb_id = ? AND media_type = ? AND season = ? AND state IN (?,?,?,?)
			LIMIT 1`,
			imdbID, mediaType, season,
			WishStatePending, WishStateSearching, WishStateFound, WishStateNotified)
	}
	item, err := scanWishRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

// ---------------------------------------------------------------------------
// 许愿众筹计数（wish_wishers）：记 wisher / 计数 / 列举 / 回填迁移
// ---------------------------------------------------------------------------

// AddWisher 把一个 user 记入某 canonical 的 wisher 集合（幂等：INSERT OR IGNORE）。
// 无论入池是「新建 wish_items」还是「命中已存在 canonical（原 Duplicate）」都应调用，
// 这样重复许愿同一片的后来者也能累计进等待人数，而不会因 wish_items 去重被丢弃。
// canonicalKey 为空（无 id）时直接跳过（与 AddWish 的 canonical 校验对齐）。
func (s *WishService) AddWisher(canonicalKey string, userID int64) error {
	canonicalKey = strings.TrimSpace(canonicalKey)
	if canonicalKey == "" || userID == 0 {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO wish_wishers (canonical_key, user_id, created_at) VALUES (?, ?, ?)`,
		canonicalKey, userID, time.Now())
	return err
}

// addWisherTx 在事务内记 wisher（供 AddWish 与落库同事务，保证「入池即记 wisher」原子性）。
func addWisherTx(tx *sql.Tx, canonicalKey string, userID int64, when time.Time) error {
	canonicalKey = strings.TrimSpace(canonicalKey)
	if canonicalKey == "" || userID == 0 {
		return nil
	}
	_, err := tx.Exec(
		`INSERT OR IGNORE INTO wish_wishers (canonical_key, user_id, created_at) VALUES (?, ?, ?)`,
		canonicalKey, userID, when)
	return err
}

// CountWishers 返回某 canonical 的等待人数（总数，只升不降——退群也算）。
func (s *WishService) CountWishers(canonicalKey string) int {
	canonicalKey = strings.TrimSpace(canonicalKey)
	if canonicalKey == "" {
		return 0
	}
	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM wish_wishers WHERE canonical_key = ?`, canonicalKey,
	).Scan(&n); err != nil {
		logger.Info("[wish] CountWishers 查询失败 key=%s: %v", canonicalKey, err)
		return 0
	}
	return n
}

// ListWishers 返回某 canonical 的所有 wisher user_id（按入等先后）。
// 供出源通知遍历做 @/PM（可达过滤由调用方负责，本方法返回全量，不剔退群者）。
func (s *WishService) ListWishers(canonicalKey string) []int64 {
	canonicalKey = strings.TrimSpace(canonicalKey)
	if canonicalKey == "" {
		return nil
	}
	rows, err := s.db.Query(
		`SELECT user_id FROM wish_wishers WHERE canonical_key = ? ORDER BY created_at ASC, user_id ASC`,
		canonicalKey)
	if err != nil {
		logger.Info("[wish] ListWishers 查询失败 key=%s: %v", canonicalKey, err)
		return nil
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			logger.Info("[wish] ListWishers 扫描失败 key=%s: %v", canonicalKey, err)
			return ids
		}
		ids = append(ids, uid)
	}
	return ids
}

// backfillWishers 向后兼容回填：存量 wish_items 每条的 (canonical_key, user_id, created_at)
// INSERT OR IGNORE 进 wish_wishers，保证每片至少 1 个 wisher（= 原发起人）。
// 用 SQL 表达式直接派生 canonical_key，与 CanonicalKey 的格式严格一致：
//   - tmdb_id != 0 → 'tmdb-<tmdb_id>-<media_type>-<season>'
//   - 否则           → 'imdb-<imdb_id>-<media_type>-<season>'
//
// media_type 空串归一为 'movie'（与 CanonicalKey 一致）。
// 仅回填有 canonical key 的行（tmdb_id!=0 或 imdb_id!=”），无 id 的脏行跳过。
// INSERT OR IGNORE + 确定性派生 → 多次启动幂等、不重复污染。
func (s *WishService) backfillWishers() error {
	res, err := s.db.Exec(`
		INSERT OR IGNORE INTO wish_wishers (canonical_key, user_id, created_at)
		SELECT
			CASE WHEN tmdb_id != 0
				THEN 'tmdb-' || tmdb_id || '-' || (CASE WHEN media_type = '' THEN 'movie' ELSE media_type END) || '-' || season
				ELSE 'imdb-' || imdb_id || '-' || (CASE WHEN media_type = '' THEN 'movie' ELSE media_type END) || '-' || season
			END AS canonical_key,
			user_id,
			created_at
		FROM wish_items
		WHERE tmdb_id != 0 OR imdb_id != ''`)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		logger.Info("[wish] 已回填 %d 条存量许愿发起人进 wish_wishers", n)
	}
	return nil
}

// SearchOffsetMinutes 按 hash(item_id)%1440 算错峰偏移（坑2）。导出供调度与测试使用。
// 注意：入池时 id 尚未生成（AUTOINCREMENT），故入池偏移用 canonical key 计算
// （见 searchOffsetForCanonical）；本函数保留用于已知 id 的兼容/测试场景。
func SearchOffsetMinutes(itemID int64) int {
	h := sha1.Sum([]byte(fmt.Sprintf("wish-%d", itemID)))
	v := binary.BigEndian.Uint64(h[:8])
	return int(v % 1440)
}

// CanonicalKey 生成许愿池的「规范键」——与 wish_items 去重所用的唯一键完全对齐：
// tmdb_id 优先，无则用 imdb_id，并带上 media_type / season。
//
// **唯一权威生成函数**：池内去重（idx_wish_canonical_*）、错峰偏移
// （searchOffsetForCanonical）、群公示去重（wishAnnounceKey）、以及新增的
// wish_wishers 计数表，全部经此函数派生 canonical 维度，避免「计数和去重对不上」。
//
// 注意：调用方须保证至少有一个 id（tmdbID!=0 或 imdbID 非空），否则键不具区分性。
// 与 findActiveByCanonicalTx 的 WHERE 子句一致：tmdb 优先用 tmdb 维度，否则 imdb 维度。
// media_type 空串按 "movie" 归一（与 AddWish 入池前归一逻辑一致）。
func CanonicalKey(tmdbID int, imdbID, mediaType string, season int) string {
	if mediaType == "" {
		mediaType = "movie"
	}
	if tmdbID != 0 {
		return fmt.Sprintf("tmdb-%d-%s-%d", tmdbID, mediaType, season)
	}
	return fmt.Sprintf("imdb-%s-%s-%d", strings.TrimSpace(imdbID), mediaType, season)
}

// canonicalKeyOf 是 *WishItem 的便捷封装，复用同一权威生成函数。
func canonicalKeyOf(item *WishItem) string {
	if item == nil {
		return ""
	}
	return CanonicalKey(item.TmdbID, item.ImdbID, item.MediaType, item.Season)
}

// searchOffsetForCanonical 用 canonical key（tmdb 优先，否则 imdb）算入池时的错峰偏移。
// canonical key 在入池前已确定且稳定，避免依赖尚未生成的自增 id。
// 复用 CanonicalKey 派生（前缀 "wish-" 仅为与历史哈希输入保持兼容、不改变既有错峰分布）。
func searchOffsetForCanonical(tmdbID int, imdbID, mediaType string, season int) int {
	key := "wish-" + CanonicalKey(tmdbID, imdbID, mediaType, season)
	h := sha1.Sum([]byte(key))
	v := binary.BigEndian.Uint64(h[:8])
	return int(v % 1440)
}

// ClaimSearchableItems 原子地「认领」当前分钟该搜、且 searching_at 自愈锁可用的条目。
// 它在一个事务里：用 SQL 直接过滤 → 立刻 UPDATE searching_at=now()（上锁）→ 返回被认领条目。
//
// 关键设计（修复 B1 饿死 + churn）：
//   - 错峰（坑2）直接在 SQL WHERE search_offset_minute = ? 过滤，每分钟只认领真正该搜的那批，
//     全天 1440 个分片天然轮转、全量覆盖、永不饿死，也不再 claim-then-release 空写。
//   - 自愈锁（坑6 / 坑B）：lockTTLMinutes 是独立的短 TTL（默认 60min），
//     与重搜周期（24h）解耦——崩溃残留旧锁超过该 TTL 即可重纳入，盲区从 ~24h 缩到 ~1h。
//   - 每片每天仅一次：search_offset_minute 在 1440 分钟里唯一命中本分钟，
//     天然保证「每个分片每天只被认领一次」，无需再叠加 24h 周期条件。
//
// 入参：
//   - nowMinuteOfDay: 当前时刻的「日内分钟」= hour*60+minute，范围 [0,1439]。
//   - lockTTLMinutes: 自愈锁 TTL（分钟），<=0 时回退 60。
//   - limit:          单次认领上限（防一次取过多）；同一分钟分片通常远小于该值。
func (s *WishService) ClaimSearchableItems(nowMinuteOfDay int, lockTTLMinutes int, limit int) ([]*WishItem, error) {
	if lockTTLMinutes <= 0 {
		lockTTLMinutes = 60
	}
	if limit <= 0 {
		limit = 200
	}
	if nowMinuteOfDay < 0 {
		nowMinuteOfDay = 0
	}
	nowMinuteOfDay = nowMinuteOfDay % 1440
	lockCutoff := time.Now().Add(-time.Duration(lockTTLMinutes) * time.Minute)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// SQL 同时做：错峰分片过滤 + 自愈锁过期过滤；PENDING 也纳入（首次入调度即 WISHED→SEARCHING）。
	// ORDER BY id 仅为稳定性，LIMIT 在已正确过滤的小集合上截断，不会造成饿死。
	rows, err := tx.Query(`
		SELECT id, user_id, tmdb_id, imdb_id, media_type, title, year, season, state, found_detail,
		       searching_at, notified_at, search_offset_minute, created_at, updated_at
		FROM wish_items
		WHERE state IN (?, ?)
		  AND search_offset_minute = ?
		  AND (searching_at IS NULL OR searching_at < ?)
		ORDER BY id ASC
		LIMIT ?`,
		WishStatePending, WishStateSearching, nowMinuteOfDay, lockCutoff, limit)
	if err != nil {
		return nil, err
	}

	var claimed []*WishItem
	for rows.Next() {
		item, scanErr := scanWishRowsCursor(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		claimed = append(claimed, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	now := time.Now()
	for _, it := range claimed {
		// 上锁 + PENDING→SEARCHING（同一事务）。
		if _, err := tx.Exec(
			`UPDATE wish_items SET searching_at = ?, state = ?, updated_at = ? WHERE id = ?`,
			now, WishStateSearching, now, it.ID,
		); err != nil {
			return nil, err
		}
		it.SearchingAt = &now
		it.State = WishStateSearching
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return claimed, nil
}

// ReleaseSearchLock 搜完未命中：清空 searching_at（解锁），状态保持 SEARCHING 等下个窗口。
func (s *WishService) ReleaseSearchLock(id int64) error {
	now := time.Now()
	_, err := s.db.Exec(
		`UPDATE wish_items SET searching_at = NULL, updated_at = ? WHERE id = ? AND state = ?`,
		now, id, WishStateSearching)
	return err
}

// ---------------------------------------------------------------------------
// 状态跃迁（全部单事务，写 state + 时间戳；带前置状态校验防并发重复推）
// ---------------------------------------------------------------------------

// MarkFound SEARCHING→FOUND：记录命中详情、清搜索锁。
// 用 WHERE state=SEARCHING 做乐观并发控制：若已被其它 goroutine 改走则本次无效（rowsAffected=0）。
// 返回 changed=true 表示本次成功跃迁（仅此时才应触发通知）。
func (s *WishService) MarkFound(id int64, foundDetail string) (bool, error) {
	now := time.Now()
	res, err := s.db.Exec(
		`UPDATE wish_items SET state = ?, found_detail = ?, searching_at = NULL, updated_at = ?
		 WHERE id = ? AND state = ?`,
		WishStateFound, foundDetail, now, id, WishStateSearching)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		logger.Info("[wish] id=%d 重搜命中 → FOUND: %s", id, foundDetail)
	}
	return n > 0, nil
}

// MarkNotified FOUND→NOTIFIED：标记已通知、写 notified_at 启动 TTL。
// 同样用前置状态校验防止重复推送。
func (s *WishService) MarkNotified(id int64) (bool, error) {
	now := time.Now()
	res, err := s.db.Exec(
		`UPDATE wish_items SET state = ?, notified_at = ?, updated_at = ?
		 WHERE id = ? AND state = ?`,
		WishStateNotified, now, now, id, WishStateFound)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkFulfilled NOTIFIED→FULFILLED：用户点「立即求片」并走完求片流程后调用。
func (s *WishService) MarkFulfilled(id int64) (bool, error) {
	now := time.Now()
	res, err := s.db.Exec(
		`UPDATE wish_items SET state = ?, searching_at = NULL, updated_at = ?
		 WHERE id = ? AND state IN (?, ?)`,
		WishStateFulfilled, now, id, WishStateNotified, WishStateFound)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		logger.Info("[wish] id=%d → FULFILLED（用户已确认求片）", id)
	}
	return n > 0, nil
}

// MarkOrphaned *→ORPHANED：用户退群/不可达（坑5）。不通知不重试。
func (s *WishService) MarkOrphaned(id int64) error {
	now := time.Now()
	_, err := s.db.Exec(
		`UPDATE wish_items SET state = ?, searching_at = NULL, updated_at = ? WHERE id = ?`,
		WishStateOrphaned, now, id)
	if err == nil {
		logger.Info("[wish] id=%d → ORPHANED（用户不可达）", id)
	}
	return err
}

// MarkExpired *→EXPIRED：超期无源 / 手动放弃（坑4）。
func (s *WishService) MarkExpired(id int64) error {
	now := time.Now()
	_, err := s.db.Exec(
		`UPDATE wish_items SET state = ?, searching_at = NULL, updated_at = ? WHERE id = ?`,
		WishStateExpired, now, id)
	if err == nil {
		logger.Info("[wish] id=%d → EXPIRED", id)
	}
	return err
}

// ---------------------------------------------------------------------------
// 审计第 14 条：状态机死状态修复
// ---------------------------------------------------------------------------

// ListStaleFoundItems 列出卡在 FOUND 且上次尝试已超过 retryAfter 的条目，
// 供调度重试通知。updated_at 在每次通知尝试后刷新（成功走 MarkNotified，
// 失败走 TouchUpdatedAt），天然节流。
func (s *WishService) ListStaleFoundItems(retryAfter time.Duration) ([]*WishItem, error) {
	cutoff := time.Now().Add(-retryAfter)
	rows, err := s.db.Query(`
		SELECT id, user_id, tmdb_id, imdb_id, media_type, title, year, season, state, found_detail,
		       searching_at, notified_at, search_offset_minute, created_at, updated_at
		FROM wish_items
		WHERE state = ? AND updated_at < ?
		ORDER BY updated_at ASC`,
		WishStateFound, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*WishItem
	for rows.Next() {
		it, err := scanWishRowsCursor(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// TouchUpdatedAt 刷新某条目的 updated_at（通知失败节流戳），状态不变。
func (s *WishService) TouchUpdatedAt(id int64) error {
	_, err := s.db.Exec(`UPDATE wish_items SET updated_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

// ExpireStaleNotified 把 NOTIFIED 超过 ttl 仍无操作（用户一直没点「立即求片」）
// 的条目置 EXPIRED，释放名额。返回过期条数。
func (s *WishService) ExpireStaleNotified(ttl time.Duration) (int64, error) {
	cutoff := time.Now().Add(-ttl)
	now := time.Now()
	res, err := s.db.Exec(
		`UPDATE wish_items SET state = ?, searching_at = NULL, updated_at = ?
		 WHERE state = ? AND notified_at < ?`,
		WishStateExpired, now, WishStateNotified, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		logger.Info("[wish] NOTIFIED 超时清理：%d 条已置 EXPIRED", n)
	}
	return n, nil
}

// RevokeOutcome 表示 RevokeWish 的结果。
type RevokeOutcome int

const (
	// RevokeSelfWithdrawn 跟随者退出自己的份，单子保留。
	RevokeSelfWithdrawn RevokeOutcome = iota + 1
	// RevokeTransferred 发起人退出，发起人身份转给最早的跟随者，单子保留。
	RevokeTransferred
	// RevokeCancelled 最后一人退出，整单 EXPIRED。
	RevokeCancelled
)

// IsWisher 检查某用户是否是某 canonical 的 wisher。
func (s *WishService) IsWisher(userID int64, canonicalKey string) bool {
	canonicalKey = strings.TrimSpace(canonicalKey)
	if canonicalKey == "" || userID == 0 {
		return false
	}
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM wish_wishers WHERE canonical_key = ? AND user_id = ? LIMIT 1`,
		canonicalKey, userID).Scan(&one)
	return err == nil
}

// RevokeWish 撤回许愿 —— 只撤自己，不连带作废他人。
//   - 跟随者撤回：只删自己的 wisher 行，单子保留（发起人/状态不变）。
//   - 发起人撤回且还有其他 wisher：发起人身份转给最早的跟随者，单子保留。
//   - 最后一人撤回：整单置 EXPIRED。
//
// 调用方须保证条目活跃；非 wisher 调用返回错误。全部单事务。
func (s *WishService) RevokeWish(userID, wishID int64) (RevokeOutcome, error) {
	if userID == 0 || wishID == 0 {
		return 0, fmt.Errorf("invalid user or wish id")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// 1) 取条目（须活跃）。
	row := tx.QueryRow(`
		SELECT id, user_id, tmdb_id, imdb_id, media_type, title, year, season, state, found_detail,
		       searching_at, notified_at, search_offset_minute, created_at, updated_at
		FROM wish_items
		WHERE id = ? AND state IN (?,?,?,?)`,
		wishID, WishStatePending, WishStateSearching, WishStateFound, WishStateNotified)
	item, err := scanWishRow(row)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("wish %d not found or not active", wishID)
	}
	if err != nil {
		return 0, err
	}

	// 2) 验身份：发起人或任一 wisher。
	ck := CanonicalKey(item.TmdbID, item.ImdbID, item.MediaType, item.Season)
	isOriginator := userID == item.UserID
	var one int
	inWishers := tx.QueryRow(
		`SELECT 1 FROM wish_wishers WHERE canonical_key = ? AND user_id = ? LIMIT 1`,
		ck, userID).Scan(&one) == nil
	if !isOriginator && !inWishers {
		return 0, fmt.Errorf("user %d is not a wisher of wish %d", userID, wishID)
	}

	// 3) 删掉自己的 wisher 行。
	if _, err := tx.Exec(
		`DELETE FROM wish_wishers WHERE canonical_key = ? AND user_id = ?`,
		ck, userID); err != nil {
		return 0, err
	}

	now := time.Now()
	if isOriginator {
		// 4) 发起人退出：转给最早的剩余跟随者；没人了则整单过期。
		var next int64
		err := tx.QueryRow(
			`SELECT user_id FROM wish_wishers WHERE canonical_key = ?
			 ORDER BY created_at ASC, user_id ASC LIMIT 1`, ck).Scan(&next)
		if err == sql.ErrNoRows {
			if _, err := tx.Exec(
				`UPDATE wish_items SET state = ?, searching_at = NULL, updated_at = ? WHERE id = ?`,
				WishStateExpired, now, wishID); err != nil {
				return 0, err
			}
			if err := tx.Commit(); err != nil {
				return 0, err
			}
			committed = true
			logger.Info("[wish] id=%d 发起人撤回且无人等待 → EXPIRED", wishID)
			return RevokeCancelled, nil
		}
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(
			`UPDATE wish_items SET user_id = ?, updated_at = ? WHERE id = ?`,
			next, now, wishID); err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		committed = true
		logger.Info("[wish] id=%d 发起人撤回 → 发起人转给 user=%d，单子保留", wishID, next)
		return RevokeTransferred, nil
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	committed = true
	logger.Info("[wish] id=%d 跟随者 user=%d 退出自己的份，单子保留", wishID, userID)
	return RevokeSelfWithdrawn, nil
}

// ---------------------------------------------------------------------------
// 查询
// ---------------------------------------------------------------------------

// GetByID 按 id 取条目。
func (s *WishService) GetByID(id int64) (*WishItem, error) {
	row := s.db.QueryRow(`
		SELECT id, user_id, tmdb_id, imdb_id, media_type, title, year, season, state, found_detail,
		       searching_at, notified_at, search_offset_minute, created_at, updated_at
		FROM wish_items WHERE id = ?`, id)
	item, err := scanWishRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return item, err
}

// ListByUser 列出某用户的活跃心愿（供 /wish 无参时展示「我的许愿」）。
func (s *WishService) ListByUser(userID int64) ([]*WishItem, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, tmdb_id, imdb_id, media_type, title, year, season, state, found_detail,
		       searching_at, notified_at, search_offset_minute, created_at, updated_at
		FROM wish_items
		WHERE user_id = ? AND state IN (?,?,?,?)
		ORDER BY created_at DESC`,
		userID, WishStatePending, WishStateSearching, WishStateFound, WishStateNotified)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*WishItem
	for rows.Next() {
		it, err := scanWishRowsCursor(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// ListForWisher returns every active wish the user joined, including wishes
// originally created by another user.
func (s *WishService) ListForWisher(userID int64) ([]*WishItem, error) {
	rows, err := s.db.Query(`
		SELECT i.id, i.user_id, i.tmdb_id, i.imdb_id, i.media_type, i.title, i.year, i.season, i.state, i.found_detail,
		       i.searching_at, i.notified_at, i.search_offset_minute, i.created_at, i.updated_at
		FROM wish_items i
		JOIN wish_wishers w ON w.canonical_key =
			CASE WHEN i.tmdb_id != 0
				THEN 'tmdb-' || i.tmdb_id || '-' || (CASE WHEN i.media_type = '' THEN 'movie' ELSE i.media_type END) || '-' || i.season
				ELSE 'imdb-' || i.imdb_id || '-' || (CASE WHEN i.media_type = '' THEN 'movie' ELSE i.media_type END) || '-' || i.season
			END
		WHERE w.user_id = ? AND i.state IN (?,?,?,?)
		ORDER BY w.created_at DESC`,
		userID, WishStatePending, WishStateSearching, WishStateFound, WishStateNotified)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*WishItem
	for rows.Next() {
		item, err := scanWishRowsCursor(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListExpiryCandidates 列出超过 expireDays 仍未出源的条目（坑4，供调度做「最终重搜」）。
// 只看 created_at（入池时间），状态须为 PENDING/SEARCHING（已 FOUND/NOTIFIED 的不算无源）。
func (s *WishService) ListExpiryCandidates(expireDays int) ([]*WishItem, error) {
	if expireDays <= 0 {
		expireDays = 30
	}
	cutoff := time.Now().AddDate(0, 0, -expireDays)
	rows, err := s.db.Query(`
		SELECT id, user_id, tmdb_id, imdb_id, media_type, title, year, season, state, found_detail,
		       searching_at, notified_at, search_offset_minute, created_at, updated_at
		FROM wish_items
		WHERE state IN (?, ?) AND created_at < ?
		ORDER BY created_at ASC`,
		WishStatePending, WishStateSearching, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*WishItem
	for rows.Next() {
		it, err := scanWishRowsCursor(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// ---------------------------------------------------------------------------
// 行扫描辅助
// ---------------------------------------------------------------------------

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanWishRow(row *sql.Row) (*WishItem, error) {
	return scanWishCommon(row)
}

func scanWishRowsCursor(rows *sql.Rows) (*WishItem, error) {
	return scanWishCommon(rows)
}

func scanWishCommon(sc rowScanner) (*WishItem, error) {
	var it WishItem
	var searchingAt, notifiedAt sql.NullTime
	err := sc.Scan(
		&it.ID, &it.UserID, &it.TmdbID, &it.ImdbID, &it.MediaType, &it.Title, &it.Year, &it.Season,
		&it.State, &it.FoundDetail, &searchingAt, &notifiedAt, &it.SearchOffsetMinute, &it.CreatedAt, &it.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if searchingAt.Valid {
		t := searchingAt.Time
		it.SearchingAt = &t
	}
	if notifiedAt.Valid {
		t := notifiedAt.Time
		it.NotifiedAt = &t
	}
	return &it, nil
}
