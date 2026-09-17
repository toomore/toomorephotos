# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 專案概述

Toomore Photos（https://photos.toomore.net）是一個以 Go 開發的 Flickr 照片展示網站。照片 metadata 可以存在本地 PostgreSQL，讀取時 DB 優先、缺資料才回頭打 Flickr API。支援 RSS/Atom feeds、sitemap、瀏覽計數。

## 環境設定

### 必要環境變數

| 變數 | 說明 |
|------|------|
| `FLICKRAPIKEY` | Flickr API Key |
| `FLICKRSECRET` | Flickr API Secret |
| `FLICKRUSERTOKEN` | Flickr User Token |
| `FLICKRUSER` | Flickr User NSID。照片頁會比對擁有者，非此 User 的照片回 404 |

### 選用環境變數

| 變數 | 說明 |
|------|------|
| `REDIS_URL` | 設定時用 Redis 快取（重啟後保留），未設定則用記憶體快取 |
| `DATABASE_URL` | 設定時啟用本地 PostgreSQL；未設定則只用 Flickr + 快取，且不記錄瀏覽數 |
| `MAPBOX_ACCESS_TOKEN` | 有設定時，有地理位置的照片會顯示靜態地圖 |
| `IMAGE_ARCHIVE_DIR` | `-archive-images` 的存檔根目錄，預設 `./archive` |

### Tags 檔案

- 啟動時讀取 `tags.txt`（每行一個標籤），首頁依此輪替顯示
- 此檔在 .gitignore 中，需手動建立；缺檔或內容為空會啟動失敗

## 程式架構

| 檔案 | 職責 |
|------|------|
| `main.go` | 進入點、CLI 旗標分派、路由註冊、http.Server 與 graceful shutdown |
| `app.go` | App struct、NewApp、template 函式、Flickr 併發控制、快取 TTL |
| `handlers.go` | HTTP handlers、ETag、`tagIndex`、`clientIP` |
| `flickr.go` | Flickr API 存取與「快取 → DB → Flickr」三層取值 |
| `feed.go` | RSS/Atom 產生與快取 |
| `views.go` | 瀏覽計數：beacon handler、批次收集器、排行輸出 |
| `sync.go` | `-sync`：Flickr metadata → DB |
| `audit.go` | `-audit`：DB metadata 豐富度稽核（唯讀） |
| `download.go` | `-archive-images`：把每張作品的所有尺寸與原圖下載到本地 |
| `logredact.go` | log 輸出遮蔽 Flickr 金鑰（lazyflickrgo 會印出完整請求網址） |
| `cache/` | Cache interface，Redis 與記憶體兩種實作 |
| `db/` | PostgreSQL：schema、photos CRUD、拍攝日期解析、瀏覽數 |

### 模板系統

Go html/template，base + content 組合：

- 首頁：`base.htm` + `index.htm`
- 照片頁：`base_2019.html` + `photo.htm`
- Sitemap：`sitemap.htm`

Template 自訂函式定義在 `app.go` 的 `newTemplateFuncs`：`isHTML`、`isAltDesc`、`isJSONContent`、`replaceHover`、`toKeywords`、`licensesName`、`licensesURL`、`iso8601`。

### HTTP Routes

| 路徑 | 說明 |
|------|------|
| `/` | 首頁，依 tag 輪替。`?t=N` 指定第幾個 tag |
| `/p/{photoid}` | 照片詳細頁 |
| `/sitemap/` | XML sitemap |
| `/rss`、`/atom` | Feeds（最近 100 張） |
| `/v?p={photoid}` | 瀏覽計數 beacon（POST，回 204） |
| `/health` | Health check |
| `/fr` | Feed 追蹤像素（回 404，只為了留下 log） |

靜態檔透過 `serveSingle()` 提供，帶內容 MD5 的 ETag。

**注意**：`/f/...`（圖片）與 `/maps/...`（地圖）**不是 Go 處理的**，由前面的 nginx 反向代理到 Flickr 與 Mapbox。模板裡的圖片網址格式必須對得上 nginx 的 `location ~ /f/(b|q|m)/farm/server/secret/id.jpg`，否則會掉進首頁 handler 回傳 HTML。

### 併發控制

`maxConcurrentFlickr = 4`（app.go）：所有會打 Flickr API 的路徑共用一個號誌加 singleflight。2026-07-30 與 08-03 兩次爬蟲暴衝時，沒有上限的 fan-out 讓記憶體漲到 1.4 GB 並拖垮整台主機，才加上這個限制。related photos 屬裝飾性資訊，取不到號誌就略過，頁面照常渲染。

