package main

import (
	"crypto/md5"
	"fmt"
	"hash"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/toomore/lazyflickrgo/jsonstruct"
)

func logs(r *http.Request, note string) {
	log.Println(r.Header.Get("X-Real-Ip"), r.Method, r.RequestURI, r.UserAgent(), note)
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
	var modValue int
	var err error
	if modValue, err = strconv.Atoi(r.URL.Query().Get("t")); err == nil {
		modValue = int(math.Mod(float64(modValue), float64(len(a.Tags))))
	} else {
		modValue = int(math.Mod(float64(time.Now().Minute()), float64(len(a.Tags))))
	}
	etagStr := fmt.Sprintf("W/\"%d-%s\"", modValue, a.Tags[modValue])

	w.Header().Set("X-Tags", a.Tags[modValue])
	w.Header().Set("X-Github", "github.com/toomore/toomorephotos")

	if r.Header.Get("If-None-Match") == etagStr {
		logs(r, "[304]")
		w.WriteHeader(http.StatusNotModified)
	} else {
		w.Header().Set("ETag", etagStr)
		w.Header().Set("Cache-Control", "max-age=120")
		result := a.getCachedFromSearch(a.Tags[modValue])
		min := 30
		if len(result) < 30 {
			min = len(result)
		}
		data := struct {
			R []jsonstruct.Photo
			L []jsonstruct.Photo
		}{result, result[:min]}
		if err := a.TplIndex.Execute(w, data); err != nil {
			log.Printf("template execute error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}
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

	if photoinfo.Photo.Owner.Nsid != "92438116@N00" {
		a.notFound(w, r)
		return
	}

	if r.Header.Get("If-None-Match") == etagStr {
		logs(r, "[304]")
		w.WriteHeader(http.StatusNotModified)
	} else {
		w.Header().Set("ETag", etagStr)
		if err := a.TplPhoto.Execute(w, photoinfo.Photo); err != nil {
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
