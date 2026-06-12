package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	archiveAPIRatePerSec   = 2 // gate Flickr API calls (matches syncRatePerSec)
	archiveWorkers         = 3 // concurrent CDN downloads per photo
	archiveDownloadsPerSec = 5 // GLOBAL cap on CDN download starts/sec — the real 429 guard
	archiveHTTPTimeout     = 120 * time.Second
	archiveMaxAttempts     = 4 // download retries on 429 / transient network errors
)

type archiveOpts struct {
	Root            string
	Limit           int
	OnlyID          string
	SkipOriginal    bool
	OnlyOriginal    bool
	SkipXLarge      bool // skip 3k/4k/5k/6k (Flickr rate-limits these hard; reproducible from original)
	DownloadsPerSec int  // global download rate (0 = default archiveDownloadsPerSec)
}

// throttleAbortStreak: if this many photos in a row download nothing but fail
// (every asset errored, typically HTTP 429), assume Flickr is throttling and
// abort the run so a supervising loop can wait and retry later.
const throttleAbortStreak = 20

// isXLargeSuffix reports whether a size suffix is an X-Large (3k/4k/5k/6k…),
// i.e. a digit followed by 'k'. Plain "k" (Large 2048) is NOT X-Large.
func isXLargeSuffix(s string) bool {
	return len(s) >= 2 && s[len(s)-1] == 'k' && s[0] >= '0' && s[0] <= '9'
}

// resolveArchiveRoot picks the archive root: flag > IMAGE_ARCHIVE_DIR > "./archive".
func resolveArchiveRoot(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if env := os.Getenv("IMAGE_ARCHIVE_DIR"); env != "" {
		return env
	}
	return "./archive"
}

// suffixFromSource parses the authoritative Flickr size suffix from a CDN URL.
// Basenames look like "{id}_{secret}_{suffix}.{ext}" (e.g. _q, _m, _b, _3k, _o),
// or "{id}_{secret}.{ext}" for Medium 500 which has no suffix letter. Parsing the
// URL avoids a brittle Label→suffix table and matches Flickr exactly. The served
// letters (b/q/m) therefore map identically to the on-disk filenames.
func suffixFromSource(src string) string {
	if i := strings.IndexAny(src, "?#"); i >= 0 {
		src = src[:i]
	}
	if i := strings.LastIndex(src, "/"); i >= 0 {
		src = src[i+1:] // basename
	}
	if i := strings.LastIndex(src, "."); i >= 0 {
		src = src[:i] // drop extension
	}
	parts := strings.Split(src, "_")
	if len(parts) >= 3 {
		return parts[len(parts)-1] // id_secret_suffix → suffix
	}
	return "m500" // id_secret → Medium 500 (no native suffix letter)
}

// extFromURL returns the lowercase file extension of a URL path, defaulting to ".jpg".
func extFromURL(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	if ext := strings.ToLower(filepath.Ext(u)); ext != "" {
		return ext
	}
	return ".jpg"
}

type manifestSize struct {
	Label  string `json:"label"`
	Suffix string `json:"suffix"`
	Width  string `json:"width,omitempty"`
	Height string `json:"height,omitempty"`
	Source string `json:"source"`
	File   string `json:"file"`
	Bytes  int64  `json:"bytes"`
}

type manifest struct {
	PhotoID     string         `json:"photo_id"`
	FetchedAt   string         `json:"fetched_at"`
	Candownload int64          `json:"candownload"`
	Sizes       []manifestSize `json:"sizes"`
}

