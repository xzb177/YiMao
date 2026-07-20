package services

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
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
