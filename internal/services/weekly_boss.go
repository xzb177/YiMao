package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// WeeklyBoss 周常梦魇数据
type WeeklyBoss struct {
	WeekStart        string  `json:"week_start"`
	MovieName        string  `json:"movie_name"`
	MovieYear        int     `json:"movie_year"`
	TMDBID           int     `json:"tmdb_id"`
	GenresJSON       string  `json:"genres_json"`
	PosterURL        string  `json:"poster_url"`
	DifficultyMod    float64 `json:"difficulty_mod"`
	FirstClearUserID int64   `json:"first_clear_user_id"`
	TotalAttempts    int     `json:"total_attempts"`
	TotalClears      int     `json:"total_clears"`
}

// GetWeeklyBoss 获取本周梦魇
func (s *SocialDB) GetWeeklyBoss() (*WeeklyBoss, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	weekStart := getMondayString()
	var wb WeeklyBoss
	err := s.db.QueryRow(
		`SELECT week_start, movie_name, movie_year, tmdb_id, genres_json,
		 difficulty_mod, first_clear_user_id, total_attempts, total_clears
		 FROM weekly_boss WHERE week_start = ?`, weekStart,
	).Scan(&wb.WeekStart, &wb.MovieName, &wb.MovieYear, &wb.TMDBID,
		&wb.GenresJSON, &wb.DifficultyMod, &wb.FirstClearUserID,
		&wb.TotalAttempts, &wb.TotalClears)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &wb, nil
}

// SetWeeklyBoss 设置本周梦魇
func (s *SocialDB) SetWeeklyBoss(wb *WeeklyBoss) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO weekly_boss
		 (week_start, movie_name, movie_year, tmdb_id, genres_json, poster_url, difficulty_mod,
		  first_clear_user_id, total_attempts, total_clears, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		wb.WeekStart, wb.MovieName, wb.MovieYear, wb.TMDBID, wb.GenresJSON,
		wb.PosterURL, wb.DifficultyMod, wb.FirstClearUserID, wb.TotalAttempts, wb.TotalClears,
	)
	return err
}

// RecordWeeklyBossAttempt 记录梦魇挑战（增加尝试次数）
func (s *SocialDB) RecordWeeklyBossAttempt(userID int64, success bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	weekStart := getMondayString()
	_, err := s.db.Exec(
		`UPDATE weekly_boss SET total_attempts = total_attempts + 1
		 WHERE week_start = ?`, weekStart)
	if err != nil {
		return err
	}
	if success {
		_, err = s.db.Exec(
			`UPDATE weekly_boss SET total_clears = total_clears + 1
			 WHERE week_start = ?`, weekStart)
	}
	return err
}

