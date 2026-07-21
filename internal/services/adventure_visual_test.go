package services

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func adventureBackdropJPEG(t *testing.T, c1, c2 color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := 0; y < 720; y++ {
		for x := 0; x < 1280; x++ {
			t := float64(x) / 1279
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(float64(c1.R)*(1-t) + float64(c2.R)*t),
				G: uint8(float64(c1.G)*(1-t) + float64(c2.G)*t),
				B: uint8(float64(c1.B)*(1-t) + float64(c2.B)*t), A: 255,
			})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestRenderAdventureVisualSlidesCreatesSceneAndChoiceCards(t *testing.T) {
	t.Setenv("YIMAO_CJK_FONT", "/usr/share/fonts/noto/NotoSansCJK-Regular.ttc")
	backdrops := [][]byte{
		adventureBackdropJPEG(t, color.RGBA{20, 34, 70, 255}, color.RGBA{120, 52, 28, 255}),
		adventureBackdropJPEG(t, color.RGBA{12, 70, 58, 255}, color.RGBA{72, 24, 86, 255}),
	}
	data := AdventureVisualData{
		MovieTitle: "测试影片", MovieYear: 2026, Level: 2, TotalLevels: 5,
		StageName: "转折·选择", SceneTitle: "雨夜走廊", Atmosphere: "诡异",
		Description: "走廊尽头传来脚步声，墙上的应急灯忽明忽暗。",
		Choices:     []string{"躲进值班室观察", "立刻追上脚步声", "拉响整栋楼的警报", "从窗户翻到外墙"},
		HP:          76, Combo: 2, Score: 40,
	}
	slides, err := RenderAdventureVisualSlides(backdrops, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(slides) != 2 {
		t.Fatalf("slides=%d, want 2", len(slides))
	}
	if bytes.Equal(slides[0], slides[1]) {
		t.Fatal("scene and choice slides are identical")
	}
	for i, raw := range slides {
		img, format, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("slide %d decode: %v", i, err)
		}
		if format != "jpeg" || img.Bounds().Dx() != 900 || img.Bounds().Dy() != 1350 {
			t.Fatalf("slide %d contract=%s %v", i, format, img.Bounds())
		}
	}
}

func TestNormalizeAdventureBackdropAllowsOnlyTMDBImages(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"/abc.jpg", "https://image.tmdb.org/t/p/w1280/abc.jpg"},
		{"https://image.tmdb.org/t/p/original/abc.jpg", "https://image.tmdb.org/t/p/original/abc.jpg"},
		{"https://image.tmdb.org:443/t/p/original/abc.jpg", "https://image.tmdb.org:443/t/p/original/abc.jpg"},
		{"https://image.tmdb.org:444/t/p/original/abc.jpg", ""},
		{"http://image.tmdb.org/t/p/original/abc.jpg", ""},
		{"https://evil.example/abc.jpg", ""},
		{"", ""},
	} {
		if got := NormalizeAdventureBackdrop(tc.input); got != tc.want {
			t.Fatalf("NormalizeAdventureBackdrop(%q)=%q, want %q", tc.input, got, tc.want)
		}
	}
}
