package main

import (
	"crypto/md5"
	"fmt"
	"hash"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/toomore/lazyflickrgo/jsonstruct"
)

func logs(r *http.Request, note string) {
	log.Println(clientIP(r), r.Method, r.RequestURI, r.UserAgent(), note)
}

// clientIP returns the visitor's address. In production nginx sets X-Real-Ip
// to the Cloudflare edge, so prefer the CF-Connecting-IP header Cloudflare adds.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	return r.Header.Get("X-Real-Ip")
}

func (a *App) serveSingle(pattern string, filename string) {
	if file, err := os.ReadFile(filename); err == nil {
		h := md5.New()
		h.Write(file)
		a.HashCache[filename] = fmt.Sprintf("W/\"%x\"", h.Sum(nil))
	}

	hashCache := a.HashCache
	http.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		logs(r, "[static]")
		if r.Header.Get("If-None-Match") == hashCache[filename] {
			logs(r, "[304]")
			w.WriteHeader(http.StatusNotModified)
		} else {
			w.Header().Set("ETag", hashCache[filename])
			http.ServeFile(w, r, filename)
		}
	})
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	logs(r, "")
	modValue := tagIndex(r.URL.Query().Get("t"), len(a.Tags), time.Now())
	etagStr := fmt.Sprintf("W/\"%d-%s-%d\"", modValue, a.Tags[modValue], time.Now().YearDay())

	w.Header().Set("X-Tags", a.Tags[modValue])
	w.Header().Set("X-Github", "github.com/toomore/toomorephotos")

	if r.Header.Get("If-None-Match") == etagStr {
		logs(r, "[304]")
		w.WriteHeader(http.StatusNotModified)
	} else {
		w.Header().Set("ETag", etagStr)
		w.Header().Set("Cache-Control", "max-age=120")
		result := a.getCachedFromSearch(a.Tags[modValue])
		wall := dailyPick(result, a.Tags[modValue], time.Now().YearDay(), indexPhotoCount)
		prefetch := wall
		if len(prefetch) > indexPrefetchCount {
			prefetch = prefetch[:indexPrefetchCount]
		}
		allPhotos := a.getCachedAllPhotos()
		var featured *jsonstruct.Photo
		if len(allPhotos) > 0 {
			f := allPhotos[time.Now().YearDay()%len(allPhotos)]
			featured = &f
		}
		var featuredWidth, featuredHeight int64
		if featured != nil {
			if w, h, ok := a.getCachedPhotosGetSizes(featured.ID); ok {
				featuredWidth, featuredHeight = w, h
			}
		}
		data := struct {
			R              []jsonstruct.Photo
			L              []jsonstruct.Photo
			Featured       *jsonstruct.Photo
			FeaturedWidth  int64
			FeaturedHeight int64
		}{wall, prefetch, featured, featuredWidth, featuredHeight}
		if err := a.TplIndex.Execute(w, data); err != nil {
			log.Printf("template execute error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}
}

const (
	// indexPhotoCount caps the wall. A tag can hold thousands of photos, and
	// rendering all of them made the homepage 539 KB for "japan".
	indexPhotoCount = 100
	// indexPrefetchCount is how many of those photo pages get rel="prefetch".
	indexPrefetchCount = 30
)

// dailyPick selects photos that stay the same for a given tag and day, so the
// page still matches its ETag and whatever Cloudflare cached, while a different
// slice of the archive surfaces each day.
//
// It walks the list with a fixed stride rather than shuffling a copy: the
// homepage is the hottest page here, and copying a few thousand structs per
// request is the kind of allocation the 2026-07 memory incident was made of.
func dailyPick(photos []jsonstruct.Photo, tag string, day, n int) []jsonstruct.Photo {
	total := len(photos)
	if total <= n {
		return photos
	}

	h := uint32(2166136261) // FNV-1a over the tag and the day
	for _, c := range tag {
		h = (h ^ uint32(c)) * 16777619
	}
	h = (h ^ uint32(day)) * 16777619

	start := int(h % uint32(total))
	// A stride coprime with the length never repeats a photo; 1 always is, so
	// the search terminates.
	stride := int(h>>8)%(total-1) + 1
	for gcd(stride, total) != 1 {
		stride++
		if stride >= total {
			stride = 1
		}
	}

	picked := make([]jsonstruct.Photo, 0, n)
	for i := 0; i < n; i++ {
		picked = append(picked, photos[(start+i*stride)%total])
	}
	return picked
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// tagIndex picks the homepage tag: ?t=N when given, otherwise rotate by minute.
// The result is always in [0, n), including for negative N.
func tagIndex(t string, n int, now time.Time) int {
	v, err := strconv.Atoi(t)
	if err != nil {
		v = now.Minute()
	}
	v %= n
	if v < 0 {
		v += n
	}
	return v
}

func (a *App) photo(w http.ResponseWriter, r *http.Request) {
	logs(r, "")
	match := a.PhotoPageExpr.FindStringSubmatch(r.RequestURI)
	var photono string
	if len(match) >= 2 {
		photono = match[1]
	}

	if photono == "" {
		a.notFound(w, r)
		return
	}
	photoinfo := a.getCachedPhotosGetInfo(photono)

	var etaghex hash.Hash
	var etagStr string
	if photoinfo.Common.Stat == "ok" {
		etaghex = md5.New()
		io.WriteString(etaghex, photoinfo.Photo.Title.Content)
		io.WriteString(etaghex, photoinfo.Photo.Description.Content)
		etagStr = fmt.Sprintf("W/\"%x\"", etaghex.Sum(nil))
	} else {
		a.notFound(w, r)
		return
	}

	if photoinfo.Photo.Owner.Nsid != a.UserID {
		a.notFound(w, r)
		return
	}

	// Without it nginx's proxy_cache never stores /p/. Set before the 304 branch
	// so revalidated responses carry it too.
	w.Header().Set("Cache-Control", "max-age=600")
	if r.Header.Get("If-None-Match") == etagStr {
		logs(r, "[304]")
		w.WriteHeader(http.StatusNotModified)
	} else {
		w.Header().Set("ETag", etagStr)
		width, height := int64(0), int64(0)
		if w, h, ok := a.getCachedPhotosGetSizes(photono); ok {
			width, height = w, h
		}
		paddingBottomPercent := 75.0 // 4:3 fallback
		if width > 0 && height > 0 {
			paddingBottomPercent = float64(height) / float64(width) * 100
		}
		var tagRaws []string
		for _, t := range photoinfo.Photo.Tags.Tag {
			tagRaws = append(tagRaws, t.Raw)
		}
		relatedPhotos := a.getCachedRelatedPhotos(photono, tagRaws)
		data := struct {
			Photo                interface{}
			Width                int64
			Height               int64
			PaddingBottomPercent float64
			RelatedPhotos        []jsonstruct.Photo
			MapboxToken          string
		}{photoinfo.Photo, width, height, paddingBottomPercent, relatedPhotos, a.MapboxToken}
		if err := a.TplPhoto.Execute(w, data); err != nil {
			log.Printf("template execute error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}
}

func (a *App) sitemap(w http.ResponseWriter, r *http.Request) {
	result := a.getCachedAllPhotos()
	tags := make([]int, len(a.Tags))
	for i := range a.Tags {
		tags[i] = i
	}
	data := struct {
		R []jsonstruct.Photo
		T []int
	}{result, tags}
	if err := a.TplSitemap.Execute(w, data); err != nil {
		log.Printf("template execute error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	logs(r, "[!] Page Not Found")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("Maybe not in this timeline ... (35.701099, 139.738557)"))
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
