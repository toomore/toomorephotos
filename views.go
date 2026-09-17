package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// viewQueueSize is generous enough that a normal burst never drops events,
	// small enough to stay negligible against the container's 512m limit.
	viewQueueSize   = 2048
	viewFlushPeriod = 30 * time.Second
	viewFlushSize   = 500
	viewDedupeTTL   = 24 * time.Hour
)

var (
	photoIDExpr = regexp.MustCompile(`^[0-9]{1,20}$`)
	// botUAExpr catches the crawlers that do run JavaScript; the ones that do
	// not never reach this endpoint in the first place.
	botUAExpr = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|scrape|facebookexternalhit|headlesschrome|phantomjs|python|okhttp|curl/|wget|java/|go-http-client|libwww|feedfetcher|preview`)
)

// viewSalt keeps the stored address hashes from being reversible with a table
// of known addresses. Regenerated per process: dedupe only spans one day.
var viewSalt = newViewSalt()

func newViewSalt() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// viewRecorder is the part of the collector the handler needs.
type viewRecorder interface {
	Record(photoID string)
}

// viewSink is the storage side of the collector.
type viewSink interface {
	AddViews(ctx context.Context, counts map[string]int64) error
}

// viewCollector batches page views: a request costs one channel send instead
// of a database round trip, so a scraper burst cannot open a connection per
// request the way the 2026-07-30 incident did.
type viewCollector struct {
	ch      chan string
	sink    viewSink
	dropped atomic.Int64
}

func newViewCollector(sink viewSink) *viewCollector {
	return &viewCollector{ch: make(chan string, viewQueueSize), sink: sink}
}

// Record never blocks: a full queue drops the event rather than delaying the
// response. Losing a view is preferable to holding a request open.
func (c *viewCollector) Record(photoID string) {
	select {
	case c.ch <- photoID:
	default:
		c.dropped.Add(1)
	}
}

// Run flushes on a timer until ctx is cancelled, then drains what is queued
// and writes it before returning, so a deploy does not lose the last batch.
func (c *viewCollector) Run(ctx context.Context) {
	ticker := time.NewTicker(viewFlushPeriod)
	defer ticker.Stop()

	counts := make(map[string]int64)
	flush := func() {
		if len(counts) == 0 {
			return
		}
		// Not ctx: during shutdown it is already cancelled, and this write is
		// exactly what must still go through.
		flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := c.sink.AddViews(flushCtx, counts)
		cancel()
		if err != nil {
			log.Printf("views: flush of %d photos failed: %v", len(counts), err)
		}
		counts = make(map[string]int64)
	}

	for {
		select {
		case id := <-c.ch:
			counts[id]++
			if len(counts) >= viewFlushSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-ctx.Done():
		drain:
			for {
				select {
				case id := <-c.ch:
					counts[id]++
				default:
					break drain
				}
			}
			flush()
			if n := c.dropped.Load(); n > 0 {
				log.Printf("views: dropped %d events (queue full)", n)
			}
			return
		}
	}
}

// view records a page view reported by the beacon on the photo page.
// Counting inside the photo handler would undercount badly: Cloudflare serves
// most /p/ HTML from its own cache, so those requests never reach this origin.
func (a *App) view(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	photoID := r.URL.Query().Get("p")
	if !photoIDExpr.MatchString(photoID) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Everything below answers 204 either way, so a client cannot probe which
	// requests are filtered out.
	w.WriteHeader(http.StatusNoContent)

	if a.Views == nil || isBot(r.UserAgent()) || isPrefetch(r) {
		return
	}
	if a.countedToday(r, photoID) {
		return
	}
	a.Views.Record(photoID)
}

// countedToday reports whether this visitor already counted for this photo
// today, so reloading a page does not inflate the number. Only a salted hash
// of the address is stored, and it expires after a day.
func (a *App) countedToday(r *http.Request, photoID string) bool {
	if a.Cache == nil {
		return false
	}
	ip := clientIP(r)
	if ip == "" {
		return false
	}
	key := fmt.Sprintf("view:%s:%s:%s", time.Now().UTC().Format("20060102"), photoID, hashIP(ip))
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	var seen bool
	if ok, _ := a.Cache.Get(ctx, key, &seen); ok {
		return true
	}
	_ = a.Cache.Set(ctx, key, true, viewDedupeTTL)
	return false
}

func hashIP(ip string) string {
	sum := sha256.Sum256([]byte(viewSalt + ip))
	return hex.EncodeToString(sum[:8])
}

func isBot(ua string) bool {
	return ua == "" || botUAExpr.MatchString(ua)
}

// isPrefetch reports whether the browser is speculatively loading the page.
// The homepage prefetches 30 photo pages, and none of them is a real read.
func isPrefetch(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Sec-Purpose"), "prefetch") ||
		strings.Contains(r.Header.Get("Purpose"), "prefetch") ||
		strings.Contains(r.Header.Get("X-Moz"), "prefetch")
}

// runViewsReport prints the view ranking collected by the beacon.
func runViewsReport(app *App, days, top int) error {
	if app.DB == nil {
		return fmt.Errorf("views 需要 DATABASE_URL，請設定環境變數後再執行")
	}
	ctx := context.Background()
	total, err := app.DB.TotalViews(ctx, days)
	if err != nil {
		return err
	}
	rows, err := app.DB.TopViews(ctx, days, top)
	if err != nil {
		return err
	}
	fmt.Printf("最近 %d 天：共 %d 次瀏覽，以下為前 %d 名\n\n", days, total, top)
	for i, r := range rows {
		title := r.Title
		if title == "" {
			title = "(無標題)"
		}
		fmt.Printf("%3d. %8d  %-18s %s\n", i+1, r.Views, r.PhotoID, title)
	}
	if len(rows) == 0 {
		fmt.Println("(還沒有資料)")
	}
	return nil
}
