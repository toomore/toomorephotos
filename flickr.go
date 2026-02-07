package main

import (
	"bufio"
	"context"
	"math/rand"
	"os"
	"strconv"
	"strings"
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
	ctx := context.Background()
	key := "index:" + tag
	var result []jsonstruct.Photo
	if ok, _ := a.Cache.Get(ctx, key, &result); ok {
		return result
	}
	result = a.fromSearch(tag)
	_ = a.Cache.Set(ctx, key, result, a.IndexCacheTTL)
	return result
}

func (a *App) getCachedPhotosGetInfo(photoID string) jsonstruct.PhotosGetInfo {
	ctx := context.Background()
	key := "photo:" + photoID
	var info jsonstruct.PhotosGetInfo
	if ok, _ := a.Cache.Get(ctx, key, &info); ok {
		return info
	}
	info = a.Flickr.PhotosGetInfo(photoID)
	_ = a.Cache.Set(ctx, key, info, a.PhotoCacheTTL)
	return info
}

type photoSizesVal struct {
	Width  int64 `json:"width"`
	Height int64 `json:"height"`
}

// getCachedPhotosGetSizes returns width and height for the Large (1024) size.
// ok is false when the API fails or no Large/Large 1024 size is found.
func (a *App) getCachedPhotosGetSizes(photoID string) (width, height int64, ok bool) {
	ctx := context.Background()
	key := "photosizes:" + photoID
	var v photoSizesVal
	if found, _ := a.Cache.Get(ctx, key, &v); found && v.Width > 0 && v.Height > 0 {
		return v.Width, v.Height, true
	}
	sizes := a.Flickr.PhotosGetSizes(photoID)
	preferredLabels := []string{"Large", "Large 1024", "Large 1600", "Medium 800", "Medium 640"}
	for _, label := range preferredLabels {
		for _, s := range sizes.Sizes.Size {
			if s.Label == label {
				w, errW := strconv.ParseInt(string(s.Width), 10, 64)
				h, errH := strconv.ParseInt(string(s.Height), 10, 64)
				if errW == nil && errH == nil && w > 0 && h > 0 {
					_ = a.Cache.Set(ctx, key, photoSizesVal{Width: w, Height: h}, a.PhotoSizesCacheTTL)
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
			_ = a.Cache.Set(ctx, key, photoSizesVal{Width: w, Height: h}, a.PhotoSizesCacheTTL)
			return w, h, true
		}
	}
	return 0, 0, false
}

func (a *App) getRelatedPhotos(photoID string, tagRaws []string) []jsonstruct.Photo {
	if len(tagRaws) == 0 {
		return nil
	}
	const maxRelated = 12
	const sameTagLimit = 8
	const otherTagLimit = 4

	// 1. Same-tag results
	args := map[string]string{
		"tags":     strings.Join(tagRaws, ","),
		"tag_mode": "any",
		"sort":     "date-posted-desc",
		"user_id":  a.UserID,
	}
	var sameTag []jsonstruct.Photo
	for _, page := range a.Flickr.PhotosSearch(args) {
		for _, p := range page.Photos.Photo {
			if p.ID != photoID && p.Ispublic != 0 {
				sameTag = append(sameTag, p)
			}
		}
	}
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(sameTag), func(i, j int) { sameTag[i], sameTag[j] = sameTag[j], sameTag[i] })
	if len(sameTag) > sameTagLimit {
		sameTag = sameTag[:sameTagLimit]
	}

	// 2. Other tags (from a.Tags, excluding current photo's tags)
	tagSet := make(map[string]bool)
	for _, t := range tagRaws {
		tagSet[t] = true
	}
	var otherTags []string
	for _, t := range a.Tags {
		if !tagSet[t] {
			otherTags = append(otherTags, t)
		}
	}

	var otherTag []jsonstruct.Photo
	if len(otherTags) > 0 {
		var h uint32
		for _, c := range photoID {
			h = h*31 + uint32(c)
		}
		pickTag := otherTags[int(h)%len(otherTags)]
		otherArgs := map[string]string{
			"tags":     pickTag,
			"tag_mode": "all",
			"sort":     "date-posted-desc",
			"user_id":  a.UserID,
		}
		seen := make(map[string]bool)
		for _, p := range sameTag {
			seen[p.ID] = true
		}
	pageLoop:
		for _, page := range a.Flickr.PhotosSearch(otherArgs) {
			for _, p := range page.Photos.Photo {
				if p.ID != photoID && p.Ispublic != 0 && !seen[p.ID] {
					otherTag = append(otherTag, p)
					seen[p.ID] = true
					if len(otherTag) >= otherTagLimit {
						break pageLoop
					}
				}
			}
		}
	}

	// 3. Merge and shuffle
	merged := append(sameTag, otherTag...)
	rand.Shuffle(len(merged), func(i, j int) { merged[i], merged[j] = merged[j], merged[i] })
	if len(merged) > maxRelated {
		merged = merged[:maxRelated]
	}
	return merged
}

func (a *App) getCachedRelatedPhotos(photoID string, tagRaws []string) []jsonstruct.Photo {
	ctx := context.Background()
	key := "related:" + photoID
	var result []jsonstruct.Photo
	if ok, _ := a.Cache.Get(ctx, key, &result); ok {
		return result
	}
	result = a.getRelatedPhotos(photoID, tagRaws)
	_ = a.Cache.Set(ctx, key, result, a.RelatedPhotosCacheTTL)
	return result
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
	ctx := context.Background()
	key := "sitemap"
	var result []jsonstruct.Photo
	if ok, _ := a.Cache.Get(ctx, key, &result); ok {
		return result
	}
	a.allPhotos(&result)
	_ = a.Cache.Set(ctx, key, result, a.SitemapCacheTTL)
	return result
}
