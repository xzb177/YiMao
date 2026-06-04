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
	CreatedAt   time.Time
	UpdatedAt   time.Time
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

	logger.Info("[wish] 许愿池数据库已初始化: %s", dbPath)
	return &WishService{db: db}, nil
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

	// 3) 落库（PENDING）。
	now := time.Now()
	res, err := tx.Exec(`
		INSERT INTO wish_items
			(user_id, tmdb_id, imdb_id, media_type, title, year, season, state, found_detail, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)
	`, item.UserID, item.TmdbID, item.ImdbID, item.MediaType, item.Title, item.Year, item.Season,
		WishStatePending, now, now)
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
			       searching_at, notified_at, created_at, updated_at
			FROM wish_items
			WHERE tmdb_id = ? AND media_type = ? AND season = ? AND state IN (?,?,?,?)
			LIMIT 1`,
			tmdbID, mediaType, season,
			WishStatePending, WishStateSearching, WishStateFound, WishStateNotified)
	} else {
		row = tx.QueryRow(`
			SELECT id, user_id, tmdb_id, imdb_id, media_type, title, year, season, state, found_detail,
			       searching_at, notified_at, created_at, updated_at
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
func SearchOffsetMinutes(itemID int64) int {
	h := sha1.Sum([]byte(fmt.Sprintf("wish-%d", itemID)))
	v := binary.BigEndian.Uint64(h[:8])
	return int(v % 1440)
}

// ClaimSearchableItems 原子地「认领」一批今天该搜、且 searching_at 锁可用的条目。
// 它在一个事务里：选出符合条件的 SEARCHING/PENDING 条目 → 立刻 UPDATE searching_at=now()（上锁）。
// 返回被认领的条目（已带新的 searching_at），调度据此去站点搜索。
//
// 锁规则（坑6）：只选 searching_at IS NULL OR searching_at < now-interval。
// interval = WISH_RESEARCH_INTERVAL_HOURS，崩溃残留的旧锁超过该窗口会自动重纳入（自愈）。
//
// 错峰（坑2）：只认领 SearchOffsetMinutes(id) 落在「当前时刻所在分钟」附近窗口内的条目，
// 由调用方按分钟 tick 传入 nowMinuteOfDay + window。
func (s *WishService) ClaimSearchableItems(intervalHours int, limit int) ([]*WishItem, error) {
	if intervalHours <= 0 {
		intervalHours = 24
	}
	if limit <= 0 {
		limit = 50
	}
	lockCutoff := time.Now().Add(-time.Duration(intervalHours) * time.Hour)

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

	// PENDING 也纳入（首次入调度即 WISHED→SEARCHING）。
	rows, err := tx.Query(`
		SELECT id, user_id, tmdb_id, imdb_id, media_type, title, year, season, state, found_detail,
		       searching_at, notified_at, created_at, updated_at
		FROM wish_items
		WHERE state IN (?, ?)
		  AND (searching_at IS NULL OR searching_at < ?)
		ORDER BY id ASC
		LIMIT ?`,
		WishStatePending, WishStateSearching, lockCutoff, limit)
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
		       searching_at, notified_at, created_at, updated_at
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
		       searching_at, notified_at, created_at, updated_at
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
		       searching_at, notified_at, created_at, updated_at
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
		&it.State, &it.FoundDetail, &searchingAt, &notifiedAt, &it.CreatedAt, &it.UpdatedAt,
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
