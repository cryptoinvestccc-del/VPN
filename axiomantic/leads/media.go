package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png" // PNG uploads, icon sizes
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Pictures uploaded in the panel. Each is decoded and written again as
// JPEG at a few widths: nothing of the original file reaches the site —
// no camera metadata, no location, no payload hidden in the file.

const (
	maxUpload    = 12 << 20
	maxImageSide = 6000
)

var mediaWidths = []int{480, 960, 1600, 2000}

type mediaEntry struct {
	W   int      `json:"w"`
	H   int      `json:"h"`
	JPG [][2]any `json:"jpg"`           // [width, file name], smallest first
	PNG [][2]any `json:"png,omitempty"` // square icon sizes, for a favicon
	// for the media library: when, by whom, the file's own name
	Time time.Time `json:"time,omitempty"`
	Name string    `json:"name,omitempty"`
	By   string    `json:"by,omitempty"`
}

// iconSizes: the tab icon, the phone home screen, the largest Android one.
var iconSizes = []int{32, 180, 512}

func readMediaManifest(sc *SiteConfig) map[string]mediaEntry {
	out := map[string]mediaEntry{}
	raw, err := os.ReadFile(filepath.Join(sc.Data, "media", "manifest.json"))
	if err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func (a *Admin) handleUpload(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+64<<10)
	file, hdr, err := r.FormFile("file")
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			adminJSON(w, http.StatusRequestEntityTooLarge, apiError{Error: "size"})
			return
		}
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "file"})
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxUpload+1))
	if err != nil || len(raw) > maxUpload {
		adminJSON(w, http.StatusRequestEntityTooLarge, apiError{Error: "size"})
		return
	}
	// Look at the header first: a small file can claim a huge picture
	// and exhaust memory when decoded.
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "jpeg" && format != "png") {
		adminJSON(w, http.StatusUnsupportedMediaType, apiError{Error: "format"})
		return
	}
	if cfg.Width < 16 || cfg.Height < 16 || cfg.Width > maxImageSide || cfg.Height > maxImageSide {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "dimensions"})
		return
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		adminJSON(w, http.StatusUnsupportedMediaType, apiError{Error: "format"})
		return
	}
	icon := r.FormValue("kind") == "icon"
	if icon {
		// a favicon is square: the middle of the picture
		img = centerSquare(img)
	}
	dir := filepath.Join(sc.Data, "media")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	id := hex.EncodeToString(randBytes(8))
	b := img.Bounds()
	entry := mediaEntry{W: b.Dx(), H: b.Dy(), Time: a.now().UTC(), Name: uploadName(hdr.Filename), By: ac.user.ID}
	widths := []int{}
	for _, mw := range mediaWidths {
		if mw < b.Dx() {
			widths = append(widths, mw)
		}
	}
	top := b.Dx()
	if top > mediaWidths[len(mediaWidths)-1] {
		top = mediaWidths[len(mediaWidths)-1]
	}
	if len(widths) == 0 || widths[len(widths)-1] != top {
		widths = append(widths, top)
	}
	for _, mw := range widths {
		mh := b.Dy() * mw / b.Dx()
		if mh < 1 {
			mh = 1
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, scaleImage(img, mw, mh), &jpeg.Options{Quality: 82}); err != nil {
			adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
			return
		}
		name := id + "-" + itoa(mw) + ".jpg"
		if err := writeFileAtomic(filepath.Join(dir, name), buf.Bytes()); err != nil {
			adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
			return
		}
		entry.JPG = append(entry.JPG, [2]any{mw, name})
	}
	if icon {
		for _, sz := range iconSizes {
			var buf bytes.Buffer
			if err := png.Encode(&buf, scalePixels(img, sz, sz, true)); err != nil {
				adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
				return
			}
			name := id + "-" + itoa(sz) + ".png"
			if err := writeFileAtomic(filepath.Join(dir, name), buf.Bytes()); err != nil {
				adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
				return
			}
			entry.PNG = append(entry.PNG, [2]any{sz, name})
		}
	}
	// the stated size is the largest file written
	entry.W = widths[len(widths)-1]
	entry.H = b.Dy() * entry.W / b.Dx()
	siteLock.Lock()
	m := readMediaManifest(sc)
	m[id] = entry
	rawM, _ := json.Marshal(m)
	err = writeFileAtomic(filepath.Join(dir, "manifest.json"), rawM)
	siteLock.Unlock()
	if err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	a.activity(sc, activityEntry{By: ac.user.ID, Kind: "upload", Note: entry.Name, Media: id})
	adminJSON(w, http.StatusOK, map[string]any{"id": id, "w": entry.W, "h": entry.H, "thumb": "/admin/media/" + id + "-" + itoa(widths[0]) + ".jpg"})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// scaleImage shrinks by averaging the source pixels under each target
