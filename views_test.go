package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRecorder captures what the handler decided to count.
type fakeRecorder struct {
	mu  sync.Mutex
	ids []string
}

func (f *fakeRecorder) Record(photoID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, photoID)
}

func (f *fakeRecorder) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ids...)
}

const chromeUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"

func beacon(t *testing.T, a *App, method, target, ua, ip string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, nil)
	r.Header.Set("User-Agent", ua)
	r.Header.Set("CF-Connecting-IP", ip)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	a.view(w, r)
	return w
}

func TestViewBeacon(t *testing.T) {
	for _, tt := range []struct {
		name     string
		method   string
		target   string
		ua       string
		ip       string
		headers  map[string]string
		wantCode int
		wantRec  int
	}{
		{name: "一般瀏覽器", method: "POST", target: "/v?p=" + testPhotoID, ua: chromeUA, ip: "203.0.113.1", wantCode: http.StatusNoContent, wantRec: 1},
		{name: "GET 不接受", method: "GET", target: "/v?p=" + testPhotoID, ua: chromeUA, ip: "203.0.113.2", wantCode: http.StatusMethodNotAllowed},
		{name: "缺少 photo id", method: "POST", target: "/v", ua: chromeUA, ip: "203.0.113.3", wantCode: http.StatusBadRequest},
		{name: "photo id 非數字", method: "POST", target: "/v?p=abc", ua: chromeUA, ip: "203.0.113.4", wantCode: http.StatusBadRequest},
		{name: "爬蟲", method: "POST", target: "/v?p=" + testPhotoID, ua: "Mozilla/5.0 (compatible; GPTBot/1.3)", ip: "203.0.113.5", wantCode: http.StatusNoContent},
		{name: "prefetch", method: "POST", target: "/v?p=" + testPhotoID, ua: chromeUA, ip: "203.0.113.6", headers: map[string]string{"Sec-Purpose": "prefetch;anonymous-client-ip"}, wantCode: http.StatusNoContent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestApp(t, []string{"a"})
			rec := &fakeRecorder{}
			a.Views = rec

			w := beacon(t, a, tt.method, tt.target, tt.ua, tt.ip, tt.headers)
			if w.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", w.Code, tt.wantCode)
			}
			if got := len(rec.recorded()); got != tt.wantRec {
				t.Errorf("記錄了 %d 筆，預期 %d 筆", got, tt.wantRec)
			}
			if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store (否則 Cloudflare 可能快取計數請求)", cc)
			}
		})
	}
}

func TestViewBeaconDedupesReloads(t *testing.T) {
	a := newTestApp(t, []string{"a"})
	rec := &fakeRecorder{}
	a.Views = rec

	for i := 0; i < 3; i++ {
		beacon(t, a, "POST", "/v?p="+testPhotoID, chromeUA, "203.0.113.7", nil)
	}
	// 同一天同一個訪客重整三次只算一次
	if got := rec.recorded(); len(got) != 1 {
		t.Errorf("同訪客重整 3 次記錄了 %d 筆，預期 1 筆", len(got))
	}
	// 換一個訪客要算
	beacon(t, a, "POST", "/v?p="+testPhotoID, chromeUA, "203.0.113.8", nil)
	if got := rec.recorded(); len(got) != 2 {
		t.Errorf("另一位訪客後共 %d 筆，預期 2 筆", len(got))
	}
}

// fakeSink records what the collector flushed.
type fakeSink struct {
	mu      sync.Mutex
	flushes []map[string]int64
}

func (f *fakeSink) AddViews(ctx context.Context, counts map[string]int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := make(map[string]int64, len(counts))
	for k, v := range counts {
		copied[k] = v
	}
	f.flushes = append(f.flushes, copied)
	return nil
}

func (f *fakeSink) totals() map[string]int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := make(map[string]int64)
	for _, m := range f.flushes {
		for k, v := range m {
			total[k] += v
		}
	}
	return total
}

func TestViewCollectorFlushesOnShutdown(t *testing.T) {
	sink := &fakeSink{}
	c := newViewCollector(sink)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()

	for i := 0; i < 5; i++ {
		c.Record("111")
	}
	c.Record("222")

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run 在 ctx 取消後沒有結束")
	}

	got := sink.totals()
	if got["111"] != 5 || got["222"] != 1 {
		t.Errorf("關機時寫出的統計 = %v，預期 111:5 222:1", got)
	}
}

func TestViewCollectorRecordNeverBlocks(t *testing.T) {
	c := newViewCollector(&fakeSink{}) // 不啟動 Run，佇列不會被消化
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < viewQueueSize*2; i++ {
			c.Record("111")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("佇列滿了之後 Record 卡住了")
	}
	if c.dropped.Load() == 0 {
		t.Error("佇列滿了卻沒有計入 dropped")
	}
}

func TestPhotoPageHasBeacon(t *testing.T) {
	a := newTestApp(t, []string{"a"})
	w := httptest.NewRecorder()
	a.photo(w, httptest.NewRequest("GET", "/p/"+testPhotoID, nil))

	want := `navigator.sendBeacon("/v?p=` + testPhotoID + `")`
	if !strings.Contains(w.Body.String(), want) {
		t.Errorf("照片頁沒有輸出 beacon：找不到 %s", want)
	}
}

func TestLogRedactsFlickrCredentials(t *testing.T) {
	var buf bytes.Buffer
	w := redactWriter{&buf}

	line := []byte("Get:  https://www.flickr.com/services/rest/?api_key=abc123&api_sig=def456&auth_token=tok789&format=json\n")
	n, err := w.Write(line)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(line) {
		t.Errorf("Write 回傳 %d，io.Writer 必須回報呼叫端的長度 %d", n, len(line))
	}
	out := buf.String()
	for _, secret := range []string{"abc123", "def456", "tok789"} {
		if strings.Contains(out, secret) {
			t.Errorf("log 仍含有機密 %q: %s", secret, out)
		}
	}
	for _, keep := range []string{"api_key=REDACTED", "api_sig=REDACTED", "auth_token=REDACTED", "format=json"} {
		if !strings.Contains(out, keep) {
			t.Errorf("log 少了 %q: %s", keep, out)
		}
	}
}

func TestCacheTTLForEmptyResult(t *testing.T) {
	full := 10 * time.Minute
	if got := cacheTTLFor(0, full); got != emptyCacheTTL {
		t.Errorf("空結果 TTL = %v, want %v（否則 Flickr 失敗一次就讓首頁空白整個 TTL）", got, emptyCacheTTL)
	}
	if got := cacheTTLFor(30, full); got != full {
		t.Errorf("正常結果 TTL = %v, want %v", got, full)
	}
}
