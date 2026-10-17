package miniapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xzb177/yimao/internal/services"
)

// TestWatchlistSeasonScopedJoinListRemove MiniApp 想看接口分季：
// POST 带季号记入该季；列表带季号展示；DELETE 只删指定季。
func TestWatchlistSeasonScopedJoinListRemove(t *testing.T) {
	carpool := services.NewCarpoolService(t.TempDir())
	tmdb, _ := newCountingTMDBClient(t)
	mapping := &stubUserMapping{boundIDs: map[int64]bool{101: true}}
	handler := NewServer(Deps{BotToken: miniAppTestToken, Carpool: carpool, TMDB: tmdb, UserMapping: mapping}).Handler()

	add := httptest.NewRecorder()
	handler.ServeHTTP(add, signedRequest(t, http.MethodPost, "/api/miniapp/v1/watchlist", `{"tmdb_id":271413,"type":"tv","season":2}`, 101))
	if add.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", add.Code, add.Body.String())
	}
	if !carpool.Contains(271413, "tv", 2, 101) {
		t.Fatalf("用户应加入 S2 想看")
	}
	if carpool.Contains(271413, "tv", 1, 101) {
		t.Fatalf("用户不应出现在 S1 想看")
	}

	// 列表带季号展示
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, signedRequest(t, http.MethodGet, "/api/miniapp/v1/watchlist", "", 101))
	var payload struct {
		Items []services.CarpoolItem `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil {
		t.Fatalf("list decode: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Season != 2 {
		t.Fatalf("列表应展示 Season=2，实际 %+v", payload.Items)
	}

	// 移除只删指定季
	remove := httptest.NewRecorder()
	handler.ServeHTTP(remove, signedRequest(t, http.MethodDelete, "/api/miniapp/v1/watchlist", `{"tmdb_id":271413,"type":"tv","season":2}`, 101))
	if remove.Code != http.StatusOK || carpool.Contains(271413, "tv", 2, 101) {
		t.Fatalf("S2 应被移除: status=%d body=%s", remove.Code, remove.Body.String())
	}
}

// TestWatchlistMovieIgnoresSeasonParam 电影忽略季号参数（不报错、不分季）。
func TestWatchlistMovieIgnoresSeasonParam(t *testing.T) {
	carpool := services.NewCarpoolService(t.TempDir())
	tmdb, _ := newCountingTMDBClient(t)
	mapping := &stubUserMapping{boundIDs: map[int64]bool{101: true}}
	handler := NewServer(Deps{BotToken: miniAppTestToken, Carpool: carpool, TMDB: tmdb, UserMapping: mapping}).Handler()

	add := httptest.NewRecorder()
	handler.ServeHTTP(add, signedRequest(t, http.MethodPost, "/api/miniapp/v1/watchlist", `{"tmdb_id":271413,"type":"movie","season":3}`, 101))
	if add.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", add.Code, add.Body.String())
	}
	if !carpool.Contains(271413, "movie", 0, 101) {
		t.Fatalf("电影应忽略季号加入")
	}
}
