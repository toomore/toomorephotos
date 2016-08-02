package main

import (
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/toomore/lazyflickrgo/flickr"
	"github.com/toomore/lazyflickrgo/jsonstruct"
)

var (
	f      *flickr.Flickr
	rTags  [10]string
	tpl    *template.Template
	userID string
)

func init() {
	tpl, _ = template.ParseFiles("./base.htm")
	f = flickr.NewFlickr(os.Getenv("FLICKRAPIKEY"), os.Getenv("FLICKRSECRET"))
	f.AuthToken = os.Getenv("FLICKRUSERTOKEN")
	userID = os.Getenv("FLICKRUSER")
	rTags = [10]string{
		"agfa,japan",
		"blackandwhite",
		"canon",
		"fuji,japan",
		"kodak,japan",
		"kyoto",
		"lomo",
		"tokyo",
		"taiwan",
		"EtoC",
	}
}

func logs(r *http.Request) {
	log.Println(r.Header["X-Real-Ip"][0], r.Method, r.RequestURI, r.UserAgent())
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
		w.Header().Set("Cache-Control", "max-age=60")
		tpl.Execute(w, fromSearch(rTags[modValue]))
	}
}

func main() {
	http.HandleFunc("/", index)
	//http.Handle("/static", http.FileServer(http.Dir("./static/")))
	serveSingle("/favicon.ico", "favicon.ico")
	serveSingle("/jquery.unveil.min.js", "jquery.unveil.min.js")
	serveSingle("/base_min.css", "base_min.css")
	log.Println(http.ListenAndServe(":8080", nil))
}