// pixel (a box filter): sharp enough for photos, no ringing. Transparent
// areas of a PNG land on white, as JPEG has no transparency.
func scaleImage(src image.Image, w, h int) *image.RGBA {
	return scalePixels(src, w, h, false)
}

// centerSquare cuts the largest square out of the middle of a picture.
func centerSquare(src image.Image) image.Image {
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	x0 := b.Min.X + (b.Dx()-side)/2
	y0 := b.Min.Y + (b.Dy()-side)/2
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(dst, dst.Bounds(), src, image.Point{x0, y0}, draw.Src)
	return dst
}

// scalePixels is scaleImage; with alpha the transparency survives (for
// PNG icons) instead of landing on white.
func scalePixels(src image.Image, w, h int, alpha bool) *image.RGBA {
	b := src.Bounds()
	rgba := image.NewRGBA(b)
	if alpha {
		draw.Draw(rgba, b, src, b.Min, draw.Src)
	} else {
		draw.Draw(rgba, b, &image.Uniform{color.White}, image.Point{}, draw.Src)
		draw.Draw(rgba, b, src, b.Min, draw.Over)
	}
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, (y+1)*sh/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, (x+1)*sw/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, al, n uint64
			for yy := y0; yy < y1 && yy < sh; yy++ {
				off := rgba.PixOffset(b.Min.X+x0, b.Min.Y+yy)
				for xx := x0; xx < x1 && xx < sw; xx++ {
					r += uint64(rgba.Pix[off])
					g += uint64(rgba.Pix[off+1])
					bl += uint64(rgba.Pix[off+2])
					al += uint64(rgba.Pix[off+3])
					n++
					off += 4
				}
			}
			if n == 0 {
				n = 1
			}
			o := dst.PixOffset(x, y)
			a := uint8(255)
			if alpha {
				a = uint8(al / n)
			}
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = uint8(r/n), uint8(g/n), uint8(bl/n), a
		}
	}
	return dst
}

// ---------------------------------------------------------------- health

type check struct {
	Level   string   `json:"level"` // ok | warn | error
	Title   string   `json:"title"`
	Details []string `json:"details"`
}

var reHref = regexp.MustCompile(`(?:href|src)="([^"#?]*)`)

