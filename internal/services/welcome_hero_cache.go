package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	welcomeHeroDirName   = "welcome_hero"
	welcomeHeroMetaName  = "meta.json"
	welcomeHeroStillName = "still.jpg"
	welcomeHeroSize      = "w1280"
	welcomeHeroOrigSize  = "original"
	welcomeHeroTimeout   = 8 * time.Second
	welcomeHeroMaxBytes  = 4 << 20
	welcomeKicker        = "YUNHAI · CINEMA"
	welcomeHeroPool      = "youth-v1"
	welcomeMinLuma       = 90.0
)

type WelcomeHero struct {
	Bytes    []byte
	Filename string
	TMDBID   int
	Date     string
	Size     string
	Pool     string
}

type welcomeHeroMeta struct {
	Date      string `json:"date"`
	TMDBID    int    `json:"tmdb_id"`
	MediaType string `json:"media_type"`
	Path      string `json:"backdrop_path"`
	Size      string `json:"size"`
	Filename  string `json:"filename"`
	Pool      string `json:"pool"`
}

type WelcomeHeroCache struct {
	dir        string
	tmdb       *TMDBClient
	imageBase  string
	httpClient *http.Client
	now        func() time.Time
	loc        *time.Location
	fallback   []byte
	pick       func(n int) int
	mu         sync.Mutex
}

func shanghaiLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

func NewWelcomeHeroCache(dataDir string, tmdb *TMDBClient, fallback []byte) *WelcomeHeroCache {
	dir := filepath.Join(dataDir, welcomeHeroDirName)
	_ = os.MkdirAll(dir, 0755)
	return &WelcomeHeroCache{
		dir:        dir,
		tmdb:       tmdb,
		imageBase:  TMDBImageBaseURL,
		httpClient: &http.Client{Timeout: welcomeHeroTimeout},
		now:        time.Now,
		loc:        shanghaiLocation(),
		fallback:   fallback,
		pick: func(n int) int {
			if n <= 1 {
				return 0
			}
			return int(time.Now().UnixNano() % int64(n))
		},
	}
}

func (c *WelcomeHeroCache) shanghaiDate() string {
	now := time.Now()
	if c.now != nil {
		now = c.now()
	}
	return now.In(c.loc).Format("2006-01-02")
}

func (c *WelcomeHeroCache) metaPath() string  { return filepath.Join(c.dir, welcomeHeroMetaName) }
func (c *WelcomeHeroCache) stillPath() string { return filepath.Join(c.dir, welcomeHeroStillName) }

func (c *WelcomeHeroCache) Get() WelcomeHero {
	c.mu.Lock()
	defer c.mu.Unlock()
	today := c.shanghaiDate()
	if hero, ok := c.loadIfDate(today); ok {
		return hero
	}
	if hero, err := c.fetchAndStore(today); err == nil && len(hero.Bytes) > 0 {
		return hero
	} else if err != nil {
		logger.Info("[WelcomeHero] fetch failed: %v", err)
	}
	if hero, ok := c.loadAny(); ok {
		logger.Info("[WelcomeHero] using last cache date=%s tmdb=%d", hero.Date, hero.TMDBID)
		return hero
	}
	return WelcomeHero{Bytes: append([]byte(nil), c.fallback...), Filename: "welcome_hero.png"}
}

func (c *WelcomeHeroCache) loadIfDate(date string) (WelcomeHero, bool) {
	hero, ok := c.loadAny()
	if !ok || hero.Date != date || hero.Pool != welcomeHeroPool {
		return WelcomeHero{}, false
	}
	return hero, true
}

func (c *WelcomeHeroCache) loadAny() (WelcomeHero, bool) {
	raw, err := os.ReadFile(c.metaPath())
	if err != nil {
		return WelcomeHero{}, false
	}
	var meta welcomeHeroMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return WelcomeHero{}, false
	}
	data, err := os.ReadFile(c.stillPath())
	if err != nil || len(data) == 0 {
		return WelcomeHero{}, false
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return WelcomeHero{}, false
	}
	name := meta.Filename
	if name == "" {
		name = welcomeHeroStillName
	}
	return WelcomeHero{Bytes: data, Filename: name, TMDBID: meta.TMDBID, Date: meta.Date, Size: meta.Size, Pool: meta.Pool}, true
}

