package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	httpPort   = flag.String("p", ":8080", "HTTP port")
	doSync     = flag.Bool("sync", false, "執行 sync：從 Flickr 取得照片 metadata 寫入 DB 後退出")
	doAudit    = flag.Bool("audit", false, "執行 metadata 豐富度稽核（讀 DB）後退出")
	doBackfill = flag.Bool("backfill-dates", false, "從描述解析拍攝日期回填 photos.taken_real 後退出")

	doArchive     = flag.Bool("archive-images", false, "下載所有照片尺寸到本地存檔後退出")
	archiveDir    = flag.String("archive-dir", "", "存檔根目錄（預設讀 IMAGE_ARCHIVE_DIR，再預設 ./archive）")
	archiveLimit  = flag.Int("limit", 0, "只處理前 N 張（測試用，0=全部）")
	archivePID    = flag.String("photo-id", "", "只處理單一 photo id（測試用）")
	skipOriginal  = flag.Bool("skip-original", false, "略過原圖，只抓縮圖")
	onlyOriginal  = flag.Bool("only-original", false, "只抓原圖")
	skipXLarge    = flag.Bool("skip-xlarge", false, "略過超大尺寸 3k/4k/5k/6k（會被 Flickr 限流，可從原圖重縮）")
	archiveDLRate = flag.Int("dl-rate", 0, "每秒下載數上限（0=預設5；原圖被限流時可調低如 2）")

	doViews   = flag.Bool("views", false, "列出照片瀏覽排行（讀 DB）後退出")
	viewsDays = flag.Int("views-days", 7, "配合 -views：統計最近幾天")
	viewsTop  = flag.Int("views-top", 20, "配合 -views：列出前幾名")
)

func main() {
	flag.Parse()
	// Before anything logs: the first Flickr call happens inside NewApp.
	log.SetOutput(redactWriter{os.Stderr})

	app, err := NewApp()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if app.DB != nil {
			app.DB.Close()
		}
	}()

	if *doSync {
		if err := runSync(app); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *doAudit {
		if err := runAudit(app); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *doBackfill {
		if app.DB == nil {
			log.Fatal("backfill-dates 需要 DATABASE_URL，請設定環境變數後再執行")
		}
		withDate, total, err := app.DB.BackfillTakenReal(context.Background())
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Backfill taken_real 完成：%d/%d 張解析出拍攝日期", withDate, total)
		return
	}

	if *doArchive {
		if err := runArchive(app, archiveOpts{
			Root:            resolveArchiveRoot(*archiveDir),
			Limit:           *archiveLimit,
			OnlyID:          *archivePID,
			SkipOriginal:    *skipOriginal,
			OnlyOriginal:    *onlyOriginal,
			SkipXLarge:      *skipXLarge,
			DownloadsPerSec: *archiveDLRate,
		}); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *doViews {
		if err := runViewsReport(app, *viewsDays, *viewsTop); err != nil {
			log.Fatal(err)
		}
		return
	}

	// Page views are collected here rather than in the photo handler because
	// Cloudflare answers most /p/ requests from its cache.
	var views *viewCollector
	if app.DB != nil {
		views = newViewCollector(app.DB)
		app.Views = views
	} else {
		log.Println("Views: DATABASE_URL 未設定，不記錄瀏覽數")
	}

	http.HandleFunc("/", app.index)
	http.HandleFunc("/p/", app.photo)
	http.HandleFunc("/sitemap/", app.sitemap)
	http.HandleFunc("/rss", app.rss)
	http.HandleFunc("/atom", app.atom)
	http.HandleFunc("/fr", app.notFound)
	http.HandleFunc("/health", app.health)
	http.HandleFunc("/v", app.view)

	app.serveSingle("/favicon.ico", "favicon.ico")
	app.serveSingle("/jquery.unveil.min.js", "jquery.unveil.min.js")
	app.serveSingle("/base_min.css", "base_min.css")
	app.serveSingle("/base_photo_min.css", "base_photo_min.css")
	app.serveSingle("/robots.txt", "robots.txt")

	// Timeouts are deliberate: the bare ListenAndServe this replaced let slow
	// or abandoned connections pile up handlers indefinitely during scraper
	// bursts.
	srv := &http.Server{
		Addr:              *httpPort,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	viewsDone := make(chan struct{})
	if views != nil {
		go func() {
			defer close(viewsDone)
			views.Run(ctx)
		}()
	} else {
		close(viewsDone)
	}

	shutdownDone := make(chan struct{})
	go func() {
		<-ctx.Done()
		log.Println("Shutting down HTTP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Println("HTTP shutdown:", err)
		}
		close(shutdownDone)
	}()

	log.Println("HTTP Port:", *httpPort)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Println(err)
		return
	}
	// ListenAndServe returns as soon as Shutdown starts; wait for in-flight
	// requests to finish, then for the last batch of views to be written,
	// before the deferred DB.Close runs.
	<-shutdownDone
	<-viewsDone
}
