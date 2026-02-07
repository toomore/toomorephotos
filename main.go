package main

import (
	"bufio"
	"crypto/md5"
	"flag"
	"fmt"
	"hash"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/feeds"
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
	hashCache     map[string]string

	feedCache   *feeds.Feed
	feedCacheAt time.Time
	feedCacheMu sync.RWMutex
	feedCacheTTL = 10 * time.Minute
)

func getTags(result *[]string) {
	file, err := os.Open("./tags.txt")
	if err != nil {
		log.Fatalf("無法讀取 tags.txt，請確認檔案存在並編輯加入至少一個標籤: %v", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		*result = append(*result, scanner.Text())
	}
	log.Println("Tags:", *result)
}

var funcs = template.FuncMap{
	"isHTML": func(content string) (template.HTML, error) {
		return template.HTML(strings.Replace(content, "\n", "<br>", -1)), nil
	},
	"isAltDesc": func(content string) string {
		return strings.Replace(content, "\n", " ", -1)
	},
	"isJSONContent": func(content string) string {
		content = strings.Replace(content, "\"", "\\u0022", -1)
		content = strings.Replace(content, "<", "\\u003c", -1)
		content = strings.Replace(content, ">", "\\u003e", -1)
		content = strings.Replace(content, "&", "\\u0026", -1)
		content = strings.Replace(content, "+", "\\u002b", -1)
		return content
	},
	"replaceHover": func(content string) string {
		return strings.Replace(content, " ", "-", -1)
	},
	"toKeywords": func(data jsonstruct.Tags) string {
		str := make([]string, len(data.Tag))
		for i, tag := range data.Tag {
			str[i] = tag.Raw
		}
		return strings.Join(str, ",")
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
}

func init() {
	getTags(&rTags)
	if len(rTags) == 0 {
		log.Fatal("tags.txt 為空，請編輯加入至少一個標籤")
	}
	tplIndex = template.Must(template.Must(template.ParseFiles("./base.htm")).Funcs(funcs).ParseFiles("./index.htm"))
	tplPhoto = template.Must(template.Must(template.ParseFiles("./base_2019.html")).Funcs(funcs).ParseFiles("./photo.htm"))
	tplSitemap = template.Must(template.ParseFiles("./sitemap.htm"))

	requiredEnv := []string{"FLICKRAPIKEY", "FLICKRSECRET", "FLICKRUSERTOKEN", "FLICKRUSER"}
	for _, key := range requiredEnv {
		if os.Getenv(key) == "" {
			log.Fatalf("缺少必要環境變數 %s，請設定後再啟動", key)
		}
	}
	f = flickr.NewFlickr(os.Getenv("FLICKRAPIKEY"), os.Getenv("FLICKRSECRET"))
	f.AuthToken = os.Getenv("FLICKRUSERTOKEN")
	userID = os.Getenv("FLICKRUSER")

	log.Println("Init flickr licenses list ...")
	licenses = make(map[string]jsonstruct.License)
	for _, data := range f.PhotosLicensesGetInfo().Licenses.License {
		if data.URL == "" {
			data.URL = "https://toomore.net/"
		}
		licenseID := strconv.FormatInt(data.ID, 10)
		licenses[licenseID] = data
	}
	log.Printf("Licenses: %+v", licenses)
	hashCache = make(map[string]string)
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
	if file, err := os.ReadFile(filename); err == nil {
		h := md5.New()
		h.Write(file)
		hashCache[filename] = fmt.Sprintf("W/\"%x\"", h.Sum(nil))
	}

	http.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		logs(r, "[static]")
		if r.Header.Get("If-None-Match") == hashCache[filename] {
			logs(r, "[304]")
			w.WriteHeader(http.StatusNotModified)
		} else {
			//w.Header().Set("Cache-Control", "public, max-age=900")
			w.Header().Set("ETag", hashCache[filename])
			http.ServeFile(w, r, filename)
		}
	})
}

