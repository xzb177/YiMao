package services

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	searchCardWidth       = 900
	searchCardHeight      = 1350
	searchCardGradient    = 690 // Gradual information fade starts below the main artwork focus.
	searchCardSafeInset   = 72  // 8% of the Telegram image width.
	searchPosterMaxBytes  = 6 * 1024 * 1024
	searchPosterMaxPixels = 20_000_000
)

var searchCardSlots = make(chan struct{}, 4)

// SearchVisualCard is a JPEG that contains all metadata users need even when a
// Telegram client chooses not to render InputRichBlockPhoto.caption.
type SearchVisualCard struct {
	ResultIndex int
	JPEG        []byte
}

// BuildSearchVisualCards downloads and composites posters concurrently. It
// makes no MoviePilot detail calls; status is supplied from the one cache index.
func BuildSearchVisualCards(results []SearchResult, subscribed map[int]struct{}) []SearchVisualCard {
	limit := len(results)
	if limit > 8 {
		limit = 8
	}
	cards := make([]SearchVisualCard, limit)
	valid := make([]bool, limit)
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !trustedSearchPosterURL(req.URL) {
			return fmt.Errorf("untrusted poster redirect")
		}
		return nil
	}}
	var wg sync.WaitGroup
	for i := 0; i < limit; i++ {
		poster := normalizeSearchPoster(results[i].Poster)
		if poster == "" {
			continue
		}
		wg.Add(1)
		go func(i int, poster string) {
			defer wg.Done()
			searchCardSlots <- struct{}{}
			defer func() { <-searchCardSlots }()
			resp, err := client.Get(poster)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return
			}
			contentType := strings.ToLower(resp.Header.Get("Content-Type"))
			if contentType != "" && !strings.HasPrefix(contentType, "image/") {
				return
			}
			data, err := io.ReadAll(io.LimitReader(resp.Body, searchPosterMaxBytes+1))
			if err != nil || len(data) > searchPosterMaxBytes {
				return
			}
			status := "点详情查看状态"
			if _, ok := subscribed[results[i].ID]; ok {
				status = "站内追更"
			}
			card, err := RenderSearchVisualCard(data, i, results[i], status)
			if err == nil {
				cards[i] = SearchVisualCard{ResultIndex: i, JPEG: card}
				valid[i] = true
			}
		}(i, poster)
	}
	wg.Wait()
	out := make([]SearchVisualCard, 0, limit)
	for i := range cards {
		if valid[i] {
			out = append(out, cards[i])
		}
	}
	return out
}

func normalizeSearchPoster(poster string) string {
	poster = strings.TrimSpace(poster)
	if poster == "" {
		return ""
	}
	if strings.HasPrefix(poster, "http://") || strings.HasPrefix(poster, "https://") {
		u, err := url.Parse(poster)
		if err != nil || !trustedSearchPosterURL(u) {
			return ""
		}
		return u.String()
	}
	return "https://image.tmdb.org/t/p/w780/" + strings.TrimLeft(poster, "/")
}

func trustedSearchPosterURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && strings.EqualFold(u.Hostname(), "image.tmdb.org") && u.User == nil
}

