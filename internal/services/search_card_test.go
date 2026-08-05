package services

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testPosterJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 300, 450))
	for y := 0; y < 450; y++ {
		for x := 0; x < 300; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 255), uint8(y % 255), 90, 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, nil); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestRenderSearchVisualCardJPEGContract(t *testing.T) {
	t.Setenv("YIMAO_CJK_FONT", "/usr/share/fonts/noto/NotoSansCJK-Regular.ttc")
	item := SearchResult{ID: 4048, Title: "凡人修仙传", Year: 2020, Type: "tv", Rating: 8.2, Overview: strings.Repeat("韩立为了修仙踏上漫漫长路。", 10)}
	data, err := RenderSearchVisualCard(testPosterJPEG(t), 0, item, "站内追更")
	if err != nil {
		t.Fatal(err)
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpeg" || decoded.Bounds().Dx() != searchCardWidth || decoded.Bounds().Dy() != searchCardHeight {
		t.Fatalf("format=%s bounds=%v", format, decoded.Bounds())
	}
	if len(data) < 20_000 {
		t.Fatalf("card unexpectedly small: %d bytes", len(data))
	}
}

func syntheticPosterJPEG(t *testing.T, base, highlight color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 240, 360))
	for y := 0; y < 360; y++ {
		for x := 0; x < 240; x++ {
			mix := float64(x+y) / 600
			img.SetRGBA(x, y, color.RGBA{R: uint8(float64(base.R)*(1-mix) + float64(highlight.R)*mix), G: uint8(float64(base.G)*(1-mix) + float64(highlight.G)*mix), B: uint8(float64(base.B)*(1-mix) + float64(highlight.B)*mix), A: 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestRenderSearchVisualCardAdaptivePosterPalettes(t *testing.T) {
	posters := map[string][2]color.RGBA{
		"warm":      {{85, 35, 16, 255}, {238, 170, 65, 255}},
		"cool":      {{12, 35, 72, 255}, {62, 184, 224, 255}},
		"bright":    {{210, 215, 190, 255}, {255, 239, 120, 255}},
		"red-black": {{5, 5, 7, 255}, {205, 20, 32, 255}},
	}
	for name, colours := range posters {
		t.Run(name, func(t *testing.T) {
			poster := syntheticPosterJPEG(t, colours[0], colours[1])
			src, _, err := image.Decode(bytes.NewReader(poster))
			if err != nil {
				t.Fatal(err)
			}
			palette := extractSearchCardPalette(src)
			if got := contrastRatio(ensureTextContrast(palette.accent, palette.pill, 4.5), palette.pill); got < 4.5 {
				t.Fatalf("pill contrast %.2f below WCAG AA; palette=%+v", got, palette)
			}
			item := SearchResult{Title: "自适应影视卡", Year: 2020, Type: "tv", Rating: 8.4, Overview: strings.Repeat("这是一段测试简介。", 20)}
			got, err := RenderSearchVisualCard(poster, 7, item, "站内追更")
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := jpeg.Decode(bytes.NewReader(got))
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Bounds() != image.Rect(0, 0, searchCardWidth, searchCardHeight) {
				t.Fatalf("bounds=%v", decoded.Bounds())
			}
		})
	}
}

func TestSearchCardLayoutSafetyAndContrastHelpers(t *testing.T) {
	if searchCardSafeInset != searchCardWidth*8/100 {
		t.Fatalf("safe inset=%d, want 8%% of width", searchCardSafeInset)
	}
	if searchCardGradient >= 984 {
		t.Fatalf("gradient starts too late: %d", searchCardGradient)
	}
	for _, bg := range []color.RGBA{{0, 0, 0, 255}, {255, 255, 255, 255}, {110, 110, 110, 255}, {120, 0, 0, 255}} {
		fg := ensureTextContrast(color.RGBA{120, 120, 120, 255}, bg, 4.5)
		if ratio := contrastRatio(fg, bg); ratio < 4.5 {
			t.Fatalf("contrast %.2f for bg=%v fg=%v", ratio, bg, fg)
		}
	}
}

func TestSearchCardStatusLabelAndPillContrast(t *testing.T) {
	cases := map[string]string{
		"站内追更":   "状态 · 站内追更",
		"云海可看":   "状态 · 云海可看",
		"可求片":    "状态 · 可求片",
		"状态暂未确认": "状态 · 暂未确认",
		"":       "状态 · 暂未确认",
	}
	for input, want := range cases {
		if got := formatSearchCardStatus(input); got != want {
			t.Fatalf("status %q => %q, want %q", input, got, want)
		}
	}
	palettes := []searchCardPalette{
		{accent: color.RGBA{224, 136, 58, 255}, deep: color.RGBA{27, 21, 18, 255}},
		{accent: color.RGBA{70, 164, 208, 255}, deep: color.RGBA{16, 24, 33, 255}},
	}
	for _, palette := range palettes {
		fill := mixCardColor(palette.deep, palette.accent, .64)
		text := ensureTextContrast(color.RGBA{248, 250, 252, 255}, fill, 4.5)
		if ratio := contrastRatio(text, fill); ratio < 4.5 {
			t.Fatalf("status contrast %.2f below AA: fill=%v text=%v", ratio, fill, text)
		}
		if contrastRatio(fill, palette.deep) < 2 {
			t.Fatalf("status fill not distinct from gradient: fill=%v deep=%v", fill, palette.deep)
		}
	}
}

func TestPaletteTextColorTracksArtworkAndKeepsContrast(t *testing.T) {
	background := color.RGBA{18, 25, 34, 255}
	warm := paletteTextColor(color.RGBA{188, 124, 74, 255}, background, .48, 4.5)
	cool := paletteTextColor(color.RGBA{68, 142, 198, 255}, background, .48, 4.5)
	if warm == cool {
		t.Fatalf("different poster palettes produced identical text: %v", warm)
	}
	if warm.R <= warm.B {
		t.Fatalf("warm artwork did not yield warm text: %v", warm)
	}
	if cool.B <= cool.R {
		t.Fatalf("cool artwork did not yield cool text: %v", cool)
	}
	for _, got := range []color.RGBA{warm, cool} {
		if ratio := contrastRatio(got, background); ratio < 4.5 {
			t.Fatalf("palette text contrast %.2f below minimum: %v", ratio, got)
		}
	}
}

func TestCenterCropBoundsPreservesTwoByThreePoster(t *testing.T) {
	src := image.Rect(0, 0, 600, 900)
	if got := centerCropBounds(src, searchCardWidth, searchCardHeight); got != src {
		t.Fatalf("2:3 poster was cropped: got=%v want=%v", got, src)
	}
	wide := centerCropBounds(image.Rect(0, 0, 1000, 1000), searchCardWidth, searchCardHeight)
	if wide.Dx() != 667 || wide.Dy() != 1000 || wide.Min.X != 166 {
		t.Fatalf("unexpected centred wide crop: %v", wide)
	}
}

func TestRenderSearchVisualCardKeepsContinuousArtworkAtTop(t *testing.T) {
	t.Setenv("YIMAO_CJK_FONT", "/usr/share/fonts/noto/NotoSansCJK-Regular.ttc")
	img := image.NewRGBA(image.Rect(0, 0, 300, 450))
	for y := 0; y < 450; y++ {
		for x := 0; x < 300; x++ {
			c := color.RGBA{200, 35, 30, 255}
			if x >= 100 && x < 200 {
				c = color.RGBA{30, 170, 60, 255}
			} else if x >= 200 {
				c = color.RGBA{35, 70, 210, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var poster bytes.Buffer
	if err := jpeg.Encode(&poster, img, &jpeg.Options{Quality: 96}); err != nil {
		t.Fatal(err)
	}
	card, err := RenderSearchVisualCard(poster.Bytes(), 0, SearchResult{Title: "连续画面"}, "点详情查看状态")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := jpeg.Decode(bytes.NewReader(card))
	if err != nil {
		t.Fatal(err)
	}
	want := []color.RGBA{{200, 35, 30, 255}, {30, 170, 60, 255}, {35, 70, 210, 255}}
	for i, x := range []int{150, 450, 750} {
		got := color.RGBAModel.Convert(rendered.At(x, 300)).(color.RGBA)
		if absInt(int(got.R)-int(want[i].R)) > 18 || absInt(int(got.G)-int(want[i].G)) > 18 || absInt(int(got.B)-int(want[i].B)) > 18 {
			t.Fatalf("artwork changed at x=%d: got=%v want≈%v", x, got, want[i])
		}
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestRenderSearchVisualCardGradientHasNoChromaticCorruption(t *testing.T) {
	t.Setenv("YIMAO_CJK_FONT", "/usr/share/fonts/noto/NotoSansCJK-Regular.ttc")
	img := image.NewRGBA(image.Rect(0, 0, 300, 450))
	base := color.RGBA{94, 128, 162, 255}
	for y := 0; y < 450; y++ {
		for x := 0; x < 300; x++ {
			img.SetRGBA(x, y, base)
		}
	}
	var poster bytes.Buffer
	if err := jpeg.Encode(&poster, img, &jpeg.Options{Quality: 98}); err != nil {
		t.Fatal(err)
	}
	card, err := RenderSearchVisualCard(poster.Bytes(), 0, SearchResult{Title: "渐变连续性"}, "点详情查看状态")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := jpeg.Decode(bytes.NewReader(card))
	if err != nil {
		t.Fatal(err)
	}
	previous := color.RGBAModel.Convert(rendered.At(450, searchCardGradient-2)).(color.RGBA)
	for y := searchCardGradient - 1; y < 900; y++ {
		current := color.RGBAModel.Convert(rendered.At(450, y)).(color.RGBA)
		if absInt(int(current.R)-int(previous.R)) > 8 || absInt(int(current.G)-int(previous.G)) > 8 || absInt(int(current.B)-int(previous.B)) > 8 {
			t.Fatalf("gradient colour jump at y=%d: previous=%v current=%v", y, previous, current)
		}
		if int(current.G)-int(current.R) > 90 || int(current.B)-int(current.R) > 120 {
			t.Fatalf("unexpected chromatic fringe at y=%d: %v", y, current)
		}
		previous = current
	}
}

func TestWrapCardTextAvoidsCJKPunctuationAtLineStart(t *testing.T) {
	t.Setenv("YIMAO_CJK_FONT", "/usr/share/fonts/noto/NotoSansCJK-Regular.ttc")
	faces, err := loadSearchCardFaces()
	if err != nil {
		t.Fatal(err)
	}
	defer faces.close()
	text := "平凡少年韩立出生贫困，为了让家人过上更好的生活，自愿前去七玄门参加入门考核，最终被墨大夫收入门下。随着修炼深入，他逐渐发现了隐藏在门派背后的秘密。"
	for _, line := range wrapCardText(faces.body, text, searchCardWidth-2*searchCardSafeInset, 3) {
		runes := []rune(line)
		if len(runes) > 0 && isCardLineStartProhibited(runes[0]) {
			t.Fatalf("line starts with prohibited punctuation: %q", line)
		}
	}
}

func TestResolveSearchCardStatusPriorityAndFailureSemantics(t *testing.T) {
	cases := []struct {
		name       string
		embyExists bool
		embyErr    error
		subscribed bool
		cacheFresh bool
		want       string
	}{
		{name: "emby wins over active subscription", embyExists: true, subscribed: true, cacheFresh: true, want: "云海可看"},
		{name: "active subscription survives emby failure", embyErr: errors.New("timeout"), subscribed: true, cacheFresh: true, want: "站内追更"},
		{name: "confirmed miss", cacheFresh: true, want: "可求片"},
		{name: "emby failure", embyErr: errors.New("timeout"), cacheFresh: true, want: "状态暂未确认"},
		{name: "stale subscription cache", want: "状态暂未确认"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveSearchCardStatus(tc.embyExists, tc.embyErr, tc.subscribed, tc.cacheFresh); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEmbyMediaAvailabilityDistinguishesMissFromFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Users/test/Items":
			if got := r.URL.Query().Get("AnyProviderIdEquals"); got != "tmdb.4048" {
				t.Fatalf("provider filter=%q", got)
			}
			_, _ = w.Write([]byte(`{"TotalRecordCount":0,"Items":[]}`))
		case "/broken/Users/test/Items":
			http.Error(w, "down", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewMoviePilotClient("http://invalid", "", "")
	client.SetEmbyConfig(server.URL, "test-key")
	client.SetEmbyUserID("test")
	client.httpClient = server.Client()
	exists, err := client.EmbyMediaAvailabilityByTMDB(4048, MediaTypeTV)
	if err != nil || exists {
		t.Fatalf("confirmed miss: exists=%v err=%v", exists, err)
	}
	client.SetEmbyConfig(server.URL+"/broken", "test-key")
	if _, err := client.EmbyMediaAvailabilityByTMDB(4048, MediaTypeTV); err == nil {
		t.Fatal("HTTP failure was collapsed into a confirmed miss")
	}
}

func TestEmbyUserDiscoveryPrefersNestedAdminAndCaches(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Users":
			calls++
			_, _ = w.Write([]byte(`[{"Id":"limited","Policy":{"IsAdministrator":false}},{"Id":"admin","Policy":{"IsAdministrator":true}}]`))
		case "/Users/admin/Items":
			_, _ = w.Write([]byte(`{"TotalRecordCount":1,"Items":[{"Id":"media-1"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewMoviePilotClient("http://invalid", "", "")
	client.SetEmbyConfig(server.URL, "test-key")
	client.httpClient = server.Client()
	for i := 0; i < 2; i++ {
		exists, err := client.EmbyMediaAvailabilityByTMDB(4048, MediaTypeTV)
		if err != nil || !exists {
			t.Fatalf("lookup %d: exists=%v err=%v", i, exists, err)
		}
	}
	if calls != 1 {
		t.Fatalf("users API calls=%d, want 1", calls)
	}
}

func TestCachedSubscriptionMediaKeysFiltersStateAndType(t *testing.T) {
	client := NewMoviePilotClient("http://invalid", "", "")
	client.subsCacheData = []SubscribeItem{
		{TMDBID: 7, Type: "movie", State: StateSearching},
		{TMDBID: 7, Type: "tv", State: StateCancelled},
		{TMDBID: 8, Type: "tv", State: StateCompleted},
		{TMDBID: 9, Type: "tv", State: StateDownloading},
	}
	client.subsCacheTime = time.Now()
	keys, fresh := client.CachedSubscriptionMediaKeys()
	if !fresh {
		t.Fatal("fresh cache reported stale")
	}
	if _, ok := keys["movie:7"]; !ok {
		t.Fatal("active movie subscription missing")
	}
	if _, ok := keys["tv:9"]; !ok {
		t.Fatal("active TV subscription missing")
	}
	for _, key := range []string{"tv:7", "tv:8"} {
		if _, ok := keys[key]; ok {
			t.Fatalf("inactive subscription leaked: %s", key)
		}
	}
}

func TestCachedSubscriptionTMDBIDsFreshOnly(t *testing.T) {
	client := NewMoviePilotClient("http://invalid", "", "")
	client.subsCacheData = []SubscribeItem{{TMDBID: 4048}, {TMDBID: 0}}
	client.subsCacheTime = time.Now()
	ids, fresh := client.CachedSubscriptionTMDBIDs()
	if !fresh {
		t.Fatal("fresh cache reported stale")
	}
	if _, ok := ids[4048]; !ok || len(ids) != 1 {
		t.Fatalf("ids=%v", ids)
	}
	client.subsCacheTime = time.Now().Add(-client.subsCacheTTL - time.Second)
	if ids, fresh := client.CachedSubscriptionTMDBIDs(); fresh || ids != nil {
		t.Fatalf("stale cache leaked: %v %v", ids, fresh)
	}
}

func TestCompactCardTextHasNinetyRuneLimit(t *testing.T) {
	got := compactCardText(strings.Repeat("简", 91), 90)
	if len([]rune(got)) != 91 || !strings.HasSuffix(got, "…") {
		t.Fatalf("got %d runes: %q", len([]rune(got)), got)
	}
}

func TestNormalizeSearchPosterRejectsUntrustedURLs(t *testing.T) {
	for _, raw := range []string{
		"http://image.tmdb.org/t/p/w500/a.jpg",
		"https://127.0.0.1/poster.jpg",
		"https://example.com/poster.jpg",
		"https://image.tmdb.org.evil.invalid/poster.jpg",
	} {
		if got := normalizeSearchPoster(raw); got != "" {
			t.Fatalf("untrusted URL accepted: %q -> %q", raw, got)
		}
	}
	if got := normalizeSearchPoster("/abc.jpg"); got != "https://image.tmdb.org/t/p/w780/abc.jpg" {
		t.Fatalf("relative poster=%q", got)
	}
}

func TestRenderSearchVisualCardRejectsOversizedDimensions(t *testing.T) {
	var raw bytes.Buffer
	raw.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	raw.Write([]byte{0, 0, 0, 13, 'I', 'H', 'D', 'R'})
	raw.Write([]byte{0, 0, 0x23, 0x29, 0, 0, 0x23, 0x29, 8, 2, 0, 0, 0}) // 9001x9001
	if _, err := RenderSearchVisualCard(raw.Bytes(), 0, SearchResult{}, ""); err == nil {
		t.Fatal("oversized dimensions accepted")
	}
}