func index(w http.ResponseWriter, r *http.Request) {
	logs(r, "")
	var modValue int
	var err error
	if modValue, err = strconv.Atoi(r.URL.Query().Get("t")); err == nil {
		modValue = int(math.Mod(float64(modValue), float64(len(rTags))))
	} else {
		modValue = int(math.Mod(float64(time.Now().Minute()), float64(len(rTags))))
	}
	etagStr := fmt.Sprintf("W/\"%d-%s\"", modValue, rTags[modValue])

	w.Header().Set("X-Tags", rTags[modValue])
	w.Header().Set("X-Github", "github.com/toomore/toomorephotos")

	if r.Header.Get("If-None-Match") == etagStr {
		logs(r, "[304]")
		w.WriteHeader(http.StatusNotModified)
	} else {
		w.Header().Set("ETag", etagStr)
		w.Header().Set("Cache-Control", "max-age=120")
		result := fromSearch(rTags[modValue])
		min := 30
		if len(result) < 30 {
			min = len(result)
		}
		data := struct {
			R []jsonstruct.Photo
			L []jsonstruct.Photo
		}{result, result[:min]}
		if err := tplIndex.Execute(w, data); err != nil {
			log.Printf("template execute error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}
}

func photo(w http.ResponseWriter, r *http.Request) {
	logs(r, "")
	match := photoPageExpr.FindStringSubmatch(r.RequestURI)
	var photono string
	if len(match) >= 2 {
		photono = match[1]
	}

	if photono == "" {
		notFound(w, r)
		return
	}
	var photoinfo jsonstruct.PhotosGetInfo
	photoinfo = f.PhotosGetInfo(photono)

	var etaghex hash.Hash
	var etagStr string
	if photoinfo.Common.Stat == "ok" {
		etaghex = md5.New()
		io.WriteString(etaghex, photoinfo.Photo.Title.Content)
		io.WriteString(etaghex, photoinfo.Photo.Description.Content)
		etagStr = fmt.Sprintf("W/\"%x\"", etaghex.Sum(nil))
	} else {
		notFound(w, r)
		return
	}

	if photoinfo.Photo.Owner.Nsid != "92438116@N00" {
		notFound(w, r)
		return
	}

	if r.Header.Get("If-None-Match") == etagStr {
		logs(r, "[304]")
		w.WriteHeader(http.StatusNotModified)
	} else {
		w.Header().Set("ETag", etagStr)
		if err := tplPhoto.Execute(w, photoinfo.Photo); err != nil {
			log.Printf("template execute error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}
}

func allPhotos(result *[]jsonstruct.Photo) {
	args := make(map[string]string)
	args["sort"] = "date-posted-desc"
	args["user_id"] = userID

	for _, val := range f.PhotosSearch(args) {
		*result = append(*result, val.Photos.Photo...)
	}
}

func createFeeds(data []jsonstruct.Photo) *feeds.Feed {
	feed := &feeds.Feed{
		Title:       "Toomore Photos",
		Link:        &feeds.Link{Href: "https://photos.toomore.net/"},
		Description: "From here to see what I see.",
		Author:      &feeds.Author{Name: "Toomore Chiang", Email: "toomore0929@gmail.com"},
	}

	var photoinfo jsonstruct.PhotosGetInfo
	var times int
	var updated time.Time
	for i, v := range data[:min(100, len(data))] {
		photoinfo = f.PhotosGetInfo(v.ID)
		times, _ = strconv.Atoi(photoinfo.Photo.Dates.Posted)
		updated = time.Unix(int64(times), 0)

		if i == 0 {
			feed.Updated = updated
		}

		desc := fmt.Sprintf(`<a href="https://photos.toomore.net/p/%s"><img src="https://photos.toomore.net/f/%d/%s/%s/%s.jpg"></a>%s<br>Photo by <a href="https://toomore.net/">Toomore</a><br><img width=1 height=3 src="https://photos.toomore.net/fr?r=%s">`, photoinfo.Photo.ID, photoinfo.Photo.Farm, photoinfo.Photo.Server, photoinfo.Photo.Secret, photoinfo.Photo.ID, strings.Replace(photoinfo.Photo.Description.Content, "\n", "<br>", -1), photoinfo.Photo.ID)

		feed.Items = append(feed.Items, &feeds.Item{
			Id:          fmt.Sprintf("https://photos.toomore.net/p/%s", v.ID),
			Title:       fmt.Sprintf("%s (%s)", v.Title, v.ID),
			Link:        &feeds.Link{Href: fmt.Sprintf("https://photos.toomore.net/p/%s", v.ID)},
			Description: desc,
			Updated:     updated,
			Author:      &feeds.Author{Name: "toomore0929@gmail.com (Toomore Chiang)"},
		})
	}
	return feed
}

func getCachedFeed() *feeds.Feed {
	feedCacheMu.RLock()
	if feedCache != nil && time.Since(feedCacheAt) < feedCacheTTL {
		feed := feedCache
		feedCacheMu.RUnlock()
		return feed
	}
	feedCacheMu.RUnlock()

	feedCacheMu.Lock()
	defer feedCacheMu.Unlock()
	if feedCache != nil && time.Since(feedCacheAt) < feedCacheTTL {
		return feedCache
	}
	var result []jsonstruct.Photo
	allPhotos(&result)
	feedCache = createFeeds(result)
	feedCacheAt = time.Now()
	return feedCache
}

func rss(w http.ResponseWriter, r *http.Request) {
	feed := getCachedFeed()
	rssFeed := feeds.Rss{Feed: feed}
	rssfeed := rssFeed.RssFeed()
	rssfeed.Language = "zh"

	rss, err := feeds.ToXML(rssfeed)
	if err != nil {
		log.Printf("feeds.ToXML error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Write([]byte(rss))
}

func atom(w http.ResponseWriter, r *http.Request) {
	feed := getCachedFeed()
	atomFeed := feeds.Atom{Feed: feed}
	atomfeed := atomFeed.AtomFeed()

	atom, err := feeds.ToXML(atomfeed)
	if err != nil {
		log.Printf("feeds.ToXML error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Write([]byte(atom))
}

func sitemap(w http.ResponseWriter, r *http.Request) {
	var result []jsonstruct.Photo
	allPhotos(&result)
	tags := make([]int, len(rTags))
	for i := range rTags {
		tags[i] = i
	}
	data := struct {
		R []jsonstruct.Photo
		T []int
	}{result, tags}
	if err := tplSitemap.Execute(w, data); err != nil {
		log.Printf("template execute error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func notFound(w http.ResponseWriter, r *http.Request) {
	logs(r, "[!] Page Not Found")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("Maybe not in this timeline ... (35.701099, 139.738557)"))
}

func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func main() {
	flag.Parse()
	http.HandleFunc("/", index)
	http.HandleFunc("/p/", photo)
	http.HandleFunc("/sitemap/", sitemap)
	http.HandleFunc("/rss", rss)
	http.HandleFunc("/atom", atom)
	http.HandleFunc("/fr", notFound)
	http.HandleFunc("/health", health)
	//http.Handle("/static", http.FileServer(http.Dir("./static/")))
	serveSingle("/favicon.ico", "favicon.ico")
	serveSingle("/jquery.unveil.min.js", "jquery.unveil.min.js")
	serveSingle("/base_min.css", "base_min.css")
	serveSingle("/base_photo_min.css", "base_photo_min.css")
	serveSingle("/robots.txt", "robots.txt")
	log.Println("HTTP Port:", *httpPort)
	log.Println(http.ListenAndServe(*httpPort, nil))
}