// RenderSearchVisualCard renders a deterministic 2:3 JPEG. Exported for image
// contract tests and for future transports that already have poster bytes.
func RenderSearchVisualCard(poster []byte, index int, item SearchResult, status string) ([]byte, error) {
	_ = index // Ordering belongs to the carousel UI, not the editorial artwork.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(poster))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8000 || cfg.Height > 8000 || int64(cfg.Width)*int64(cfg.Height) > searchPosterMaxPixels {
		return nil, fmt.Errorf("unsafe poster dimensions")
	}
	src, _, err := image.Decode(bytes.NewReader(poster))
	if err != nil {
		return nil, fmt.Errorf("decode poster: %w", err)
	}
	faces, err := loadSearchCardFaces()
	if err != nil {
		return nil, err
	}
	defer faces.close()

	palette := extractSearchCardPalette(src)
	canvas := image.NewRGBA(image.Rect(0, 0, searchCardWidth, searchCardHeight))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: palette.deep}, image.Point{}, draw.Src)
	// Use one continuous artwork plane. Most TMDB posters are already 2:3, so
	// this preserves the complete poster; unusual ratios receive only a centred
	// cover crop. Do not add blurred side extensions or alpha-feathered seams:
	// both made the production card look assembled and could introduce colour
	// fringes at the blend boundary.
	artwork := image.Rect(0, 0, searchCardWidth, searchCardHeight)
	xdraw.CatmullRom.Scale(canvas, artwork, src, centerCropBounds(src.Bounds(), artwork.Dx(), artwork.Dy()), draw.Src, nil)

	// A single continuous tonal gradient carries the information. The easing is
	// intentionally gradual so poster art and metadata remain one visual object,
	// without a panel edge or horizontal separator.
	gradientStart := searchCardGradient
	for y := gradientStart; y < searchCardHeight; y++ {
		t := float64(y-gradientStart) / float64(searchCardHeight-gradientStart-1)
		eased := t * t * (3 - 2*t) // smoothstep
		alpha := uint8(250 * eased)
		overlay := color.NRGBA{R: palette.deep.R, G: palette.deep.G, B: palette.deep.B, A: alpha}
		draw.Draw(canvas, image.Rect(0, y, searchCardWidth, y+1), &image.Uniform{C: overlay}, image.Point{}, draw.Over)
	}

	x := searchCardSafeInset
	// Typography follows the artwork palette instead of sitting on top as fixed
	// white/grey UI. Each role uses a different white mix, then lightens only as
	// much as needed to preserve contrast against the final dark gradient.
	titleColor := paletteTextColor(palette.accent, palette.deep, .72, 7)
	drawWrapped(canvas, faces.title, strings.TrimSpace(item.Title), x, 980, searchCardWidth-2*searchCardSafeInset, 51, titleColor, 2)

	metadata := make([]string, 0, 3)
	if item.Year > 0 {
		metadata = append(metadata, fmt.Sprintf("%d", item.Year))
	}
	if item.Type == "tv" || item.Type == "电视剧" {
		metadata = append(metadata, "剧集")
	} else {
		metadata = append(metadata, "电影")
	}
	if item.Rating > 0 {
		metadata = append(metadata, fmt.Sprintf("★ %.1f", item.Rating))
	}
	metaColor := paletteTextColor(palette.accent, palette.deep, .48, 4.5)
	drawText(canvas, faces.meta, strings.Join(metadata, "  ·  "), x, 1087, metaColor)

	pillText := ensureTextContrast(palette.accent, palette.pill, 4.5)
	pillWidth := font.MeasureString(faces.status, status).Ceil() + 40
	drawRoundedRect(canvas, image.Rect(x, 1107, x+pillWidth, 1157), 25, palette.pill)
	drawText(canvas, faces.status, status, x+20, 1142, pillText)

	overview := compactCardText(item.Overview, 120)
	if overview == "" {
		overview = "暂无简介"
	}
	bodyColor := paletteTextColor(palette.accent, palette.deep, .62, 4.5)
	drawWrapped(canvas, faces.body, overview, x, 1192, searchCardWidth-2*searchCardSafeInset, 34, bodyColor, 3)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, canvas, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type cardFaces struct{ title, meta, status, body font.Face }

type searchCardPalette struct {
	accent color.RGBA
	deep   color.RGBA
	pill   color.RGBA
}

// extractSearchCardPalette follows the overall artwork colour rather than one
// saturated logo or mark. The averaged colour is gently warmed, brightened and
// saturation-clamped so the status pill remains editorial instead of neon.
func extractSearchCardPalette(src image.Image) searchCardPalette {
	b := src.Bounds()
	var sumR, sumG, sumB, count uint64
	for gy := 0; gy < 12; gy++ {
		for gx := 0; gx < 10; gx++ {
			x := b.Min.X + (2*gx+1)*b.Dx()/20
			y := b.Min.Y + (2*gy+1)*b.Dy()/24
			r16, g16, b16, _ := src.At(x, y).RGBA()
			r, g, bl := uint8(r16>>8), uint8(g16>>8), uint8(b16>>8)
			sumR, sumG, sumB, count = sumR+uint64(r), sumG+uint64(g), sumB+uint64(bl), count+1
		}
	}
	avgR, avgG, avgB := uint8(sumR/count), uint8(sumG/count), uint8(sumB/count)
	accent := restrainedAccent(avgR, avgG, avgB)
	deep := color.RGBA{uint8(10 + int(avgR)*8/100), uint8(11 + int(avgG)*8/100), uint8(13 + int(avgB)*8/100), 255}
	pill := color.RGBA{uint8((int(deep.R)*3 + int(accent.R)) / 4), uint8((int(deep.G)*3 + int(accent.G)) / 4), uint8((int(deep.B)*3 + int(accent.B)) / 4), 255}
	return searchCardPalette{accent: accent, deep: deep, pill: pill}
}