// handleHealth looks the site over: the draft against the schema, the
// last build, pictures still missing, links inside the published site that
// lead nowhere, and search snippets that are too long.
func (a *Admin) handleHealth(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	siteLock.Lock()
	defer siteLock.Unlock()
	checks := []check{}
	schema, err := a.schemaOf(sc)
	d, derr := a.loadDraft(r.Context(), sc)
	switch {
	case err != nil || derr != nil:
		checks = append(checks, check{Level: "error", Title: "Черновик или схема не читаются", Details: []string{}})
	default:
		bad := []string{}
		for _, sec := range schema.Sections {
			vals := map[string]any{}
			for _, f := range sec.Fields {
				v := getPath(d.Content, f.Key)
				// an object holds keys other sections edit (SITE.operator):
				// check only this section's own
				if obj, ok := v.(map[string]any); ok && f.Type == "object" {
					own := map[string]any{}
					for _, k := range f.Fields {
						if x, has := obj[k.Key]; has {
							own[k.Key] = x
						}
					}
					v = own
				}
				vals[f.Key] = v
			}
			if _, err := schema.ApplySection(d.Content, sec.ID, vals, a.mediaExists(sc)); err != nil {
				bad = append(bad, sec.Title+": "+err.Error())
			}
		}
		if len(bad) > 0 {
			checks = append(checks, check{Level: "error", Title: "В черновике есть ошибки", Details: bad})
		} else {
			checks = append(checks, check{Level: "ok", Title: "Черновик заполнен правильно", Details: []string{}})
		}
		long := []string{}
		if cases, ok := d.Content["CASES"].([]any); ok {
			for _, c := range cases {
				m, _ := c.(map[string]any)
				if t, _ := m["title"].(string); utf8.RuneCountInString(t) > 60 {
					long = append(long, "Кейс «"+t+"»: заголовок длиннее 60 символов — в поиске обрежется")
				}
			}
		}
		if arts, ok := d.Content["ARTICLES"].([]any); ok {
			for _, x := range arts {
				m, _ := x.(map[string]any)
				t, _ := m["title"].(string)
				if desc, _ := m["desc"].(string); utf8.RuneCountInString(desc) > 160 {
					long = append(long, "Статья «"+t+"»: описание длиннее 160 символов")
				}
			}
		}
		if site, ok := d.Content["SITE"].(map[string]any); ok && site["demo"] == true {
			long = append(long, "Включена плашка «Демо» — выключите её в «Контакты, меню, реквизиты», когда содержимое станет настоящим")
		}
		if len(long) > 0 {
			checks = append(checks, check{Level: "warn", Title: "Поиск и запуск", Details: long})
		}
	}
	if lb := a.lastBuild(sc); lb != nil {
		switch {
		case !lb.OK:
			checks = append(checks, check{Level: "error", Title: "Последняя сборка не удалась", Details: []string{lastLines(lb.Log, 12)}})
		case len(lb.Missing) > 0:
			checks = append(checks, check{Level: "warn", Title: "Картинки, которых пока нет (на сайте заглушки)", Details: lb.Missing})
		default:
			checks = append(checks, check{Level: "ok", Title: "Последняя сборка прошла", Details: []string{}})
		}
	}
	if id := currentRelease(sc); id == "" {
		checks = append(checks, check{Level: "warn", Title: "Сайт ещё ни разу не публиковался из панели", Details: []string{}})
	} else {
		broken := brokenLinks(filepath.Join(sc.Root, "releases", id))
		if len(broken) > 0 {
			checks = append(checks, check{Level: "error", Title: "Ссылки, которые никуда не ведут", Details: broken})
		} else {
			checks = append(checks, check{Level: "ok", Title: "Все внутренние ссылки опубликованного сайта работают", Details: []string{}})
		}
	}
	adminJSON(w, http.StatusOK, map[string]any{"checks": checks})
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// brokenLinks lists relative links and pictures in the site's pages whose
// target file is missing.
func brokenLinks(root string) []string {
	out := []string{}
	seen := map[string]bool{}
	_ = filepath.WalkDir(root, func(p string, de os.DirEntry, err error) error {
		if err != nil || de.IsDir() || !strings.HasSuffix(p, ".html") || len(out) >= 50 {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for _, m := range reHref.FindAllSubmatch(raw, -1) {
			ref := string(m[1])
			if ref == "" || strings.Contains(ref, ":") || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "//") {
				continue
			}
			target := filepath.Join(filepath.Dir(p), filepath.FromSlash(ref))
			if st, err := os.Stat(target); err == nil && st.IsDir() {
				target = filepath.Join(target, "index.html")
			}
			if _, err := os.Stat(target); err != nil {
				page, _ := filepath.Rel(root, p)
				key := page + " → " + ref
				if !seen[key] {
					seen[key] = true
					out = append(out, key)
				}
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}
