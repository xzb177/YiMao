package services

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
)

const (
	adventureCardWidth       = 900
	adventureCardHeight      = 1350
	adventureBackdropMaxSize = 6 * 1024 * 1024
)

var (
	adventureVisualSlots = make(chan struct{}, 2)
	adventureBackdropMu  sync.Mutex
	adventureBackdropTTL = 2 * time.Hour
	adventureBackdropMap = make(map[string]adventureBackdropCacheEntry)
)

type adventureBackdropCacheEntry struct {
	data      []byte
	expiresAt time.Time
}

type AdventureVisualData struct {
	MovieTitle  string
	MovieYear   int
	Level       int
	TotalLevels int
	StageName   string
	SceneTitle  string
	Atmosphere  string
	Description string
	Choices     []string
	HP          int
	Combo       int
	Score       int
}

func NormalizeAdventureBackdrop(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "/") {
		return "https://image.tmdb.org/t/p/w1280/" + strings.TrimLeft(raw, "/")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "image.tmdb.org") || (u.Port() != "" && u.Port() != "443") || u.User != nil {
		return ""
	}
	return u.String()
}

func DownloadAdventureBackdrops(paths []string) [][]byte {
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || NormalizeAdventureBackdrop(req.URL.String()) == "" {
			return fmt.Errorf("untrusted backdrop redirect")
		}
		return nil
	}}
	out := make([][]byte, 0, 2)
	for _, raw := range paths {
		if len(out) == 2 {
			break
		}
		path := NormalizeAdventureBackdrop(raw)
		if path == "" {
			continue
		}
		adventureBackdropMu.Lock()
		cached, ok := adventureBackdropMap[path]
		if ok && time.Now().Before(cached.expiresAt) {
			data := append([]byte(nil), cached.data...)
			adventureBackdropMu.Unlock()
			out = append(out, data)
			continue
		}
		adventureBackdropMu.Unlock()
		resp, err := client.Get(path)
		if err != nil {
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 || (resp.Header.Get("Content-Type") != "" && !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "image/")) {
			resp.Body.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, adventureBackdropMaxSize+1))
		resp.Body.Close()
		if err != nil || len(data) > adventureBackdropMaxSize {
			continue
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8000 || cfg.Height > 8000 || int64(cfg.Width)*int64(cfg.Height) > searchPosterMaxPixels {
			continue
		}
		adventureBackdropMu.Lock()
		if len(adventureBackdropMap) >= 16 {
			for key := range adventureBackdropMap {
				delete(adventureBackdropMap, key)
				break
			}
		}
		adventureBackdropMap[path] = adventureBackdropCacheEntry{data: append([]byte(nil), data...), expiresAt: time.Now().Add(adventureBackdropTTL)}
		adventureBackdropMu.Unlock()
		out = append(out, data)
	}
	return out
}

func RenderAdventureVisualSlides(backdrops [][]byte, data AdventureVisualData) ([][]byte, error) {
	adventureVisualSlots <- struct{}{}
	defer func() { <-adventureVisualSlots }()
	if len(backdrops) == 0 {
		return nil, fmt.Errorf("no adventure backdrops")
	}
	slides := make([][]byte, 0, 2)
	for i := 0; i < 2; i++ {
		raw := backdrops[i%len(backdrops)]
		slide, err := renderAdventureSlide(raw, data, i)
		if err != nil {
			return nil, err
		}
		slides = append(slides, slide)
	}
	return slides, nil
}

func renderAdventureSlide(raw []byte, data AdventureVisualData, variant int) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8000 || cfg.Height > 8000 || int64(cfg.Width)*int64(cfg.Height) > searchPosterMaxPixels {
		return nil, fmt.Errorf("unsafe backdrop dimensions")
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	faces, err := loadSearchCardFaces()
	if err != nil {
		return nil, err
	}
	defer faces.close()
	palette := extractSearchCardPalette(src)
	canvas := image.NewRGBA(image.Rect(0, 0, adventureCardWidth, adventureCardHeight))
	xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), src, centerCropBounds(src.Bounds(), adventureCardWidth, adventureCardHeight), draw.Src, nil)
	for y := 0; y < adventureCardHeight; y++ {
		top := float64(y) / adventureCardHeight
		alpha := uint8(35 + 205*top*top)
		if y < 250 {
			alpha = uint8(120 - y*60/250)
		}
		draw.Draw(canvas, image.Rect(0, y, adventureCardWidth, y+1), &image.Uniform{C: color.NRGBA{R: palette.deep.R, G: palette.deep.G, B: palette.deep.B, A: alpha}}, image.Point{}, draw.Over)
	}
	x := 64
	accent := paletteTextColor(palette.accent, palette.deep, .68, 7)
	white := color.RGBA{245, 247, 250, 255}
	muted := color.RGBA{210, 216, 224, 255}
	progress := fmt.Sprintf("第 %d / %d 关", data.Level, data.TotalLevels)
	drawText(canvas, faces.status, progress, x, 92, accent)
	pageLabel := "场景  1 / 2"
	if variant == 1 {
		pageLabel = "抉择  2 / 2"
	}
	pageWidth := font.MeasureString(faces.status, pageLabel).Ceil()
	drawText(canvas, faces.status, pageLabel, adventureCardWidth-x-pageWidth, 92, accent)
	drawWrapped(canvas, faces.title, strings.TrimSpace(data.MovieTitle), x, 170, 772, 55, white, 2)
	if data.MovieYear > 0 {
		drawText(canvas, faces.meta, fmt.Sprintf("%d", data.MovieYear), x, 238, muted)
	}
	if variant == 0 {
		drawText(canvas, faces.meta, strings.TrimSpace(data.StageName)+"  ·  "+strings.TrimSpace(data.Atmosphere), x, 1035, accent)
		drawWrapped(canvas, faces.title, strings.TrimSpace(data.SceneTitle), x, 1110, 772, 52, white, 2)
		drawWrapped(canvas, faces.body, compactCardText(data.Description, 120), x, 1210, 772, 38, muted, 3)
	} else {
		drawText(canvas, faces.meta, "观察与抉择", x, 720, accent)
		drawWrapped(canvas, faces.title, strings.TrimSpace(data.SceneTitle), x, 795, 772, 52, white, 2)
		base := 905
		for i, choice := range data.Choices {
			if i >= 4 {
				break
			}
			rowTop := base - 45 + i*82
			fill := color.NRGBA{R: palette.deep.R, G: palette.deep.G, B: palette.deep.B, A: 190}
			drawRoundedRect(canvas, image.Rect(x-16, rowTop, adventureCardWidth-x, rowTop+62), 18, fill)
			line := fmt.Sprintf("%d  %s", i+1, compactCardText(choice, 30))
			drawWrapped(canvas, faces.body, line, x, base+i*82, 772, 34, white, 2)
		}
		status := fmt.Sprintf("HP %d%%   ·   连击 x%d   ·   %d 分", data.HP, data.Combo, data.Score)
		drawText(canvas, faces.status, status, x, 1280, accent)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, canvas, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