func restrainedAccent(r, g, b uint8) color.RGBA {
	rf := .82*float64(r) + .18*176
	gf := .82*float64(g) + .18*154
	bf := .82*float64(b) + .18*126
	maxV := math.Max(rf, math.Max(gf, bf))
	if maxV < 168 {
		s := 168 / math.Max(maxV, 1)
		rf, gf, bf = rf*s, gf*s, bf*s
	}
	clamp := func(v float64) uint8 {
		if v < 72 {
			return 72
		}
		if v > 205 {
			return 205
		}
		return uint8(v)
	}
	return color.RGBA{clamp(rf), clamp(gf), clamp(bf), 255}
}

func relativeLuminance(c color.RGBA) float64 {
	linear := func(v uint8) float64 {
		n := float64(v) / 255
		if n <= .04045 {
			return n / 12.92
		}
		return math.Pow((n+.055)/1.055, 2.4)
	}
	return .2126*linear(c.R) + .7152*linear(c.G) + .0722*linear(c.B)
}

func contrastRatio(a, b color.RGBA) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + .05) / (lb + .05)
}

func ensureTextContrast(preferred, background color.RGBA, minimum float64) color.RGBA {
	if contrastRatio(preferred, background) >= minimum {
		return preferred
	}
	white, black := color.RGBA{255, 255, 255, 255}, color.RGBA{8, 9, 11, 255}
	if contrastRatio(white, background) >= contrastRatio(black, background) {
		return white
	}
	return black
}

func paletteTextColor(accent, background color.RGBA, whiteMix, minimum float64) color.RGBA {
	if whiteMix < 0 {
		whiteMix = 0
	}
	if whiteMix > 1 {
		whiteMix = 1
	}
	mix := func(amount float64) color.RGBA {
		blend := func(channel uint8) uint8 {
			return uint8(math.Round(float64(channel)*(1-amount) + 255*amount))
		}
		return color.RGBA{blend(accent.R), blend(accent.G), blend(accent.B), 255}
	}
	for amount := whiteMix; amount <= 1.0001; amount += .04 {
		candidate := mix(math.Min(amount, 1))
		if contrastRatio(candidate, background) >= minimum {
			return candidate
		}
	}
	return ensureTextContrast(mix(1), background, minimum)
}

func drawRoundedRect(dst draw.Image, rect image.Rectangle, radius int, c color.Color) {
	r2 := radius * radius
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			dx, dy := 0, 0
			if x < rect.Min.X+radius {
				dx = rect.Min.X + radius - x
			}
			if x >= rect.Max.X-radius {
				dx = x - (rect.Max.X - radius - 1)
			}
			if y < rect.Min.Y+radius {
				dy = rect.Min.Y + radius - y
			}
			if y >= rect.Max.Y-radius {
				dy = y - (rect.Max.Y - radius - 1)
			}
			if dx*dx+dy*dy <= r2 {
				dst.Set(x, y, c)
			}
		}
	}
}

func maxByte(a, b uint8) uint8 {
	if a > b {
		return a
	}
	return b
}
func minByte(a, b uint8) uint8 {
	if a < b {
		return a
	}
	return b
}

var (
	searchCardFontOnce   sync.Once
	searchCardParsedFont *opentype.Font
	searchCardFontErr    error
)

func (f cardFaces) close() {
	_ = f.title.Close()
	_ = f.meta.Close()
	_ = f.status.Close()
	_ = f.body.Close()
}