### 快取

| 項目 | TTL |
|------|-----|
| 照片 info | 30 天 |
| 照片尺寸 | 365 天 |
| 相關作品 | 24 小時 |
| 首頁 tag 搜尋 | 10 分鐘 |
| Sitemap / feeds | 30 分鐘 |
| 瀏覽去重 | 24 小時 |

ETag：首頁基於 tag 與日期，照片頁基於標題與描述的 MD5，靜態檔基於內容 MD5。照片頁另外送 `Cache-Control: max-age=600`。

### 資料庫

- `photos`：photo_id、info_json (JSONB)、width、height、taken_real、fetched_at
- `photo_tags`：photo_id、tag
- `photo_views`：photo_id、day、views

`InitSchema` 在啟動時執行 `db/schema.sql`（全部 `IF NOT EXISTS`）。

### 瀏覽計數

照片頁載入後以 `navigator.sendBeacon` 打 `POST /v?p={id}`，`viewCollector` 批次累加後寫入 `photo_views`。計數放在瀏覽器而非 handler，是因為 Cloudflare 會直接用快取回應大部分照片頁，那些請求不會到達後端。會濾掉爬蟲 UA、prefetch、Do Not Track，以及同一訪客當天對同一張照片的重複載入（位址加鹽雜湊，不存原始 IP）。

## 常用指令

```bash
go build -v ./          # 或 make build
go test ./...           # 全部測試
gofmt -l .              # 格式檢查

# 需要真實 PostgreSQL 的 SQL 測試
docker run --rm -d -p 15432:5432 -e POSTGRES_PASSWORD=x --name pgtest postgres:17-alpine
TEST_DATABASE_URL='postgres://postgres:x@127.0.0.1:15432/postgres?sslmode=disable' go test ./db/
```

### CLI 旗標

| 指令 | 說明 |
|------|------|
| `./toomorephotos -p :8081` | 指定 port（預設 :8080） |
| `./toomorephotos -sync` | 從 Flickr 同步 metadata 至 DB 後退出 |
| `./toomorephotos -audit` | DB metadata 豐富度稽核（唯讀）後退出 |
| `./toomorephotos -backfill-dates` | 從描述解析拍攝日期回填 `taken_real` 後退出 |
| `./toomorephotos -archive-images` | 下載所有尺寸與原圖到本地存檔後退出 |
| `./toomorephotos -views` | 列出瀏覽排行後退出（`-views-days`、`-views-top`） |

`-archive-images` 另有 `-archive-dir`、`-limit`、`-photo-id`、`-skip-original`、`-only-original`、`-skip-xlarge`、`-dl-rate`。

### CSS/JS 壓縮

```bash
go install github.com/tdewolff/minify/v2/cmd/minify@latest
make minify   # base.css → base_min.css、base_photo.css → base_photo_min.css
```

Docker build 會自己做這一步。

## 正式環境

```
Cloudflare → nginx (octo2026) ─┬─ /、/p/、/rss …  → Go app (natsu:9900 → 容器 8080)
                               ├─ /f/(b|q|m)/…    → farmN.staticflickr.com（有磁碟快取）
                               └─ /maps/…         → api.mapbox.com
```

- **app 跑在 natsu 的 `/srv/toomorephotos`，用 docker compose，不是 `make`**——那台沒有裝 Go
- 部署：本機 push → natsu `git pull --ff-only origin master` → `docker compose build app && docker compose up -d app`。建置前先 `docker tag toomorephotos-app toomorephotos-app:rollback-<date>`
- nginx 設定在 octo2026 的 `/etc/nginx/conf.d/photos_toomore.conf`（不在這個 repo 裡），`/p/` 有 rate limit，`/f/` 有磁碟快取
- nginx 送來的 `X-Real-Ip` 是 Cloudflare 節點位址，真實訪客 IP 要看 `CF-Connecting-IP`
- 容器有 `mem_limit: 512m`，爆量時死的是容器而不是整台主機

## 注意事項

- 首頁的 `{{range .R}}` 會渲染該 tag 的**全部**照片，`.L`（前 30 張）只用在 `rel="prefetch"`
- Feed 輸出最近 100 張
- 所有 log 輸出到 stdout/stderr，並經 `redactWriter` 遮蔽金鑰
- `-sync` 應只從單一 instance 執行
