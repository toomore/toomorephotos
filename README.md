# Toomore Photos

Flickr 照片展示網站，以 Go 開發，支援 RSS/Atom、sitemap。  
A Flickr photo gallery built with Go, supporting RSS/Atom feeds and sitemap.

---

## 功能特色 / Features

- **Flickr API 整合**：透過 Flickr API 取得照片資料 / Flickr API integration for photo data
- **首頁輪替**：依 `tags.txt` 依時間輪替顯示不同標籤的照片 / Homepage rotates photos by tags based on time
- **照片詳細頁**：完整顯示標題、描述、標籤、授權、地圖 / Photo detail page with title, description, tags, license, map
- **RSS/Atom feeds**：支援訂閱，含 30 分鐘 TTL 快取 / Feed support with 30-minute TTL cache
- **XML Sitemap**：供搜尋引擎索引 / XML sitemap for search engines
- **ETag 快取**：靜態檔與頁面快取 / ETag caching for static files and pages
- **響應式設計**：lazy loading 圖片 / Responsive design with lazy loading
- **本地資料庫**：可選 PostgreSQL 儲存照片 metadata，減少對 Flickr API 依賴 / Optional PostgreSQL for local photo metadata storage

---

## 需求 / Requirements

- Go 1.21+
- Flickr API 金鑰（見下方環境變數）
- `tags.txt`：每行一個標籤，需手動建立（此檔在 .gitignore 中）

---

## 快速開始 / Quick Start

```bash
# 1. Get dependencies
go get -v -a ./...

# 2. Build
make build
# or: go build -v ./

# 3. Set environment variables (see below)
export FLICKRAPIKEY=...
export FLICKRSECRET=...
export FLICKRUSERTOKEN=...
export FLICKRUSER=...

# 4. Create tags.txt (one tag per line)

# 5. Run
./toomorephotos
```

---

## 環境變數 / Environment Variables

| Variable | Description |
|----------|-------------|
| FLICKRAPIKEY | Flickr API Key |
| FLICKRSECRET | Flickr API Secret |
| FLICKRUSERTOKEN | Flickr User Token |
| FLICKRUSER | Flickr User ID |
| REDIS_URL | (Optional) Redis URL for persistent cache, e.g. `redis://localhost:6379`. If not set, uses in-memory cache. |
| DATABASE_URL | (Optional) PostgreSQL URL for local photo metadata. If set, app uses DB first and fallback to Flickr API. If not set, uses Flickr + cache only. |

---

## 快取 TTL / Cache TTL

Redis/記憶體快取的有效時間（未設定 REDIS_URL 時使用記憶體）：

| 項目 | TTL | 說明 |
|------|-----|------|
| 照片詳情 (PhotosGetInfo) | 30 天 | 標題、描述等 metadata |
| 照片尺寸 | 365 天 | 尺寸不變，長期快取 |
| 相關作品 | 1 小時 | 同主題 + 其他主題混入 |
| 首頁 tag 搜尋 | 10 分鐘 | 依 tag 輪替 |
| Sitemap / RSS / Atom | 30 分鐘 | 全站列表與 feeds |

---

## 靜態資源（可選） / Static Assets (Optional)

- 下載 [unveil.js](https://github.com/luis-almeida/unveil) 並命名為 `jquery.unveil.js`
- 執行 `make minify` 壓縮 CSS/JS（需先 `go install github.com/tdewolff/minify/v2/cmd/minify@latest`）

---

## Docker Compose

```bash
# 1. Set environment variables (or use .env file)
export FLICKRAPIKEY=...
export FLICKRSECRET=...
export FLICKRUSERTOKEN=...
export FLICKRUSER=...

# 2. (Optional) Create tags.txt and uncomment volumes in docker-compose.yml for custom tags

# 3. Start app + Redis
docker compose up --build -d
```

Runs app on port 8080 with Redis-backed cache (persists across restarts). Uses built-in default tag "photo" unless you mount custom tags.txt.

docker-compose 會自動啟動 PostgreSQL；首次啟動時 app 會建立 `photos`、`photo_tags` 表。需執行 sync 將 Flickr 照片 metadata 寫入 DB。

---

## 執行與管理 / Run & Manage

| Command | Description |
|---------|-------------|
| `./toomorephotos` | Single instance (port 8080) |
| `./toomorephotos -p :8081` | Specify port |
| `./toomorephotos -sync` | 從 Flickr 同步照片 metadata 至 DB 後退出 / Sync photo metadata from Flickr to DB, then exit |
| `REDIS_URL=redis://localhost:6379 ./toomorephotos` | Use Redis cache |
| `./toomorephotos >> ./log.log 2>&1 &` | Run in background |
| `make start` | Start 4 instances (ports 8080–8083) |
| `make stop` | Stop all instances |
| `make restart` | Restart |

### Sync 指令 / Sync Command

有設定 `DATABASE_URL` 時，可執行 sync 將 Flickr 照片 metadata 寫入本地 DB：

```bash
# Docker Compose
docker compose run --rm app ./toomorephotos -sync

# 本地（需 DATABASE_URL 與 Flickr 環境變數）
DATABASE_URL=postgres://... ./toomorephotos -sync
```

**建議**：應只從單一 instance 執行 sync，避免同時執行。可定期以 cron 或 systemd timer 排程。

### 資料庫備份 / Database Backup

若 DB 為 metadata 唯一來源，建議定期備份：

```bash
# pg_dump
docker compose exec postgres pg_dump -U toomorephotos toomorephotos > backup.sql

# 或備份 postgres 的 volume
```

---

## 程式架構 / Project Structure

| File | Responsibility |
|------|----------------|
| `main.go` | Entry point, route registration, -sync flag |
| `app.go` | App struct, NewApp, DB init |
| `handlers.go` | HTTP handlers |
| `feed.go` | RSS/Atom, feed cache |
| `flickr.go` | Flickr API, getTags, DB-first logic |
| `sync.go` | Sync: Flickr → DB |
| `db/` | PostgreSQL schema, photos CRUD |

See [CLAUDE.md](CLAUDE.md) for full architecture documentation.

---

## HTTP Routes

| Path | Description |
|------|-------------|
| `/` | 首頁 / Homepage |
| `/p/{photoid}` | 照片詳細頁 / Photo detail |
| `/sitemap/` | XML sitemap |
| `/rss` | RSS feed |
| `/atom` | Atom feed |
| `/health` | Health check |
