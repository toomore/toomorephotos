package main

import (
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/toomore/lazyflickrgo/flickr"
	"github.com/toomore/lazyflickrgo/jsonstruct"
)

var (
	f             *flickr.Flickr
	photoPageExpr = regexp.MustCompile(`/p/([0-9]+)-?(.+)?`)
	rTags         [14]string
	tplIndex      *template.Template
	tplPhoto      *template.Template
	userID        string
)

func init() {
	funcs := template.FuncMap{
		"isHTML": func(content string) (template.HTML, error) {
			return template.HTML(strings.Replace(content, "\n", "<br>", -1)), nil
		},
		"isAltDesc": func(content string) (string, error) {
			return strings.Replace(content, "\n", " ", -1), nil
		},
	}
	tplIndex, _ = template.Must(template.ParseFiles("./base.htm")).ParseFiles("./index.htm")
	tplPhoto = template.Must(template.Must(template.ParseFiles("./base.htm")).Funcs(funcs).ParseFiles("./photo.htm"))

	f = flickr.NewFlickr(os.Getenv("FLICKRAPIKEY"), os.Getenv("FLICKRSECRET"))
	f.AuthToken = os.Getenv("FLICKRUSERTOKEN")
	userID = os.Getenv("FLICKRUSER")
	rTags = [14]string{
		"agfa,japan",
		"blackandwhite",
		"canon",
		"fuji,japan",
		"kodak,japan",
		"kyoto",
		"lomo",
		"tokyo",
		"taiwan",
		"japan",
		"moviefilms",
		"EtoC",
		"model",
		"遺留給妳的文字與影像",
	}
}

func logs(r *http.Request) {
	log.Println(r.Header.Get("X-Real-Ip"), r.Method, r.RequestURI, r.UserAgent())
}

func fromSearch(tags string) []jsonstruct.Photo {
	args := make(map[string]string)
	args["tags"] = tags
	args["tag_mode"] = "all"
	args["sort"] = "date-posted-desc"
	args["user_id"] = userID

	searchResult := f.PhotosSearch(args)

	var result []jsonstruct.Photo
	for _, val := range searchResult {
		result = append(result, val.Photos.Photo...)
	}

	return result
}

func serveSingle(pattern string, filename string) {
	http.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public")
		http.ServeFile(w, r, filename)
	})
}

func index(w http.ResponseWriter, r *http.Request) {
	logs(r)
	modValue := int(math.Mod(float64(time.Now().Minute()), float64(len(rTags))))
	etagStr := fmt.Sprintf("W/\"%d-%s\"", modValue, rTags[modValue])

	w.Header().Set("X-Tags", rTags[modValue])
	w.Header().Set("X-Github", "github.com/toomore/toomorephotos")

	if r.Header.Get("If-None-Match") == etagStr {
		w.WriteHeader(http.StatusNotModified)
	} else {
		w.Header().Set("ETag", etagStr)
		w.Header().Set("Cache-Control", "max-age=120")
		tplIndex.Execute(w, fromSearch(rTags[modValue]))
	}
}

func photo(w http.ResponseWriter, r *http.Request) {
	logs(r)
	match := photoPageExpr.FindStringSubmatch(r.RequestURI)
	var photono string
	if len(match) >= 2 {
		photono = match[1]
	}
	etagStr := fmt.Sprintf("W/\"%s\"", photono)

	if r.Header.Get("If-None-Match") == etagStr {
		w.WriteHeader(http.StatusNotModified)
	} else {
		var photoinfo jsonstruct.PhotosGetInfo
		if photono != "" {
			photoinfo = f.PhotosGetInfo(photono)
			if photoinfo.Common.Stat == "ok" {
				w.Header().Set("ETag", etagStr)
				w.Header().Set("Cache-Control", "max-age=300")
				tplPhoto.Execute(w, photoinfo.Photo)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}
}

func sitemap(w http.ResponseWriter, r *http.Request) {
	args := make(map[string]string)
	args["sort"] = "date-posted-desc"
	args["user_id"] = userID

	searchResult := f.PhotosSearch(args)

	var result []jsonstruct.Photo
	for _, val := range searchResult {
		result = append(result, val.Photos.Photo...)
	}
	template.Must(template.ParseFiles("./sitemap.htm")).Execute(w, result)
}

func main() {
	http.HandleFunc("/", index)
	http.HandleFunc("/p/", photo)
	http.HandleFunc("/sitemap/", sitemap)
	//http.Handle("/static", http.FileServer(http.Dir("./static/")))
	serveSingle("/favicon.ico", "favicon.ico")
	serveSingle("/jquery.unveil.min.js", "jquery.unveil.min.js")
	serveSingle("/base_min.css", "base_min.css")
	log.Println(http.ListenAndServe(":8080", nil))
}
