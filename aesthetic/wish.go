package aesthetic

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

func (adb *AestheticDB) CreateWish(wish *Wish) error {
	adb.mu.Lock()
	defer adb.mu.Unlock()

	query := `
		INSERT INTO wishes (tg_id, title, category, energy, status, tmdb_id, media_type, poster_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := adb.db.Exec(query,
		wish.TgID, wish.Title, wish.Category, wish.Energy,
		WishStatusDormant, wish.TmdbID, wish.MediaType, wish.PosterPath,
	)
	if err != nil {
		return err
	}

	id, _ := result.LastInsertId()
	wish.ID = int(id)
	wish.Status = WishStatusDormant

	return nil
}

func (adb *AestheticDB) IgniteWish(wishID int, tgID int) error {
	adb.mu.Lock()
	defer adb.mu.Unlock()

	query := `
		UPDATE wishes
		SET status = ?, energy = energy + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND tg_id = ?
	`

	result, err := adb.db.Exec(query, WishStatusGlow, wishID, tgID)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("wish not found")
	}

	return nil
}

func (adb *AestheticDB) AccumulateEnergy(title string, delta int) error {
	adb.mu.Lock()
	defer adb.mu.Unlock()

	query := `
		UPDATE wishes
		SET energy = energy + ?, updated_at = CURRENT_TIMESTAMP
		WHERE title = ? AND status IN ('dormant', 'glow')
	`

	_, err := adb.db.Exec(query, delta, title)
	return err
}

func (adb *AestheticDB) FindWishByTitle(tgID int64, title string) (*Wish, error) {
	adb.mu.RLock()
	defer adb.mu.RUnlock()

	wish := &Wish{}
	err := adb.db.QueryRow(`
		SELECT id, tg_id, title, category, energy, status, tmdb_id, media_type, poster_path, created_at, updated_at, ignited_at
		FROM wishes
		WHERE tg_id = ? AND title = ?
		ORDER BY id DESC LIMIT 1
	`, tgID, title).Scan(
		&wish.ID, &wish.TgID, &wish.Title, &wish.Category,
		&wish.Energy, &wish.Status, &wish.TmdbID, &wish.MediaType,
		&wish.PosterPath, &wish.CreatedAt, &wish.UpdatedAt, &wish.IgnitedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return wish, nil
}

func (adb *AestheticDB) GetUserWishes(tgID int64) ([]Wish, error) {
	adb.mu.RLock()
	defer adb.mu.RUnlock()

	rows, err := adb.db.Query(`
		SELECT id, tg_id, title, category, energy, status, tmdb_id, media_type, poster_path, created_at, updated_at, ignited_at
		FROM wishes
		WHERE tg_id = ?
		ORDER BY energy DESC, created_at DESC
	`, tgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var wishes []Wish
	for rows.Next() {
		var w Wish
		err := rows.Scan(
			&w.ID, &w.TgID, &w.Title, &w.Category,
			&w.Energy, &w.Status, &w.TmdbID, &w.MediaType,
			&w.PosterPath, &w.CreatedAt, &w.UpdatedAt, &w.IgnitedAt,
		)
		if err != nil {
			return nil, err
		}
		wishes = append(wishes, w)
	}

	return wishes, nil
}

func (adb *AestheticDB) GetWishByID(id int) (*Wish, error) {
	adb.mu.RLock()
	defer adb.mu.RUnlock()

	wish := &Wish{}
	err := adb.db.QueryRow(`
		SELECT id, tg_id, title, category, energy, status, tmdb_id, media_type, poster_path, created_at, updated_at, ignited_at
		FROM wishes
		WHERE id = ?
	`, id).Scan(
		&wish.ID, &wish.TgID, &wish.Title, &wish.Category,
		&w.Energy, &wish.Status, &wish.TmdbID, &wish.MediaType,
		&w.PosterPath, &w.CreatedAt, &w.UpdatedAt, &w.IgnitedAt,
	)

	if err != nil {
		return nil, err
	}

	return wish, nil
}

func (adb *AestheticDB) SetWishIgnited(id int, tmdbID int) error {
	adb.mu.Lock()
	defer adb.mu.Unlock()

	now := time.Now()
	_, err := adb.db.Exec(`
		UPDATE wishes
		SET status = ?, tmdb_id = ?, ignited_at = ?
		WHERE id = ?
	`, WishStatusIgnited, tmdbID, now, id)

	return err
}

func (adb *AestheticDB) SetWishFaded(id int) error {
	adb.mu.Lock()
	defer adb.mu.Unlock()

	_, err := adb.db.Exec(`
		UPDATE wishes
		SET status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, WishStatusFaded, id)

	return err
}

func (adb *AestheticDB) DeleteWish(id int) error {
	adb.mu.Lock()
	defer adb.mu.Unlock()

	_, err := adb.db.Exec("DELETE FROM wishes WHERE id = ?", id)
	return err
}
