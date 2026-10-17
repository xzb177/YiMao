package services

import (
	"testing"
)

// 审计 A7④：imdb canonical 的许愿以前不计入每人上限（统计 SQL 只关联 tmdb）。
func TestWishPerUserLimitCountsImdbCanonical(t *testing.T) {
	ws := newTestWishService(t)

	// 20 条 imdb 许愿（TmdbID=0）全部入池。
	for i := 0; i < WishMaxPerUser; i++ {
		res, err := ws.AddWish(&WishItem{
			UserID: 1, ImdbID: "tt9000" + string(rune('a'+i)), MediaType: "movie",
			Title: "IMDb 片",
		})
		if err != nil || !res.Created {
			t.Fatalf("第 %d 条 imdb 许愿应成功: res=%+v err=%v", i+1, res, err)
		}
	}
	// 第 21 条 imdb 许愿必须被上限拦下。
	res, err := ws.AddWish(&WishItem{UserID: 1, ImdbID: "tt9999999", MediaType: "movie", Title: "超限"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OverPerUser {
		t.Fatalf("第 21 条 imdb 许愿应触发 OverPerUser，实际 %+v", res)
	}
}

// 审计 A7④：重复许愿（duplicate 路径）以前不查上限，可无限给别人的愿望"点赞"刷计数。
func TestWishDuplicatePathEnforcesPerUserLimit(t *testing.T) {
	ws := newTestWishService(t)

	// 用户 2 先许一个愿（tmdb）。
	res, err := ws.AddWish(&WishItem{UserID: 2, TmdbID: 200, MediaType: "movie", Title: "片B"})
	if err != nil || !res.Created {
		t.Fatalf("建愿失败: %+v %v", res, err)
	}

	// 用户 1 把自己的 20 个名额用满。
	for i := 0; i < WishMaxPerUser; i++ {
		if _, err := ws.AddWish(&WishItem{
			UserID: 1, TmdbID: 1000 + i, MediaType: "movie", Title: "片",
		}); err != nil {
			t.Fatalf("第 %d 条许愿失败: %v", i+1, err)
		}
	}

	// 用户 1 再去"重复许愿"用户 2 的片（duplicate 路径）——应被上限拦下。
	res, err = ws.AddWish(&WishItem{UserID: 1, TmdbID: 200, MediaType: "movie", Title: "片B"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OverPerUser {
		t.Fatalf("满额后重复许愿应触发 OverPerUser，实际 %+v", res)
	}
	if n := ws.CountWishers(CanonicalKey(200, "", "movie", 0)); n != 1 {
		t.Fatalf("被拦下的重复许愿不应计入等待人数，实际 %d", n)
	}
}
