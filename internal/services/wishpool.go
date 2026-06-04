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

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open wishpool database: %w", err)
	}

	// 建表 + 唯一索引（canonical key）。
	// 唯一索引说明：tmdb_id 优先；tmdb_id=0（仅 imdb）的条目用 imdb_id 区分。
	// 这里建两个部分唯一索引分别覆盖「有 tmdb」与「无 tmdb 仅 imdb」两种情况，
	// 避免 tmdb_id=0 的多条 imdb 条目互相撞唯一键。
	_, err = db.Exec(`
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

		CREATE UNIQUE INDEX IF NOT EXISTS idx_wish_canonical_tmdb
			ON wish_items(tmdb_id, media_type, season)
			WHERE tmdb_id != 0;

		CREATE UNIQUE INDEX IF NOT EXISTS idx_wish_canonical_imdb
			ON wish_items(imdb_id, media_type, season)
			WHERE tmdb_id = 0 AND imdb_id != '';

		CREATE INDEX IF NOT EXISTS idx_wish_state ON wish_items(state);
		CREATE INDEX IF NOT EXISTS idx_wish_user ON wish_items(user_id);
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create wish_items table: %w", err)
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

	logger.Info("[wish] 许愿池数据库已初始化: %s", dbPath)
	return &WishService{db: db}, nil
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
		`SELECT COUNT(*) FROM wish_items WHERE user_id = ? AND state IN (?,?,?,?)`,
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
// 调度查询 + searching_at 锁（坑2 / 坑6）
// ---------------------------------------------------------------------------

// SearchOffsetMinutes 按 hash(item_id)%1440 算错峰偏移（坑2）。导出供调度与测试使用。
// 注意：入池时 id 尚未生成（AUTOINCREMENT），故入池偏移用 canonical key 计算
// （见 searchOffsetForCanonical）；本函数保留用于已知 id 的兼容/测试场景。
func SearchOffsetMinutes(itemID int64) int {
	h := sha1.Sum([]byte(fmt.Sprintf("wish-%d", itemID)))
	v := binary.BigEndian.Uint64(h[:8])
	return int(v % 1440)
}

// searchOffsetForCanonical 用 canonical key（tmdb 优先，否则 imdb）算入池时的错峰偏移。
// canonical key 在入池前已确定且稳定，避免依赖尚未生成的自增 id。
func searchOffsetForCanonical(tmdbID int, imdbID, mediaType string, season int) int {
	var key string
	if tmdbID != 0 {
		key = fmt.Sprintf("wish-tmdb-%d-%s-%d", tmdbID, mediaType, season)
	} else {
		key = fmt.Sprintf("wish-imdb-%s-%s-%d", imdbID, mediaType, season)
	}
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
