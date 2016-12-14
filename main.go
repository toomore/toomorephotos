package main

import (
	"bufio"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/toomore/lazyflickrgo/flickr"
	"github.com/toomore/lazyflickrgo/jsonstruct"
)

var (
	f             *flickr.Flickr
	httpPort      = flag.String("p", ":8080", "HTTP port")
	licenses      map[string]jsonstruct.License
	photoPageExpr = regexp.MustCompile(`/p/([0-9]+)-?(.+)?`)
	rTags         []string
	tplIndex      *template.Template
	tplPhoto      *template.Template
	tplSitemap    *template.Template
	userID        string
)

func getTags(result *[]string) {
	file, err := os.Open("./tags.txt")
	defer file.Close()

	if err != nil {
		log.Panic(err)
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		*result = append(*result, scanner.Text())
	}
	log.Println("Tags:", *result)
}

func init() {
	getTags(&rTags)
	funcs := template.FuncMap{
		"isHTML": func(content string) (template.HTML, error) {
			return template.HTML(strings.Replace(content, "\n", "<br>", -1)), nil
		},
		"isAltDesc": func(content string) (string, error) {
			return strings.Replace(content, "\n", " ", -1), nil
		},
		"replaceHover": func(content string) string {
			return strings.Replace(content, " ", "-", -1)
		},
		"toKeywords": func(data jsonstruct.Tags) (string, error) {
			str := make([]string, len(data.Tag))
			for i, tag := range data.Tag {
				str[i] = tag.Raw
			}
			return strings.Join(str, ","), nil
		},
		"licensesName": func(lno string) string {
			return licenses[lno].Name
		},
		"licensesURL": func(lno string) string {
			return licenses[lno].URL
		},
		"iso8601": func(stamp string) string {
			ts, err := time.Parse("2006-01-02 15:04:05", stamp)
			if err == nil {
				return ts.Format(time.RFC3339)
			}
			times, _ := strconv.Atoi(stamp)
			return time.Unix(int64(times), 0).Format(time.RFC3339)
		},
		"fileFormat": func(format string) string {
			if format == "png" {
				return "image/png"
			}
			return "image/jpeg"
		},
	}
	tplIndex = template.Must(template.Must(template.ParseFiles("./base.htm")).Funcs(funcs).ParseFiles("./index.htm"))
	tplPhoto = template.Must(template.Must(template.ParseFiles("./base.htm")).Funcs(funcs).ParseFiles("./photo.htm"))
	tplSitemap = template.Must(template.ParseFiles("./sitemap.htm"))

	f = flickr.NewFlickr(os.Getenv("FLICKRAPIKEY"), os.Getenv("FLICKRSECRET"))
	f.AuthToken = os.Getenv("FLICKRUSERTOKEN")
	userID = os.Getenv("FLICKRUSER")

	log.Println("Init flickr licenses list ...")
	licenses = make(map[string]jsonstruct.License)
	for _, data := range f.PhotosLicensesGetInfo().Licenses.License {
		if data.URL == "" {
			data.URL = "https://toomore.net/"
		}
		licenses[data.ID] = data
	}
	log.Printf("Licenses: %+v", licenses)
}

func logs(r *http.Request, note string) {
	log.Println(r.Header.Get("X-Real-Ip"), r.Method, r.RequestURI, r.UserAgent(), note)
}

func fromSearch(tags string) []jsonstruct.Photo {
	args := make(map[string]string)
	args["tags"] = tags
	args["tag_mode"] = "all"
	args["sort"] = "date-posted-desc"
	args["user_id"] = userID

	var result []jsonstruct.Photo
	for _, val := range f.PhotosSearch(args) {
		result = append(result, val.Photos.Photo...)
	}

	return result
}

func serveSingle(pattern string, filename string) {
	http.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		logs(r, "[static]")
		w.Header().Set("Cache-Control", "public, max-age=900")
		http.ServeFile(w, r, filename)
	})
}

func index(w http.ResponseWriter, r *http.Request) {
	logs(r, "")
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
	logs(r, "")
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
				notFound(w, r)
			}
		} else {
			notFound(w, r)
		}
	}
}

func sitemap(w http.ResponseWriter, r *http.Request) {
	args := make(map[string]string)
	args["sort"] = "date-posted-desc"
	args["user_id"] = userID

	var result []jsonstruct.Photo
	for _, val := range f.PhotosSearch(args) {
		result = append(result, val.Photos.Photo...)
	}
	tplSitemap.Execute(w, result)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	logs(r, "[!] Page Not Found")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("Maybe not in this timeline ... (35.701099, 139.738557)"))
}

func main() {
	flag.Parse()
	http.HandleFunc("/", index)
	http.HandleFunc("/p/", photo)
	http.HandleFunc("/sitemap/", sitemap)
	//http.Handle("/static", http.FileServer(http.Dir("./static/")))
	serveSingle("/favicon.ico", "favicon.ico")
	serveSingle("/jquery.unveil.min.js", "jquery.unveil.min.js")
	serveSingle("/base_min.css", "base_min.css")
	serveSingle("/robots.txt", "robots.txt")
	log.Println("HTTP Port:", *httpPort)
	log.Println(http.ListenAndServe(*httpPort, nil))
}