func loadSearchCardFaces() (cardFaces, error) {
	searchCardFontOnce.Do(func() {
		paths := []string{os.Getenv("YIMAO_CJK_FONT"), "/usr/share/fonts/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc", "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc", "/usr/share/fonts/opentype/unifont/unifont.otf"}
		var raw []byte
		var picked string
		for _, path := range paths {
			if path == "" {
				continue
			}
			if data, err := os.ReadFile(filepath.Clean(path)); err == nil {
				raw, picked = data, path
				break
			}
		}
		if raw == nil {
			searchCardFontErr = fmt.Errorf("CJK font not found; set YIMAO_CJK_FONT")
			return
		}
		if strings.HasSuffix(strings.ToLower(picked), ".ttc") {
			collection, err := opentype.ParseCollection(raw)
			if err != nil {
				searchCardFontErr = fmt.Errorf("parse CJK font collection: %w", err)
				return
			}
			searchCardParsedFont, searchCardFontErr = collection.Font(0)
		} else {
			searchCardParsedFont, searchCardFontErr = opentype.Parse(raw)
		}
	})
	if searchCardFontErr != nil {
		return cardFaces{}, searchCardFontErr
	}
	face := func(size float64) (font.Face, error) {
		return opentype.NewFace(searchCardParsedFont, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	}
	title, err := face(46.2)
	if err != nil {
		return cardFaces{}, err
	}
	meta, err := face(33)
	if err != nil {
		_ = title.Close()
		return cardFaces{}, err
	}
	status, err := face(26.4)
	if err != nil {
		_ = title.Close()
		_ = meta.Close()
		return cardFaces{}, err
	}
	body, err := face(30.8)
	if err != nil {
		_ = title.Close()
		_ = meta.Close()
		_ = status.Close()
		return cardFaces{}, err
	}
	return cardFaces{title: title, meta: meta, status: status, body: body}, nil
}

func drawText(dst draw.Image, face font.Face, text string, x, baseline int, c color.Color) {
	(&font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, baseline)}).DrawString(text)
}

func drawWrapped(dst draw.Image, face font.Face, text string, x, baseline, width, lineHeight int, c color.Color, maxLines int) int {
	lines := wrapCardText(face, text, width, maxLines)
	for _, line := range lines {
		drawText(dst, face, line, x, baseline, c)
		baseline += lineHeight
	}
	return baseline
}

func wrapCardText(face font.Face, text string, width, maxLines int) []string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	lines := make([]string, 0, maxLines)
	for len(runes) > 0 && len(lines) < maxLines {
		n := 1
		for n <= len(runes) && font.MeasureString(face, string(runes[:n])).Ceil() <= width {
			n++
		}
		n--
		if n < 1 {
			n = 1
		}
		// Basic CJK kinsoku: do not leave closing punctuation at the start of
		// the next line. Move one preceding rune with it instead of overfilling
		// the current line, which keeps the measured width contract intact.
		if n < len(runes) && n > 1 && isCardLineStartProhibited(runes[n]) {
			n--
		}
		line := strings.TrimSpace(string(runes[:n]))
		runes = runes[n:]
		if len(lines) == maxLines-1 && len(runes) > 0 {
			line = strings.TrimRight(line, "…") + "…"
			runes = nil
		}
		lines = append(lines, line)
	}
	return lines
}

func isCardLineStartProhibited(r rune) bool {
	return strings.ContainsRune("。！？；：，、）》】〕〉」』〗）］…", r)
}

func compactCardText(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

func centerCropBounds(src image.Rectangle, dstWidth, dstHeight int) image.Rectangle {
	if src.Dx() <= 0 || src.Dy() <= 0 || dstWidth <= 0 || dstHeight <= 0 {
		return src
	}
	srcAspect := float64(src.Dx()) / float64(src.Dy())
	dstAspect := float64(dstWidth) / float64(dstHeight)
	if srcAspect > dstAspect {
		cropWidth := int(math.Round(float64(src.Dy()) * dstAspect))
		left := src.Min.X + (src.Dx()-cropWidth)/2
		return image.Rect(left, src.Min.Y, left+cropWidth, src.Max.Y)
	}
	if srcAspect < dstAspect {
		cropHeight := int(math.Round(float64(src.Dx()) / dstAspect))
		top := src.Min.Y + (src.Dy()-cropHeight)/2
		return image.Rect(src.Min.X, top, src.Max.X, top+cropHeight)
	}
	return src
}