// RecordFirstBossClear 记录首个梦魇通关者
func (s *SocialDB) RecordFirstBossClear(userID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	weekStart := getMondayString()
	result, err := s.db.Exec(
		`UPDATE weekly_boss SET first_clear_user_id = ?
		 WHERE week_start = ? AND first_clear_user_id = 0`,
		userID, weekStart)
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// ============================================================
//  周榜 (Weekly Leaderboard)
// ============================================================

// WeeklyRankEntry 周榜条目
type WeeklyRankEntry struct {
	UserID   int64  `json:"user_id"`
	UserName string `json:"user_name"`
	Value    int    `json:"value"`
	Rank     int    `json:"rank"`
}

// UpdateWeeklyLeaderboard 更新周榜数据
func (s *SocialDB) UpdateWeeklyLeaderboard(userID int64, category string, value int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	weekStart := getMondayString()
	_, err := s.db.Exec(
		`INSERT INTO weekly_leaderboard (week_start, user_id, category, value, updated_at)
		 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(week_start, user_id, category) DO UPDATE SET
		 value = value + ?, updated_at = CURRENT_TIMESTAMP`,
		weekStart, userID, category, value, value,
	)
	return err
}

// GetWeeklyLeaderboard 获取指定分类周榜Top N
func (s *SocialDB) GetWeeklyLeaderboard(category string, limit int) ([]*WeeklyRankEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	weekStart := getMondayString()
	rows, err := s.db.Query(
		`SELECT user_id, value FROM weekly_leaderboard
		 WHERE week_start = ? AND category = ?
		 ORDER BY value DESC LIMIT ?`,
		weekStart, category, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*WeeklyRankEntry
	rank := 0
	for rows.Next() {
		rank++
		var e WeeklyRankEntry
		if err := rows.Scan(&e.UserID, &e.Value); err != nil {
			continue
		}
		e.Rank = rank
		entries = append(entries, &e)
	}
	return entries, nil
}

// ============================================================
//  回归钩子
// ============================================================

// GetInactiveUsersWithNemesis 获取超过24h未冒险且有宿敌的用户
func (s *SocialDB) GetInactiveUsersWithNemesis() ([]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cutoff := time.Now().Add(-24 * time.Hour)
	rows, err := s.db.Query(`
		SELECT a.user_id FROM adventure_stats a
		WHERE a.success = 0 AND a.user_id NOT IN (
			SELECT DISTINCT user_id FROM adventure_stats
			WHERE created_at > ?
		)
		GROUP BY a.user_id
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err == nil {
			users = append(users, uid)
		}
	}
	return users, nil
}

// GetTopNemesis 获取用户最大宿敌
func (s *SocialDB) GetTopNemesis(userID int64) (movieName string, failCount int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.db.QueryRow(
		`SELECT movie_name, COUNT(*) as cnt FROM adventure_stats
		 WHERE user_id = ? AND success = 0
		 GROUP BY movie_name ORDER BY cnt DESC LIMIT 1`,
		userID,
	).Scan(&movieName, &failCount)
	return
}

// getMondayString 返回本周一日期
func getMondayString() string {
	now := time.Now()
	weekday := now.Weekday()
	daysSinceMonday := int(weekday) - 1
	if weekday == time.Sunday {
		daysSinceMonday = 6
	}
	monday := now.AddDate(0, 0, -daysSinceMonday)
	return monday.Format("2006-01-02")
}

// AutoGenerateWeeklyBoss 自动生成每周梦魇（TMDB trending 选片）
// 如果本周梦魇已存在则跳过，否则从 TMDB trending API 选取第一部有海报的电影作为梦魇。
func (s *SocialDB) AutoGenerateWeeklyBoss(tmdbAPIKey string) {
	if tmdbAPIKey == "" {
		return
	}

	// 检查本周梦魇是否已存在
	existing, err := s.GetWeeklyBoss()
	if err != nil || existing != nil {
		return
	}

	// 调用 TMDB trending API
	apiURL := "https://api.themoviedb.org/3/trending/movie/week?language=zh-CN&api_key=" + tmdbAPIKey
	resp, err := http.Get(apiURL)
	if err != nil {
		logger.Info("[WeeklyBoss] TMDB trending API failed: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		logger.Info("[WeeklyBoss] Failed to read TMDB response: %v", err)
		return
	}

	var result struct {
		Results []struct {
			ID          int    `json:"id"`
			Title       string `json:"title"`
			ReleaseDate string `json:"release_date"`
			PosterPath  string `json:"poster_path"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		logger.Info("[WeeklyBoss] TMDB response parse failed: %v", err)
		return
	}

	// 选第一部有海报的电影
	for _, movie := range result.Results {
		if movie.PosterPath == "" {
			continue
		}

		year := 0
		if len(movie.ReleaseDate) >= 4 {
			fmt.Sscanf(movie.ReleaseDate[:4], "%d", &year)
		}

		wb := &WeeklyBoss{
			WeekStart:     getMondayString(),
			MovieName:     movie.Title,
			MovieYear:     year,
			TMDBID:        movie.ID,
			PosterURL:     "https://image.tmdb.org/t/p/w500" + movie.PosterPath,
			DifficultyMod: 1.0,
		}

		if err := s.SetWeeklyBoss(wb); err != nil {
			logger.Info("[WeeklyBoss] Failed to save weekly boss: %v", err)
			return
		}

		logger.Info("[WeeklyBoss] Auto-generated weekly boss: %s (%d)", movie.Title, year)
		return
	}

	logger.Info("[WeeklyBoss] No movie with poster found in trending")
}

// migrateWeeklyBossPosterURL adds poster_url column to existing weekly_boss tables
func (s *SocialDB) migrateWeeklyBossPosterURL() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if column exists
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('weekly_boss') WHERE name='poster_url'`).Scan(&count)
	if err != nil {
		return fmt.Errorf("check column: %w", err)
	}
	if count > 0 {
		return nil // Already exists
	}

	_, err = s.db.Exec(`ALTER TABLE weekly_boss ADD COLUMN poster_url TEXT DEFAULT ''`)
	if err != nil {
		return fmt.Errorf("add column: %w", err)
	}
	logger.Info("[SocialDB] Migrated: added poster_url to weekly_boss")
	return nil
}

// GetUserAdventureStats 获取用户冒险统计（无此类→返回nil）
func (s *SocialDB) GetUserAdventureStats(userID int64) (total, wins, sss, maxScore, maxCombo, streakDays, bestStreak int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.db.QueryRow(`SELECT COUNT(*) FROM adventure_stats WHERE user_id = ?`, userID).Scan(&total)
	s.db.QueryRow(`SELECT COUNT(*) FROM adventure_stats WHERE user_id = ? AND success = 1`, userID).Scan(&wins)
	s.db.QueryRow(`SELECT COUNT(*) FROM adventure_stats WHERE user_id = ? AND success = 1 AND grade = 'SSS'`, userID).Scan(&sss)
	s.db.QueryRow(`SELECT COALESCE(MAX(score), 0) FROM adventure_stats WHERE user_id = ?`, userID).Scan(&maxScore)
	s.db.QueryRow(`SELECT COALESCE(MAX(max_combo), 0) FROM adventure_stats WHERE user_id = ?`, userID).Scan(&maxCombo)

	streak, err := s.GetAdventureStreak(userID)
	if err == nil && streak != nil {
		streakDays = streak.CurrentStreak
		bestStreak = streak.BestStreak
	}
	return
}

// GetUserWeeklyRank 获取用户本周排名
func (s *SocialDB) GetUserWeeklyRank(userID int64, category string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	weekStart := getMondayString()
	var value int
	s.db.QueryRow(
		`SELECT COALESCE(value, 0) FROM weekly_leaderboard
		 WHERE week_start = ? AND user_id = ? AND category = ?`,
		weekStart, userID, category,
	).Scan(&value)

	if value == 0 {
		return 0
	}

	var rank int
	s.db.QueryRow(
		`SELECT COUNT(*) + 1 FROM weekly_leaderboard
		 WHERE week_start = ? AND category = ? AND value > ?`,
		weekStart, category, value,
	).Scan(&rank)
	return rank
}

// NemesisEntry 宿敌条目
type NemesisEntry struct {
	MovieName string
	FailCount int
	Revenged  bool
}

// GetAllNemeses 获取用户所有宿敌列表
func (s *SocialDB) GetAllNemeses(userID int64) ([]NemesisEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT a.movie_name, COUNT(*) as fail_count,
			CASE WHEN EXISTS(SELECT 1 FROM adventure_stats b
				WHERE b.user_id = a.user_id AND b.movie_name = a.movie_name AND b.success = 1)
				THEN 1 ELSE 0 END as revenged
		FROM adventure_stats a
		WHERE a.user_id = ? AND a.success = 0
		GROUP BY a.movie_name
		ORDER BY fail_count DESC
		LIMIT 5
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []NemesisEntry
	for rows.Next() {
		var e NemesisEntry
		if err := rows.Scan(&e.MovieName, &e.FailCount, &e.Revenged); err == nil {
			entries = append(entries, e)
		}
	}
	return entries, nil
}
