package main

import (
	"bytes"
	"context"
	"html/template"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/toomore/lazyflickrgo/jsonstruct"
	"github.com/toomore/toomorephotos/cache"
)

const (
	testUserID  = "92438116@N00"
	testPhotoID = "22948409279"
)

// newTestApp builds an App whose cache already holds everything the handlers
// read, so no request reaches Flickr (App.Flickr is nil) or PostgreSQL.
func newTestApp(t *testing.T, tags []string) *App {
	t.Helper()
	funcs := newTemplateFuncs(nil)
	tplIndex := template.Must(template.Must(template.ParseFiles("./base.htm")).Funcs(funcs).ParseFiles("./index.htm"))
	tplPhoto := template.Must(template.Must(template.ParseFiles("./base_2019.html")).Funcs(funcs).ParseFiles("./photo.htm"))

	a := &App{
		Tags:          tags,
		UserID:        testUserID,
		TplIndex:      tplIndex,
		TplPhoto:      tplPhoto,
		PhotoPageExpr: regexp.MustCompile(`/p/([0-9]+)-?(.+)?`),
		Cache:         cache.NewMemoryCache(),
	}

	photo := jsonstruct.Photo{ID: testPhotoID, Title: "test", Secret: "05565b8464", Server: "5662", Farm: 6, Ispublic: 1}
	var info jsonstruct.PhotosGetInfo
	info.Common.Stat = "ok"
	info.Photo.ID = testPhotoID
	info.Photo.Secret = photo.Secret
	info.Photo.Server = photo.Server
	info.Photo.Farm = photo.Farm
	info.Photo.Owner.Nsid = testUserID
	info.Photo.Title.Content = "test"

	ctx := context.Background()
	for _, tag := range tags {
		a.Cache.Set(ctx, "index:"+tag, []jsonstruct.Photo{photo}, time.Hour)
	}
	a.Cache.Set(ctx, "sitemap", []jsonstruct.Photo{photo}, time.Hour)
	a.Cache.Set(ctx, "photo:"+testPhotoID, info, time.Hour)
	a.Cache.Set(ctx, "photosizes:"+testPhotoID, photoSizesVal{Width: 1024, Height: 683}, time.Hour)
	a.Cache.Set(ctx, "related:"+testPhotoID, []jsonstruct.Photo{}, time.Hour)
	return a
}

func TestIndexTagQuery(t *testing.T) {
	a := newTestApp(t, []string{"a", "b", "c"})
	for _, tt := range []struct {
		query string
		want  string
	}{
		{"?t=0", "a"},
		{"?t=2", "c"},
		{"?t=4", "b"},
		{"?t=-1", "c"},
		{"?t=-7", "c"},
		{"?t=-3", "a"},
	} {
		t.Run(tt.query, func(t *testing.T) {
			w := httptest.NewRecorder()
			a.index(w, httptest.NewRequest("GET", "/"+tt.query, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			if got := w.Header().Get("X-Tags"); got != tt.want {
				t.Errorf("X-Tags = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPhotoCacheControl(t *testing.T) {
	a := newTestApp(t, []string{"a"})

	w := httptest.NewRecorder()
	a.photo(w, httptest.NewRequest("GET", "/p/"+testPhotoID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Header().Get("Cache-Control") == "" {
		t.Error("200 response has no Cache-Control, so nginx proxy_cache never stores /p/")
	}

	r := httptest.NewRequest("GET", "/p/"+testPhotoID, nil)
	r.Header.Set("If-None-Match", w.Header().Get("ETag"))
	w = httptest.NewRecorder()
	a.photo(w, r)
	if w.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", w.Code)
	}
	if w.Header().Get("Cache-Control") == "" {
		t.Error("304 response has no Cache-Control")
	}
}

func TestLogsClientIP(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	r := httptest.NewRequest("GET", "/p/"+testPhotoID, nil)
	r.Header.Set("X-Real-Ip", "172.71.8.38")        // nginx sets this to the Cloudflare edge
	r.Header.Set("CF-Connecting-IP", "203.0.113.9") // the actual visitor
	logs(r, "")

	if out := buf.String(); !strings.Contains(out, " 203.0.113.9 GET ") {
		t.Errorf("log line does not start with the visitor IP: %q", out)
	}
}
