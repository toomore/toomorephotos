package main

import (
	"context"
	"flag"
	"log"
	"net/http"
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
)

func main() {
	flag.Parse()
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

	http.HandleFunc("/", app.index)
	http.HandleFunc("/p/", app.photo)
	http.HandleFunc("/sitemap/", app.sitemap)
	http.HandleFunc("/rss", app.rss)
	http.HandleFunc("/atom", app.atom)
	http.HandleFunc("/fr", app.notFound)
	http.HandleFunc("/health", app.health)

	app.serveSingle("/favicon.ico", "favicon.ico")
	app.serveSingle("/jquery.unveil.min.js", "jquery.unveil.min.js")
	app.serveSingle("/base_min.css", "base_min.css")
	app.serveSingle("/base_photo_min.css", "base_photo_min.css")
	app.serveSingle("/robots.txt", "robots.txt")

	log.Println("HTTP Port:", *httpPort)
	log.Println(http.ListenAndServe(*httpPort, nil))
}
