package main

import (
	"bufio"
	"os"
	"strconv"
	"time"

	"github.com/toomore/lazyflickrgo/jsonstruct"
)

func getTags(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var result []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		result = append(result, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *App) fromSearch(tags string) []jsonstruct.Photo {
	args := map[string]string{
		"tags":      tags,
		"tag_mode":  "all",
		"sort":      "date-posted-desc",
		"user_id":   a.UserID,
	}

	var result []jsonstruct.Photo
	for _, val := range a.Flickr.PhotosSearch(args) {
		result = append(result, val.Photos.Photo...)
	}
	return result
}

func (a *App) getCachedFromSearch(tag string) []jsonstruct.Photo {
	a.indexCacheMu.RLock()
	if ent, ok := a.indexCache[tag]; ok && time.Now().Before(ent.expiresAt) {
		result := ent.photos
		a.indexCacheMu.RUnlock()
		return result
	}
	a.indexCacheMu.RUnlock()

	a.indexCacheMu.Lock()
	defer a.indexCacheMu.Unlock()
	if ent, ok := a.indexCache[tag]; ok && time.Now().Before(ent.expiresAt) {
		return ent.photos
	}
	result := a.fromSearch(tag)
	a.indexCache[tag] = indexCacheEntry{photos: result, expiresAt: time.Now().Add(a.indexCacheTTL)}
	return result
}

func (a *App) getCachedPhotosGetInfo(photoID string) jsonstruct.PhotosGetInfo {
	a.photoCacheMu.RLock()
	if ent, ok := a.photoCache[photoID]; ok && time.Now().Before(ent.expiresAt) {
		info := ent.info
		a.photoCacheMu.RUnlock()
		return info
	}
	a.photoCacheMu.RUnlock()

	a.photoCacheMu.Lock()
	defer a.photoCacheMu.Unlock()
	if ent, ok := a.photoCache[photoID]; ok && time.Now().Before(ent.expiresAt) {
		return ent.info
	}
	info := a.Flickr.PhotosGetInfo(photoID)
	a.photoCache[photoID] = photoCacheEntry{info: info, expiresAt: time.Now().Add(a.photoCacheTTL)}
	return info
}

// getCachedPhotosGetSizes returns width and height for the Large (1024) size.
// ok is false when the API fails or no Large/Large 1024 size is found.
func (a *App) getCachedPhotosGetSizes(photoID string) (width, height int64, ok bool) {
	a.photoSizesCacheMu.RLock()
	if ent, hit := a.photoSizesCache[photoID]; hit && time.Now().Before(ent.expiresAt) {
		a.photoSizesCacheMu.RUnlock()
		return ent.width, ent.height, true
	}
	a.photoSizesCacheMu.RUnlock()

	a.photoSizesCacheMu.Lock()
	defer a.photoSizesCacheMu.Unlock()
	if ent, hit := a.photoSizesCache[photoID]; hit && time.Now().Before(ent.expiresAt) {
		return ent.width, ent.height, true
	}
	sizes := a.Flickr.PhotosGetSizes(photoID)
	preferredLabels := []string{"Large", "Large 1024", "Large 1600", "Medium 800", "Medium 640"}
	for _, label := range preferredLabels {
		for _, s := range sizes.Sizes.Size {
			if s.Label == label {
				w, errW := strconv.ParseInt(string(s.Width), 10, 64)
				h, errH := strconv.ParseInt(string(s.Height), 10, 64)
				if errW == nil && errH == nil && w > 0 && h > 0 {
					a.photoSizesCache[photoID] = photoSizesCacheEntry{
						width:     w,
						height:    h,
						expiresAt: time.Now().Add(a.photoSizesCacheTTL),
					}
					return w, h, true
				}
				break
			}
		}
	}
	// Fallback: use first available size with valid dimensions
	for _, s := range sizes.Sizes.Size {
		w, errW := strconv.ParseInt(string(s.Width), 10, 64)
		h, errH := strconv.ParseInt(string(s.Height), 10, 64)
		if errW == nil && errH == nil && w > 0 && h > 0 {
			a.photoSizesCache[photoID] = photoSizesCacheEntry{
				width:     w,
				height:    h,
				expiresAt: time.Now().Add(a.photoSizesCacheTTL),
			}
			return w, h, true
		}
	}
	return 0, 0, false
}

func (a *App) allPhotos(result *[]jsonstruct.Photo) {
	args := map[string]string{
		"sort":     "date-posted-desc",
		"user_id":  a.UserID,
	}

	for _, val := range a.Flickr.PhotosSearch(args) {
		*result = append(*result, val.Photos.Photo...)
	}
}

func (a *App) getCachedAllPhotos() []jsonstruct.Photo {
	a.sitemapCacheMu.RLock()
	if a.sitemapCache != nil && time.Since(a.sitemapCacheAt) < a.sitemapCacheTTL {
		result := a.sitemapCache
		a.sitemapCacheMu.RUnlock()
		return result
	}
	a.sitemapCacheMu.RUnlock()

	a.sitemapCacheMu.Lock()
	defer a.sitemapCacheMu.Unlock()
	if a.sitemapCache != nil && time.Since(a.sitemapCacheAt) < a.sitemapCacheTTL {
		return a.sitemapCache
	}
	var result []jsonstruct.Photo
	a.allPhotos(&result)
	a.sitemapCache = result
	a.sitemapCacheAt = time.Now()
	return a.sitemapCache
}
