package aesthetic

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	RealmInit     = 0
	RealmFamiliar  = 1
	RealmProfound  = 2
	RealmLegendary = 3

	QuotaNameScant   = "SCANT"
	QuotaNameAbund   = "ABUND"
	QuotaNameExcell  = "EXCEL"
	QuotaNameApex    = "APEX"

	WishStatusDormant  = "dormant"
	WishStatusGlow    = "glow"
	WishStatusIgnited = "ignited"
	WishStatusFaded   = "faded"
)

type Binding struct {
	TgID        int64     `json:"tg_id"`
	EmbyAccount string    `json:"emby_account"`
	Realm       int       `json:"realm"`
	Points      int       `json:"points"`
	MovieQuota  int       `json:"movie_quota"`
	TvQuota     int       `json:"tv_quota"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeen    time.Time `json:"last_seen"`
}

type Wish struct {
	ID         int       `json:"id"`
	TgID       int64     `json:"tg_id"`
	Title      string    `json:"title"`
	Category   string    `json:"category"`
	Energy     int       `json:"energy"`
	Status     string    `json:"status"`
	TmdbID     int       `json:"tmdb_id"`
	MediaType  string    `json:"media_type"`
	PosterPath string    `json:"poster_path"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	IgnitedAt  *time.Time `json:"ignited_at,omitempty"`
}

type AestheticDB struct {
	db   *sql.DB
	mu   sync.RWMutex
	path string
}

func NewAestheticDB(dbPath string) (*AestheticDB, error) {
	dir := dbPath[:len(dbPath)-len("aesthetic.db")]
	if err := createDirIfNotExist(dir); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	adb := &AestheticDB{db: db, path: dbPath}

	if err := adb.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return adb, nil
}

func (adb *AestheticDB) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS bindings (
		tg_id INTEGER PRIMARY KEY,
		emby_account TEXT,
		realm INTEGER DEFAULT 0,
		points INTEGER DEFAULT 0,
		movie_quota INTEGER DEFAULT 2,
		tv_quota INTEGER DEFAULT 2,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_seen DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS wishes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tg_id INTEGER NOT NULL,
		title TEXT NOT NULL COLLATE NOCASE,
		category TEXT DEFAULT 'general',
		energy INTEGER DEFAULT 1,
		status TEXT DEFAULT 'dormant',
		tmdb_id INTEGER,
		media_type TEXT,
		poster_path TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		ignited_at DATETIME,
		FOREIGN KEY (tg_id) REFERENCES bindings(tg_id) ON DELETE CASCADE
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_wishes_title ON wishes(title, tg_id);
	CREATE INDEX IF NOT EXISTS idx_wishes_tg_id ON wishes(tg_id);
	CREATE INDEX IF NOT EXISTS idx_wishes_status ON wishes(status);
	`

	_, err := adb.db.Exec(schema)
	return err
}

func (adb *AestheticDB) Close() error {
	adb.mu.Lock()
	defer adb.mu.Unlock()
	return adb.db.Close()
}

func createDirIfNotExist(path string) error {
	return nil
}

func (adb *AestheticDB) GetOrCreateBinding(tgID int64) (*Binding, error) {
	adb.mu.Lock()
	defer adb.mu.Unlock()

	binding := &Binding{}
	err := adb.db.QueryRow(`
		SELECT tg_id, emby_account, realm, points, movie_quota, tv_quota, created_at, last_seen
		FROM bindings WHERE tg_id = ?
	`, tgID).Scan(
		&binding.TgID, &binding.EmbyAccount, &binding.Realm,
		&binding.Points, &binding.MovieQuota, &binding.TvQuota,
		&binding.CreatedAt, &binding.LastSeen,
	)

	if err == sql.ErrNoRows {
		_, err = adb.db.Exec(`
			INSERT INTO bindings (tg_id, realm, points, movie_quota, tv_quota)
			VALUES (?, 0, 0, 2, 2)
		`, tgID)
		if err != nil {
			return nil, err
		}

		binding.TgID = tgID
		binding.Realm = RealmInit
		binding.Points = 0
		binding.MovieQuota = 2
		binding.TvQuota = 2

		return binding, nil
	}

	if err != nil {
		return nil, err
	}

	adb.touchBinding(tgID)
	return binding, nil
}

func (adb *AestheticDB) touchBinding(tgID int64) {
	_, _ = adb.db.Exec("UPDATE bindings SET last_seen = CURRENT_TIMESTAMP WHERE tg_id = ?", tgID)
}

func (adb *AestheticDB) AddPoint(tgID int) error {
	adb.mu.Lock()
	defer adb.mu.Unlock()

	_, err := adb.db.Exec("UPDATE bindings SET points = points + 1 WHERE tg_id = ?", tgID)
	return err
}

func (adb *AestheticDB) AdvanceRealm(tgID int) error {
	adb.mu.Lock()
	defer adb.mu.Unlock()

	_, err := adb.db.Exec("UPDATE bindings SET realm = MIN(realm + 1, 3) WHERE tg_id = ?", tgID)
	return err
}

func (adb *AestheticDB) ConsumeQuota(tgID int, mediaType string) error {
	adb.mu.Lock()
	defer adb.mu.Unlock()

	var column string
	if mediaType == "movie" {
		column = "movie_quota"
	} else {
		column = "tv_quota"
	}

	_, err := adb.db.Exec(fmt.Sprintf("UPDATE bindings SET %s = %s - 1 WHERE tg_id = ? AND %s > 0", column, column, column), tgID)
	return err
}

func (adb *AestheticDB) RestoreQuota(tgID int) error {
	adb.mu.Lock()
	defer adb.mu.Unlock()

	_, err := adb.db.Exec(`
		UPDATE bindings SET
			movie_quota = 2,
			tv_quota = 2
		WHERE tg_id = ?
	`, tgID)
	return err
}
