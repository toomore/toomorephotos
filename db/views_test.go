package db

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/toomore/lazyflickrgo/jsonstruct"
)

// TestViewsSQL exercises the view queries against a real PostgreSQL, because
// the SQL is only checked by the server: set TEST_DATABASE_URL to run it.
//
//	docker run --rm -d -p 15432:5432 -e POSTGRES_PASSWORD=x --name pgtest postgres:17-alpine
//	TEST_DATABASE_URL=postgres://postgres:x@127.0.0.1:15432/postgres?sslmode=disable go test ./db/
func TestViewsSQL(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL 未設定，略過需要 PostgreSQL 的測試")
	}
	ctx := context.Background()
	d, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	if err := d.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	if _, err := d.pool.Exec(ctx, `TRUNCATE photo_views`); err != nil {
		t.Fatalf("TRUNCATE: %v", err)
	}

	if err := d.AddViews(ctx, map[string]int64{"111": 3, "222": 1}); err != nil {
		t.Fatalf("AddViews: %v", err)
	}
	// 同一天再寫一次要累加，而不是覆蓋
	if err := d.AddViews(ctx, map[string]int64{"111": 2}); err != nil {
		t.Fatalf("AddViews 第二次: %v", err)
	}
	if err := d.AddViews(ctx, nil); err != nil {
		t.Fatalf("AddViews 空 map 應該是 no-op: %v", err)
	}

	total, err := d.TotalViews(ctx, 7)
	if err != nil {
		t.Fatalf("TotalViews: %v", err)
	}
	if total != 6 {
		t.Errorf("TotalViews = %d, want 6", total)
	}

	top, err := d.TopViews(ctx, 7, 10)
	if err != nil {
		t.Fatalf("TopViews: %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("TopViews 回傳 %d 筆, want 2", len(top))
	}
	if top[0].PhotoID != "111" || top[0].Views != 5 {
		t.Errorf("第一名 = %+v, want 111 共 5 次", top[0])
	}
	if top[1].PhotoID != "222" || top[1].Views != 1 {
		t.Errorf("第二名 = %+v, want 222 共 1 次", top[1])
	}
}

// TestGetPhotosByTagIsCaseInsensitive guards the mismatch found on 2026-09-18:
// photo_tags keeps the tag as typed on Flickr ("Tokyo") while tags.txt is lower
// case, so an exact match returned nothing and the homepage rendered empty.
func TestGetPhotosByTagIsCaseInsensitive(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL 未設定，略過需要 PostgreSQL 的測試")
	}
	ctx := context.Background()
	d, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	if err := d.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	if _, err := d.pool.Exec(ctx, `TRUNCATE photos CASCADE`); err != nil {
		t.Fatalf("TRUNCATE: %v", err)
	}

	// jsonstruct 的 tag 型別未匯出，只能從 JSON 還原
	var info jsonstruct.PhotosGetInfo
	raw := `{"photo":{"id":"555","secret":"s","server":"1","farm":6,
	          "title":{"_content":"t"},
	          "tags":{"tag":[{"raw":"Tokyo"}]},
	          "dates":{"posted":"1600000000"}}}`
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := d.UpsertPhoto(ctx, "555", info, 1024, 683); err != nil {
		t.Fatalf("UpsertPhoto: %v", err)
	}

	for _, want := range []string{"tokyo", "Tokyo", "TOKYO"} {
		photos, err := d.GetPhotosByTag(ctx, want)
		if err != nil {
			t.Fatalf("GetPhotosByTag(%q): %v", want, err)
		}
		if len(photos) != 1 {
			t.Errorf("GetPhotosByTag(%q) 回傳 %d 筆, want 1", want, len(photos))
		}
	}
	if photos, _ := d.GetPhotosByTag(ctx, "kyoto"); len(photos) != 0 {
		t.Errorf("不相干的 tag 竟然回傳 %d 筆", len(photos))
	}
}
