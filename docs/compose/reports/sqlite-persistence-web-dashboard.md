---
feature: SQLite 持久化 + Web Dashboard
status: delivered
specs: []
plans:
  - docs/compose/plans/2026-06-25-sqlite-persistence-web-dashboard.md
branch: main
commits: (pending)
---

# SQLite 持久化 + Web Dashboard — Final Report

## What Was Built

为 sunsetbot 添加了 SQLite 数据持久化和 Web 数据看板功能。每次从 sunsetbot.top API 获取朝霞/晚霞预报数据后，自动将鲜艳度（quality）、气溶胶（AOD）、时间、模型等信息写入 SQLite 数据库（`sunset.db`），已存在数据自动更新（upsert）。

同时提供了一个 Web 页面（默认端口 8080），支持按朝霞/晚霞筛选、按日期范围查询、折线图展示（鲜艳度 + 气溶胶，按 GFS/EC 模型分组），以及 CSV/JSON 数据导出。

## Architecture

### 新增文件
- **`store.go`** — SQLite 存储层。`Store` 类型封装数据库连接，提供 `InitStore()`、`UpsertRecord()`、`QueryRecords()`、`ExportCSV()`、`ExportJSON()` 方法。
- **`web.go`** — HTTP 服务器。`StartWebServer()` 启动 HTTP 服务，提供 `/`（HTML页面）、`/api/data`（JSON查询）、`/api/export`（CSV/JSON导出）端点。
- **`templates/index.html`** — 单文件仪表盘。Chart.js 折线图 + 数据表格 + 筛选控件 + 导出按钮。

### 修改文件
- **`predictor.go`** — `WeatherPredictor` 新增 `store` 字段；`WeatherData` 新增 `AODNum` 字段；`buildMarkdownResponse()` 每次解析数据后调用 `store.UpsertRecord()`。
- **`main.go`** — 启动时初始化 store，传递给 predictor，启动 web server goroutine。
- **`go.mod`** — 新增 `modernc.org/sqlite`（纯 Go 实现，无 CGO）。
- **`Dockerfile`** — 添加 `EXPOSE 8080` 和 `COPY templates`。
- **`docker-compose.yaml`** — 添加端口映射和 volume 持久化 DB。
- **`README.md`** — 新增 `DB_PATH`、`WEB_PORT` 环境变量说明和数据看板文档。

### 数据流
```
Cron 触发 → FetchData() → fetchSingleData() → parseWeatherData()
                                                  ↓
                                          store.UpsertRecord() → SQLite
                                                  ↓
                                          sendNtfyNotification() → ntfy
```

### Database Schema
```sql
CREATE TABLE sunset_data (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    date TEXT NOT NULL,
    time TEXT NOT NULL,
    event_type TEXT NOT NULL,    -- 'morning' / 'evening'
    model TEXT NOT NULL,         -- 'GFS' / 'EC'
    quality REAL,
    aod REAL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(date, event_type, model)
);
```

## Usage

### 环境变量
| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DB_PATH` | `sunset.db` | SQLite 数据库路径 |
| `WEB_PORT` | `8080` | Web 看板端口 |

### Docker Compose
```yaml
ports:
  - "8080:8080"
volumes:
  - ./data:/app/data
environment:
  - DB_PATH=/app/data/sunset.db
  - WEB_PORT=8080
```

### API 端点
- `GET /` — 数据看板页面
- `GET /api/data?event_type=&start=&end=` — JSON 查询
- `GET /api/export?format=csv&event_type=&start=&end=` — 导出 CSV
- `GET /api/export?format=json&event_type=&start=&end=` — 导出 JSON

## Verification

- `go build ./...` — 编译通过
- `go vet ./...` — 无警告
- 冒烟测试：启动程序，HTML 页面正常返回，`/api/data` 返回空数组（符合预期，尚无数据）
- `modernc.org/sqlite` 纯 Go 实现，Docker 构建无需 CGO

## Journey Log

- [lesson] `modernc.org/sqlite` 通过 `proxy.golang.org` 下载超时，切换到 `goproxy.cn` 镜像后正常