type backdropCandidate struct {
	ID        int
	MediaType string
	Path      string
	Lang      string
}

func (c *WelcomeHeroCache) fetchAndStore(today string) (WelcomeHero, error) {
	if c.tmdb == nil {
		return WelcomeHero{}, fmt.Errorf("tmdb client is nil")
	}
	candidates := c.listBackdrops()
	if len(candidates) == 0 {
		return WelcomeHero{}, fmt.Errorf("no backdrop candidates")
	}
	start := 0
	if c.pick != nil {
		start = c.pick(len(candidates))
	}
	if start < 0 || start >= len(candidates) {
		start = 0
	}
	var chosen backdropCandidate
	var data []byte
	var size string
	var err error
	for i := 0; i < len(candidates); i++ {
		chosen = candidates[(start+i)%len(candidates)]
		data, size, err = c.downloadBackdrop(chosen.Path)
		if err == nil {
			break
		}
		logger.Info("[WelcomeHero] skip tmdb=%d: %v", chosen.ID, err)
	}
	if err != nil || len(data) == 0 {
		return WelcomeHero{}, fmt.Errorf("no usable youth still")
	}
	if stamped := stampCinemaKicker(data); len(stamped) > 0 {
		data = stamped
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return WelcomeHero{}, fmt.Errorf("decoded still invalid: %w", err)
	}
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return WelcomeHero{}, err
	}
	if err := atomicWriteFile(c.stillPath(), data, 0644); err != nil {
		return WelcomeHero{}, err
	}
	meta := welcomeHeroMeta{Date: today, TMDBID: chosen.ID, MediaType: chosen.MediaType, Path: chosen.Path, Size: size, Filename: welcomeHeroStillName, Pool: welcomeHeroPool}
	raw, _ := json.Marshal(meta)
	if err := atomicWriteFile(c.metaPath(), raw, 0644); err != nil {
		return WelcomeHero{}, err
	}
	logger.Info("[WelcomeHero] cached date=%s tmdb=%d size=%s bytes=%d", today, chosen.ID, size, len(data))
	return WelcomeHero{Bytes: data, Filename: welcomeHeroStillName, TMDBID: chosen.ID, Date: today, Size: size, Pool: welcomeHeroPool}, nil
}

func (c *WelcomeHeroCache) listBackdrops() []backdropCandidate {
	out := make([]backdropCandidate, 0, 40)
	seen := map[string]bool{}
	add := func(m TMDBTrendingMediaInfo, mediaType string) {
		if !youthStillOK(m) {
			return
		}
		path := strings.TrimSpace(m.BackdropPath)
		key := fmt.Sprintf("%s:%d", mediaType, m.ID)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, backdropCandidate{ID: m.ID, MediaType: mediaType, Path: path, Lang: m.OriginalLanguage})
	}
	if trending, err := c.tmdb.GetTrendingMovies("day"); err == nil && trending != nil {
		for _, m := range trending.Results {
			add(m, "movie")
		}
	}
	if trending, err := c.tmdb.GetTrendingTV("day"); err == nil && trending != nil {
		for _, m := range trending.Results {
			mt := m.MediaType
			if mt == "" {
				mt = "tv"
			}
			add(m, mt)
		}
	}
	if popular, err := c.tmdb.GetPopularMovies(1); err == nil && popular != nil {
		for _, m := range popular.Results {
			add(m, "movie")
		}
	}
	if len(out) == 0 {
		c.appendDiscoverYouth(&out, seen)
	}
	preferYouthLanguage(out)
	return out
}

func (c *WelcomeHeroCache) downloadBackdrop(path string) ([]byte, string, error) {
	for _, size := range []string{welcomeHeroSize, welcomeHeroOrigSize} {
		url := fmt.Sprintf("%s/%s%s", strings.TrimRight(c.imageBase, "/"), size, path)
		data, err := c.getImage(url)
		if err != nil {
			logger.Info("[WelcomeHero] download %s failed: %v", size, err)
			continue
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			continue
		}
		b := img.Bounds()
		if b.Dy() > b.Dx() {
			continue
		}
		if size == welcomeHeroSize && b.Dx() < 1280 {
			continue
		}
		if b.Dx() < 640 || b.Dy() < 360 {
			continue
		}
		if !stillBrightEnough(img) {
			continue
		}
		return data, size, nil
	}
	return nil, "", fmt.Errorf("no usable landscape still for %s", path)
}