// runArchive downloads every Flickr size (and the original) for all photos into
// {root}/photos/{id}/{suffix}.{ext}, with a per-photo sizes.json manifest.
// Resumable: existing non-empty files are skipped. Failures are logged, never fatal.
func runArchive(app *App, opts archiveOpts) error {
	if opts.SkipOriginal && opts.OnlyOriginal {
		return fmt.Errorf("不能同時指定 -skip-original 與 -only-original")
	}
	if err := os.MkdirAll(filepath.Join(opts.Root, "photos"), 0o755); err != nil {
		return fmt.Errorf("建立存檔根目錄失敗: %w", err)
	}

	ids, err := collectPhotoIDs(app)
	if err != nil {
		return err
	}
	if opts.OnlyID != "" {
		ids = []string{opts.OnlyID}
	} else if opts.Limit > 0 && opts.Limit < len(ids) {
		ids = ids[:opts.Limit]
	}
	log.Printf("Archive: 根目錄=%s 照片數=%d skipOrig=%v onlyOrig=%v", opts.Root, len(ids), opts.SkipOriginal, opts.OnlyOriginal)

	hc := &http.Client{Timeout: archiveHTTPTimeout}
	rate := time.NewTicker(time.Second / archiveAPIRatePerSec)
	defer rate.Stop()
	dlRate := opts.DownloadsPerSec
	if dlRate <= 0 {
		dlRate = archiveDownloadsPerSec
	}
	dlGate := time.NewTicker(time.Second / time.Duration(dlRate))
	defer dlGate.Stop()

	var tGot, tSkip, tFail int
	consecFail := 0
	for i, id := range ids {
		<-rate.C
		g, s, f := archiveOne(app, opts.Root, id, hc, dlGate.C, opts)
		tGot += g
		tSkip += s
		tFail += f
		switch {
		case g > 0:
			consecFail = 0
		case f > 0:
			consecFail++
			if consecFail >= throttleAbortStreak {
				log.Printf("Archive: 連續 %d 張全部下載失敗（疑似被限流），中止本輪以待重試", consecFail)
				return fmt.Errorf("suspected throttling after %d consecutive failed photos", consecFail)
			}
		}
		if (i+1)%50 == 0 {
			log.Printf("Archive: 進度 %d/%d (got=%d skip=%d fail=%d)", i+1, len(ids), tGot, tSkip, tFail)
		}
	}
	log.Printf("Archive: 完成 照片=%d 下載=%d 跳過=%d 失敗=%d", len(ids), tGot, tSkip, tFail)
	return nil
}

// collectPhotoIDs prefers the local DB (zero API cost); falls back to Flickr search.
func collectPhotoIDs(app *App) ([]string, error) {
	ctx := context.Background()
	if app.DB != nil {
		if photos, err := app.DB.GetAllPhotos(ctx); err == nil && len(photos) > 0 {
			ids := make([]string, len(photos))
			for i, p := range photos {
				ids[i] = p.ID
			}
			return ids, nil
		}
	}
	var ids []string
	args := map[string]string{"sort": "date-posted-desc", "user_id": app.UserID}
	for _, page := range app.Flickr.PhotosSearch(args) {
		for _, p := range page.Photos.Photo {
			if p.Ispublic != 0 {
				ids = append(ids, p.ID)
			}
		}
	}
	return ids, nil
}

type archiveJob struct {
	src, dst, file, suffix, label, w, h string
}

