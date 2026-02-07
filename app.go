package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/toomore/lazyflickrgo/flickr"
	"github.com/toomore/lazyflickrgo/jsonstruct"
	"github.com/toomore/toomorephotos/cache"
	"github.com/toomore/toomorephotos/db"
)

type App struct {
	Flickr        *flickr.Flickr
	Licenses      map[string]jsonstruct.License
	Tags          []string
	UserID        string
	TplIndex      *template.Template
	TplPhoto      *template.Template
	TplSitemap    *template.Template
	HashCache     map[string]string
	PhotoPageExpr *regexp.Regexp

	Cache cache.Cache
	DB    *db.DB

	IndexCacheTTL        time.Duration
	PhotoCacheTTL        time.Duration
	PhotoSizesCacheTTL   time.Duration
	RelatedPhotosCacheTTL time.Duration
	SitemapCacheTTL      time.Duration
	FeedCacheTTL         time.Duration
}

func newTemplateFuncs(licenses map[string]jsonstruct.License) template.FuncMap {
	return template.FuncMap{
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
}

func NewApp() (*App, error) {
	tags, err := getTags("./tags.txt")
	if err != nil {
		return nil, fmt.Errorf("無法讀取 tags.txt，請確認檔案存在並編輯加入至少一個標籤: %w", err)
	}
	if len(tags) == 0 {
		return nil, &appError{msg: "tags.txt 為空，請編輯加入至少一個標籤"}
	}
	log.Println("Tags:", tags)

	requiredEnv := []string{"FLICKRAPIKEY", "FLICKRSECRET", "FLICKRUSERTOKEN", "FLICKRUSER"}
	for _, key := range requiredEnv {
		if os.Getenv(key) == "" {
			return nil, &appError{msg: "缺少必要環境變數 " + key + "，請設定後再啟動"}
		}
	}

	f := flickr.NewFlickr(os.Getenv("FLICKRAPIKEY"), os.Getenv("FLICKRSECRET"))
	f.AuthToken = os.Getenv("FLICKRUSERTOKEN")
	userID := os.Getenv("FLICKRUSER")

	licenses := make(map[string]jsonstruct.License)
	for _, data := range f.PhotosLicensesGetInfo().Licenses.License {
		if data.URL == "" {
			data.URL = "https://toomore.net/"
		}
		licenseID := strconv.FormatInt(data.ID, 10)
		licenses[licenseID] = data
	}
	log.Printf("Licenses: %+v", licenses)

	funcs := newTemplateFuncs(licenses)

	tIndex, err := template.ParseFiles("./base.htm")
	if err != nil {
		return nil, err
	}
	tplIndex, err := tIndex.Funcs(funcs).ParseFiles("./index.htm")
	if err != nil {
		return nil, err
	}

	tPhoto, err := template.ParseFiles("./base_2019.html")
	if err != nil {
		return nil, err
	}
	tplPhoto, err := tPhoto.Funcs(funcs).ParseFiles("./photo.htm")
	if err != nil {
		return nil, err
	}

	tplSitemap, err := template.ParseFiles("./sitemap.htm")
	if err != nil {
		return nil, err
	}

	var database *db.DB
	if url := os.Getenv("DATABASE_URL"); url != "" {
		var err error
		database, err = db.Open(context.Background(), url)
		if err != nil {
			return nil, fmt.Errorf("DATABASE_URL 連線失敗: %w", err)
		}
		if err := database.InitSchema(context.Background()); err != nil {
			database.Close()
			return nil, fmt.Errorf("DB schema 初始化失敗: %w", err)
		}
	} else {
		log.Println("DB: DATABASE_URL 未設定，跳過本地資料庫")
	}

	return &App{
		Flickr:               f,
		Licenses:             licenses,
		Tags:                 tags,
		UserID:               userID,
		TplIndex:             tplIndex,
		TplPhoto:             tplPhoto,
		TplSitemap:           tplSitemap,
		HashCache:            make(map[string]string),
		PhotoPageExpr:        regexp.MustCompile(`/p/([0-9]+)-?(.+)?`),
		Cache:                cache.New(),
		DB:                   database,
		IndexCacheTTL:        10 * time.Minute,
		PhotoCacheTTL:        30 * 24 * time.Hour,     // 30 天
		PhotoSizesCacheTTL:   365 * 24 * time.Hour,    // 365 天
		RelatedPhotosCacheTTL: 1 * time.Hour,
		SitemapCacheTTL:      30 * time.Minute,
		FeedCacheTTL:         30 * time.Minute,
	}, nil
}

type appError struct {
	msg string
}

func (e *appError) Error() string {
	return e.msg
}
