package main

import (
	"strings"
	"testing"

	"github.com/toomore/lazyflickrgo/jsonstruct"
)

func TestFeedImageURL(t *testing.T) {
	a := newTestApp(t, []string{"a"})
	feed := a.createFeeds([]jsonstruct.Photo{{ID: testPhotoID, Title: "test"}})

	// nginx only proxies `location ~ /f/(b|q|m)/farm/server/secret/id.jpg` to
	// Flickr; any other /f/ path falls through to the Go index page.
	want := `<img src="https://photos.toomore.net/f/b/6/5662/05565b8464/22948409279.jpg">`
	if desc := feed.Items[0].Description; !strings.Contains(desc, want) {
		t.Errorf("feed description has no %s\n got: %s", want, desc)
	}
}