// archiveOne fetches all sizes for one photo and downloads them concurrently.
// dlGate is a shared global rate limiter consumed once per actual network download.
func archiveOne(app *App, root, id string, hc *http.Client, dlGate <-chan time.Time, opts archiveOpts) (got, skip, fail int) {
	// Flickr returns "stat" at the top level, which PhotoSizes does not capture,
	// so gate on whether any sizes actually came back rather than on Sizes.Stat.
	dir := filepath.Join(root, "photos", id)
	// Fast path: when only fetching originals, skip the API call entirely if the
	// original is already on disk — keeps throttle-retry passes from re-scanning
	// thousands of already-done photos via the API.
	if opts.OnlyOriginal {
		if existing, _ := filepath.Glob(filepath.Join(dir, "o.*")); len(existing) > 0 {
			return 0, 1, 0
		}
	}
	sizes := app.Flickr.PhotosGetSizes(id)
	if len(sizes.Sizes.Size) == 0 {
		log.Printf("Archive: %s 跳過 (getSizes 無尺寸資料)", id)
		return 0, 0, 1
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("Archive: %s mkdir 失敗: %v", id, err)
		return 0, 0, 1
	}

	var jobs []archiveJob
	hasOriginal := false
	for _, s := range sizes.Sizes.Size {
		isOrig := s.Label == "Original"
		if isOrig {
			hasOriginal = true
		}
		if opts.SkipOriginal && isOrig {
			continue
		}
		if opts.OnlyOriginal && !isOrig {
			continue
		}
		if s.Source == "" {
			continue
		}
		suffix := suffixFromSource(s.Source)
		if opts.SkipXLarge && isXLargeSuffix(suffix) {
			continue
		}
		file := suffix + extFromURL(s.Source)
		jobs = append(jobs, archiveJob{
			src: s.Source, dst: filepath.Join(dir, file), file: file,
			suffix: suffix, label: s.Label, w: string(s.Width), h: string(s.Height),
		})
	}

	// Original fallback: build URL from PhotosGetInfo when getSizes lacks an "Original".
	if !hasOriginal && !opts.SkipOriginal {
		info := app.Flickr.PhotosGetInfo(id)
		if info.Common.Stat == "ok" && info.Photo.Orgsecret != "" {
			ext := info.Photo.Orgformat
			if ext == "" {
				ext = "jpg"
			}
			src := fmt.Sprintf("https://farm%d.staticflickr.com/%s/%s_%s_o.%s",
				info.Photo.Farm, info.Photo.Server, id, info.Photo.Orgsecret, ext)
			file := "o." + ext
			jobs = append(jobs, archiveJob{src: src, dst: filepath.Join(dir, file), file: file, suffix: "o", label: "Original"})
		}
	}

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		sem    = make(chan struct{}, archiveWorkers)
		msizes []manifestSize
	)
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j archiveJob) {
			defer wg.Done()
			defer func() { <-sem }()
			bytes, skipped, err := downloadFile(hc, j.src, j.dst, dlGate)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				fail++
				log.Printf("Archive: %s %s 下載失敗: %v", id, j.file, err)
				return
			}
			if skipped {
				skip++
			} else {
				got++
			}
			msizes = append(msizes, manifestSize{
				Label: j.label, Suffix: j.suffix, Width: j.w, Height: j.h,
				Source: j.src, File: j.file, Bytes: bytes,
			})
		}(j)
	}
	wg.Wait()

	m := manifest{
		PhotoID:     id,
		FetchedAt:   time.Now().UTC().Format(time.RFC3339),
		Candownload: sizes.Sizes.Candownload,
		Sizes:       msizes,
	}
	if data, err := json.MarshalIndent(m, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "sizes.json"), data, 0o644)
	}
	return got, skip, fail
}

// downloadFile GETs src to dst atomically (tmp + rename). Skips if dst exists &
// size>0. Retries on HTTP 429 and transient network errors with backoff
// (honouring Retry-After when present), since Flickr's CDN rate-limits bursts.
func downloadFile(hc *http.Client, src, dst string, gate <-chan time.Time) (int64, bool, error) {
	if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
		return fi.Size(), true, nil // skip: no network, no rate-limit token consumed
	}
	var lastErr error
	for attempt := 0; attempt < archiveMaxAttempts; attempt++ {
		if gate != nil {
			<-gate // global rate limit each network attempt (incl. retries)
		}
		n, retryAfter, err := tryDownloadOnce(hc, src, dst)
		if err == nil {
			return n, false, nil
		}
		lastErr = err
		if attempt < archiveMaxAttempts-1 {
			wait := retryAfter
			if wait <= 0 {
				wait = time.Duration(2<<attempt) * time.Second // 2s, 4s, 8s
			}
			time.Sleep(wait)
		}
	}
	return 0, false, lastErr
}

// tryDownloadOnce performs a single GET+write. On HTTP 429 it returns the
// suggested wait from Retry-After (0 if absent) so the caller can back off.
func tryDownloadOnce(hc *http.Client, src, dst string) (int64, time.Duration, error) {
	resp, err := hc.Get(src)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		ra := time.Duration(0)
		if s := strings.TrimSpace(resp.Header.Get("Retry-After")); s != "" {
			if secs, err := strconv.Atoi(s); err == nil && secs > 0 {
				ra = time.Duration(secs) * time.Second
			}
		}
		io.Copy(io.Discard, resp.Body)
		return 0, ra, fmt.Errorf("HTTP 429")
	}
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return 0, 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, 0, err
	}
	n, err := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if err != nil {
		os.Remove(tmp)
		return 0, 0, err
	}
	if closeErr != nil {
		os.Remove(tmp)
		return 0, 0, closeErr
	}
	if n == 0 {
		os.Remove(tmp)
		return 0, 0, fmt.Errorf("empty body")
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return 0, 0, err
	}
	return n, 0, nil
}
