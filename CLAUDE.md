# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 專案概述

Toomore Photos 是一個使用 Go 開發的 Flickr 照片展示網站。主程式透過 Flickr API 取得照片資料，並使用 Go template 渲染 HTML 頁面。支援 RSS/Atom feeds、sitemap 生成等功能。

## 環境設定

### 必要環境變數

程式需要以下 Flickr API 相關環境變數才能正常運作（main.go:102-104）：

- `FLICKRAPIKEY`: Flickr API Key
- `FLICKRSECRET`: Flickr API Secret
- `FLICKRUSERTOKEN`: Flickr User Token
- `FLICKRUSER`: Flickr User ID

### Tags 檔案

- 程式啟動時會讀取 `tags.txt` 檔案（main.go:41-52）
- 此檔案包含標籤列表，每行一個標籤
- 首頁會根據時間輪替顯示不同標籤的照片
- **注意**: `tags.txt` 在 .gitignore 中，需手動建立

## 常用指令

### 建置專案

```bash
# 取得相依套件
go get -v -a ./...

# 編譯（開發環境）
make build
# 或直接執行
go build -v ./
```

### CSS/JS 壓縮

專案使用 minify 工具壓縮靜態資源：

```bash
# 安裝 minify 工具
go install github.com/tdewolff/minify/v2/cmd/minify@latest

# 壓縮所有 CSS 和 JS
make minify
```

壓縮檔案輸出：
- `base.css` → `base_min.css`
- `base_photo.css` → `base_photo_min.css`
- `jquery.unveil.js` → `jquery.unveil.min.js`

### 執行程式

```bash
# 單一實例（預設 port 8080）
./toomorephotos

# 指定 port
./toomorephotos -p :8081

# 背景執行（記錄 log）
./toomorephotos >> ./log.log 2>&1 &

# 啟動多個實例（load balancing）
make start  # 啟動 4 個實例在 port 8080-8083
```

### 程式管理

```bash
# 停止所有執行中的實例
make stop

# 重新啟動
make restart
```

## 程式架構

### 核心檔案

- **main.go**: 唯一的 Go 原始檔，包含所有邏輯
  - HTTP handlers（首頁、照片頁、feeds、sitemap）
  - Template 渲染函式
  - Flickr API 整合
  - ETag 快取機制

### 模板系統

專案使用 Go html/template，採用 base template + content template 組合：

1. **首頁** (main.go:97)
   - Base: `base.htm`
   - Content: `index.htm`

2. **照片頁** (main.go:98)
   - Base: `base_2019.html`
   - Content: `photo.htm`

3. **Sitemap** (main.go:99)
   - Template: `sitemap.htm`

### Template 自訂函式

在 main.go:54-93 定義了多個 template 函式：

- `isHTML`: 將換行轉為 `<br>` 標籤
- `isAltDesc`: 移除換行符號
- `isJSONContent`: 跳脫 JSON 特殊字元
- `toKeywords`: 將 tags 轉為逗號分隔字串
- `licensesName`, `licensesURL`: 取得授權資訊
- `iso8601`: 時間格式轉換

### HTTP Routes

- `/` - 首頁（依 tags 輪替顯示照片）
- `/p/{photoid}` - 照片詳細頁
- `/sitemap/` - XML sitemap
- `/rss` - RSS feed
- `/atom` - Atom feed
- `/health` - Health check endpoint
- 靜態檔案透過 `serveSingle()` 函式提供

### 快取機制

- **Redis（選用）**: 設定 `REDIS_URL` 時使用 Redis 快取，重啟後仍保留；未設定則使用記憶體快取
- **PostgreSQL（選用）**: 設定 `DATABASE_URL` 時使用本地 DB 儲存照片 metadata；讀取時 DB 優先，無資料時 fallback 至 Flickr API，fallback 取得後寫回 DB
- **快取項目與 TTL**: 照片 info 30 天、照片尺寸 365 天、相關作品 1h、首頁 10min、sitemap/feed 30min
- **首頁**: 使用 ETag 基於 tag 內容產生
- **照片頁**: ETag 基於照片標題和描述的 MD5
- **靜態檔案**: ETag 基於檔案內容的 MD5
- 前端使用 jquery.unveil.js 實作 lazy loading

### 本地資料庫（db 套件）

- `db/db.go`: 連線、InitSchema（啟動時建表，CREATE TABLE IF NOT EXISTS）
- `db/photos.go`: GetPhoto、UpsertPhoto、GetPhotosByTag、GetAllPhotos、GetRelatedPhotos
- `photos` 表：photo_id, info_json (JSONB), width, height, fetched_at
- `photo_tags` 表：photo_id, tag（供 tag 查詢）
- `./toomorephotos -sync`: 從 Flickr 同步 metadata 至 DB

### Docker Compose

- `docker compose up --build` 啟動 app + Redis + PostgreSQL
- 需提供 `tags.txt`（volume 掛載）及 Flickr 環境變數
- 首次啟動自動建表；可用 `docker compose run --rm app ./toomorephotos -sync` 同步照片

### 依賴套件

- `github.com/toomore/lazyflickrgo`: Flickr API 客戶端
- `github.com/gorilla/feeds`: RSS/Atom feed 生成
- `github.com/redis/go-redis/v9`: Redis 客戶端（選用）
- `github.com/jackc/pgx/v5`: PostgreSQL 客戶端（選用）

## 注意事項

- 程式碼中硬編碼了特定的 Flickr User NSID (`92438116@N00`，main.go:224)，非此 User 的照片會回傳 404
- 首頁預設顯示 30 張照片（main.go:179-182）
- Feed 輸出最近 100 張照片（main.go:277）
- 程式使用 Go 1.12 module 系統
- 所有 log 輸出到 stdout/stderr