func (c *WelcomeHeroCache) getImage(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "YiMaoWelcomeHero/1.0")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, welcomeHeroMaxBytes))
	if err != nil {
		return nil, err
	}
	if len(data) < 32 {
		return nil, fmt.Errorf("too small")
	}
	return data, nil
}

func stampCinemaKicker(src []byte) []byte {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil
	}
	b := img.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, img, b.Min, draw.Src)
	padX, padY := 28, 22
	label := welcomeKicker
	face := basicfont.Face7x13
	width := len(label)*face.Advance + 24
	height := 28
	rect := image.Rect(padX, padY, padX+width, padY+height)
	draw.Draw(dst, rect, image.NewUniform(color.NRGBA{R: 8, G: 6, B: 4, A: 170}), image.Point{}, draw.Over)
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(color.NRGBA{R: 232, G: 214, B: 176, A: 255}), Face: face, Dot: fixed.P(padX+12, padY+18)}
	d.DrawString(label)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil
	}
	return buf.Bytes()
}

var youthGenreIDs = map[int]bool{35: true, 18: true, 10749: true}
var bannedGenreIDs = map[int]bool{27: true, 53: true, 80: true, 99: true, 10752: true, 10768: true, 10770: true}

func youthStillOK(m TMDBTrendingMediaInfo) bool {
	if m.Adult || m.ID == 0 || !strings.HasPrefix(strings.TrimSpace(m.BackdropPath), "/") {
		return false
	}
	ids := append([]int(nil), m.GenreIds...)
	for _, g := range m.Genres {
		ids = append(ids, g.ID)
		name := strings.ToLower(g.Name)
		if strings.Contains(name, "horror") || strings.Contains(name, "thriller") || strings.Contains(name, "war") || strings.Contains(name, "crime") || strings.Contains(name, "documentary") || strings.Contains(name, "恐怖") || strings.Contains(name, "惊悚") || strings.Contains(name, "战争") || strings.Contains(name, "犯罪") || strings.Contains(name, "纪录") {
			return false
		}
	}
	hasYouth := false
	for _, id := range ids {
		if bannedGenreIDs[id] {
			return false
		}
		if youthGenreIDs[id] {
			hasYouth = true
		}
	}
	return hasYouth
}

func preferYouthLanguage(items []backdropCandidate) {
	if len(items) < 2 {
		return
	}
	n := 0
	for i, it := range items {
		if youthLangPreferred(it.Lang) {
			items[n], items[i] = items[i], items[n]
			n++
		}
	}
}

func youthLangPreferred(lang string) bool {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "zh", "zh-cn", "zh-tw", "cn", "ja", "ko":
		return true
	default:
		return false
	}
}

func stillBrightEnough(img image.Image) bool {
	b := img.Bounds()
	var sum, n float64
	for y := b.Min.Y; y < b.Max.Y; y += 8 {
		for x := b.Min.X; x < b.Max.X; x += 8 {
			r, g, bl, _ := img.At(x, y).RGBA()
			sum += 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(bl>>8)
			n++
		}
	}
	return n > 0 && sum/n >= welcomeMinLuma
}

func (c *WelcomeHeroCache) appendDiscoverYouth(out *[]backdropCandidate, seen map[string]bool) {
	if c.tmdb == nil {
		return
	}
	u, err := url.Parse(strings.TrimRight(c.tmdb.baseURL, "/") + "/discover/movie")
	if err != nil {
		return
	}
	q := u.Query()
	q.Set("api_key", c.tmdb.apiKey)
	q.Set("language", "zh-CN")
	q.Set("include_adult", "false")
	q.Set("sort_by", "popularity.desc")
	q.Set("with_genres", "35|18|10749")
	q.Set("without_genres", "27,53,80,99,10752")
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return
	}
	resp, err := c.tmdb.httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	var result TMDBPopularResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return
	}
	for _, m := range result.Results {
		if !youthStillOK(m) {
			continue
		}
		key := fmt.Sprintf("movie:%d", m.ID)
		if seen[key] {
			continue
		}
		seen[key] = true
		*out = append(*out, backdropCandidate{ID: m.ID, MediaType: "movie", Path: m.BackdropPath, Lang: m.OriginalLanguage})
	}
}
