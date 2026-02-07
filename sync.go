package main

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/toomore/lazyflickrgo/jsonstruct"
)

const syncRatePerSec = 2

// runSync fetches all photos from Flickr and upserts to DB.
func runSync(app *App) error {
	if app.DB == nil {
		log.Fatal("sync 需要 DATABASE_URL，請設定環境變數")
	}
	ctx := context.Background()

	// 1. Get all photo IDs
	var allIDs []string
	args := map[string]string{
		"sort":     "date-posted-desc",
		"user_id":  app.UserID,
	}
	for _, page := range app.Flickr.PhotosSearch(args) {
		for _, p := range page.Photos.Photo {
			if p.Ispublic != 0 {
				allIDs = append(allIDs, p.ID)
			}
		}
	}
	log.Printf("Sync: 取得 %d 張照片 ID", len(allIDs))

	rate := time.NewTicker(time.Second / syncRatePerSec)
	defer rate.Stop()

	okCount, failCount := 0, 0
	for i, id := range allIDs {
		<-rate.C
		info, width, height := fetchPhotoWithRetry(app, id)
		if info.Common.Stat != "ok" {
			log.Printf("Sync: 跳過 %s (API stat=%s)", id, info.Common.Stat)
			failCount++
			continue
		}
		if err := app.DB.UpsertPhoto(ctx, id, info, width, height); err != nil {
			log.Printf("Sync: 寫入 DB 失敗 %s: %v", id, err)
			failCount++
			continue
		}
		okCount++
		if (i+1)%50 == 0 {
			log.Printf("Sync: 進度 %d/%d", i+1, len(allIDs))
		}
	}
	log.Printf("Sync: 完成 %d 成功, %d 失敗", okCount, failCount)
	return nil
}

func fetchPhotoWithRetry(app *App, photoID string) (jsonstruct.PhotosGetInfo, int64, int64) {
	info := app.Flickr.PhotosGetInfo(photoID)
	if info.Common.Stat != "ok" {
		return info, 0, 0
	}
	sizes := app.Flickr.PhotosGetSizes(photoID)
	preferredLabels := []string{"Large", "Large 1024", "Large 1600", "Medium 800", "Medium 640"}
	for _, label := range preferredLabels {
		for _, s := range sizes.Sizes.Size {
			if s.Label == label {
				w, errW := strconv.ParseInt(string(s.Width), 10, 64)
				h, errH := strconv.ParseInt(string(s.Height), 10, 64)
				if errW == nil && errH == nil && w > 0 && h > 0 {
					return info, w, h
				}
				break
			}
		}
	}
	for _, s := range sizes.Sizes.Size {
		w, errW := strconv.ParseInt(string(s.Width), 10, 64)
		h, errH := strconv.ParseInt(string(s.Height), 10, 64)
		if errW == nil && errH == nil && w > 0 && h > 0 {
			return info, w, h
		}
	}
	return info, 0, 0
}
