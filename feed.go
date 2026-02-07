package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/feeds"
	"github.com/toomore/lazyflickrgo/jsonstruct"
)

const feedConcurrency = 10

func (a *App) createFeeds(data []jsonstruct.Photo) *feeds.Feed {
	feed := &feeds.Feed{
		Title:       "Toomore Photos",
		Link:        &feeds.Link{Href: "https://photos.toomore.net/"},
		Description: "From here to see what I see.",
		Author:      &feeds.Author{Name: "Toomore Chiang", Email: "toomore0929@gmail.com"},
	}

	n := min(100, len(data))
	if n == 0 {
		return feed
	}

	results := make([]jsonstruct.PhotosGetInfo, n)
	sem := make(chan struct{}, feedConcurrency)
	var wg sync.WaitGroup

	for i, v := range data[:n] {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = a.getCachedPhotosGetInfo(id)
		}(i, v.ID)
	}
	wg.Wait()

	for i, v := range data[:n] {
		photoinfo := results[i]
		times, _ := strconv.Atoi(photoinfo.Photo.Dates.Posted)
		updated := time.Unix(int64(times), 0)

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

func (a *App) getCachedFeed() *feeds.Feed {
	a.feedCacheMu.RLock()
	if a.feedCache != nil && time.Since(a.feedCacheAt) < a.feedCacheTTL {
		feed := a.feedCache
		a.feedCacheMu.RUnlock()
		return feed
	}
	a.feedCacheMu.RUnlock()

	a.feedCacheMu.Lock()
	defer a.feedCacheMu.Unlock()
	if a.feedCache != nil && time.Since(a.feedCacheAt) < a.feedCacheTTL {
		return a.feedCache
	}
	result := a.getCachedAllPhotos()
	a.feedCache = a.createFeeds(result)
	a.feedCacheAt = time.Now()
	return a.feedCache
}

func (a *App) rss(w http.ResponseWriter, r *http.Request) {
	feed := a.getCachedFeed()
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

func (a *App) atom(w http.ResponseWriter, r *http.Request) {
	feed := a.getCachedFeed()
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
