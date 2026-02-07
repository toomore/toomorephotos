package main

import (
	"bufio"
	"os"
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
