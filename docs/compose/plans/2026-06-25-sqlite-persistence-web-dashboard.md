# SQLite 持久化 + Web Dashboard Implementation Plan

> [!NOTE]
> This document may not reflect the current implementation.
> See the final report for up-to-date state:
> [Final Report](../reports/sqlite-persistence-web-dashboard.md)

> **For agentic workers:** REQUIRED SUB-SKILL: Use compose:subagent (recommended) or compose:execute to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist sunrise/sunset weather data (quality, AOD, time) to SQLite with upsert, and serve a web dashboard with line charts and data export.

**Architecture:** Add a SQLite storage layer (`store.go`) and HTTP web server (`web.go`) to the existing Go cron bot. The predictor saves data after each fetch. A single HTML page with Chart.js renders line charts (quality + AOD, grouped by model) and a data table with export buttons.

**Tech Stack:** Go, `modernc.org/sqlite` (pure Go, no CGO), `net/http`, Chart.js (CDN), `encoding/csv`

## Global Constraints

- Pure Go SQLite driver (`modernc.org/sqlite`) — no CGO required for Docker builds
- No authentication on web endpoints (internal/localhost use only)
- Environment variables for config: `DB_PATH` (default `sunset.db`), `WEB_PORT` (default `8080`)
- Follow existing code conventions: Chinese log messages, same package `main`
- Minimal dependencies — only add what's needed

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `go.mod` | Modify | Add `modernc.org/sqlite` dependency |
| `store.go` | Create | SQLite init, upsert, query, export |
| `web.go` | Create | HTTP server, API endpoints, serves HTML |
| `templates/index.html` | Create | Dashboard: table + Chart.js line charts |
| `predictor.go` | Modify | Call store after data parse; add `AOD` field to `WeatherData` |
| `main.go` | Modify | Init store, start web server, new env vars |
| `Dockerfile` | Modify | Add `EXPOSE 8080` |
| `docker-compose.yaml` | Modify | Add ports + volume for DB persistence |

---

### Task 1: SQLite Storage Layer

**Covers:** Data persistence with upsert

**Files:**
- Create: `store.go`
- Modify: `go.mod` (add dependency)

**Interfaces:**
- Produces: `Store` type with `InitStore()`, `UpsertRecord()`, `QueryRecords()`, `ExportCSV()`, `ExportJSON()`

- [ ] **Step 1: Add SQLite dependency**

```bash
go get modernc.org/sqlite
```

- [ ] **Step 2: Create `store.go`**

```go
package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type SunsetRecord struct {
	ID        int64  `json:"id"`
	Date      string `json:"date"`
	Time      string `json:"time"`
	EventType string `json:"event_type"`
	Model     string `json:"model"`
	Quality   *float64 `json:"quality"`
	AOD       *float64 `json:"aod"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func InitStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS sunset_data (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		date TEXT NOT NULL,
		time TEXT NOT NULL,
		event_type TEXT NOT NULL,
		model TEXT NOT NULL,
		quality REAL,
		aod REAL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(date, event_type, model)
	);`

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create table: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) UpsertRecord(r SunsetRecord) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := s.db.Exec(`
		INSERT INTO sunset_data (date, time, event_type, model, quality, aod, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(date, event_type, model) DO UPDATE SET
			time = excluded.time,
			quality = excluded.quality,
			aod = excluded.aod,
			updated_at = excluded.updated_at`,
		r.Date, r.Time, r.EventType, r.Model, r.Quality, r.AOD, now, now)
	return err
}

func (s *Store) QueryRecords(eventType, startDate, endDate string) ([]SunsetRecord, error) {
	query := `SELECT id, date, time, event_type, model, quality, aod, created_at, updated_at
		FROM sunset_data WHERE 1=1`
	args := []interface{}{}

	if eventType != "" {
		query += ` AND event_type = ?`
		args = append(args, eventType)
	}
	if startDate != "" {
		query += ` AND date >= ?`
		args = append(args, startDate)
	}
	if endDate != "" {
		query += ` AND date <= ?`
		args = append(args, endDate)
	}
	query += ` ORDER BY date ASC, event_type ASC, model ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []SunsetRecord
	for rows.Next() {
		var r SunsetRecord
		if err := rows.Scan(&r.ID, &r.Date, &r.Time, &r.EventType, &r.Model,
			&r.Quality, &r.AOD, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *Store) ExportCSV(w io.Writer, eventType, startDate, endDate string) error {
	records, err := s.QueryRecords(eventType, startDate, endDate)
	if err != nil {
		return err
	}

	writer := csv.NewWriter(w)
	defer writer.Flush()

	writer.Write([]string{"date", "time", "event_type", "model", "quality", "aod", "created_at", "updated_at"})
	for _, r := range records {
		qStr := ""
		if r.Quality != nil {
			qStr = fmt.Sprintf("%.4f", *r.Quality)
		}
		aStr := ""
		if r.AOD != nil {
			aStr = fmt.Sprintf("%.4f", *r.AOD)
		}
		writer.Write([]string{r.Date, r.Time, r.EventType, r.Model, qStr, aStr, r.CreatedAt, r.UpdatedAt})
	}
	return nil
}

func (s *Store) ExportJSON(w io.Writer, eventType, startDate, endDate string) error {
	records, err := s.QueryRecords(eventType, startDate, endDate)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(records)
}

func (s *Store) Close() error {
	return s.db.Close()
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add store.go go.mod go.sum
git commit -m "feat: add SQLite storage layer with upsert and export"
```

---

### Task 2: Modify Predictor to Persist Data

**Covers:** Every fetch writes to DB

**Files:**
- Modify: `predictor.go:33-38` (add `AOD` field to `WeatherData`)
- Modify: `predictor.go:140-146` (set `AOD` in return)
- Modify: `predictor.go:264-269` (add `aod` to `dateEntry`)
- Modify: `predictor.go:344-349` (store `aod` in `dateEntry`)
- Modify: `predictor.go:317-387` (`buildMarkdownResponse` → also upsert)
- Create: `store.go` (already done in Task 1)

**Interfaces:**
- Consumes: `Store.UpsertRecord()` from Task 1
- Produces: Modified `FetchData` persists every parsed record

- [ ] **Step 1: Add `AOD` field to `WeatherData` and populate it**

In `predictor.go`, add `AOD` to the struct and set it:

```go
// After line 33, WeatherData struct becomes:
type WeatherData struct {
	PushStr    string
	QualityNum float64
	AODNum     float64  // new
	AODStr     string   // new: raw string like "0.25" or "N/A"
	DateStr    string
	TimeStr    string
}
```

In `parseWeatherData`, after line 116 where `aodNum` is parsed, add:

```go
	var aodStrVal string
	if aodNum != nil {
		aodStrVal = fmt.Sprintf("%.4f", *aodNum)
	} else {
		aodStrVal = "N/A"
	}
```

And in the return (line 140), add:

```go
	return &WeatherData{
		PushStr:    pushStr.String(),
		QualityNum: qualityNum,
		AODNum:     derefFloat(aodNum),
		AODStr:     aodStrVal,
		DateStr:    dateStr,
		TimeStr:    timeStr,
	}
```

Add helper at top of file:

```go
func derefFloat(f *float64) float64 {
	if f != nil {
		return *f
	}
	return 0
}
```

- [ ] **Step 2: Add `aod` to `dateEntry`**

```go
type dateEntry struct {
	model      string
	pushStr    string
	qualityNum float64
	aodNum     float64  // new
	timeStr    string
}
```

- [ ] **Step 3: In `buildMarkdownResponse`, pass `aodNum` and upsert**

After fetching data (around line 344), add upsert call and store aod:

```go
		wp.store.UpsertRecord(SunsetRecord{
			Date:      result.DateStr,
			Time:      result.TimeStr,
			EventType: eventType,  // need to pass this in
			Model:     model,
			Quality:   &result.QualityNum,
			AOD:       floatPtr(result.AODNum),
		})
```

Add helper:
```go
func floatPtr(f float64) *float64 {
	if f == 0 {
		return nil
	}
	return &f
}
```

Modify `buildMarkdownResponse` signature to accept `eventType string` and `store *Store`. Add `store` field to `WeatherPredictor` struct:

```go
type WeatherPredictor struct {
	config  *Config
	client  *http.Client
	logger  *log.Logger
	store   *Store  // new
}
```

Update `NewWeatherPredictor` to accept and set `store`.

- [ ] **Step 4: Verify compilation**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add predictor.go
git commit -m "feat: persist weather data to SQLite on every fetch"
```

---

### Task 3: Web Server + API Endpoints

**Covers:** Web page serving, JSON API, CSV/JSON export

**Files:**
- Create: `web.go`
- Modify: `main.go`

**Interfaces:**
- Consumes: `Store` from Task 1
- Produces: HTTP server on configurable port

- [ ] **Step 1: Create `web.go`**

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func StartWebServer(port string, store *Store, logger *log.Logger) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		serveIndex(w, r)
	})

	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		eventType := r.URL.Query().Get("event_type")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")

		records, err := store.QueryRecords(eventType, start, end)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.Encode(records)
	})

	mux.HandleFunc("/api/export", func(w http.ResponseWriter, r *http.Request) {
		format := r.URL.Query().Get("format")
		eventType := r.URL.Query().Get("event_type")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")

		if format == "csv" {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", "attachment; filename=sunset_data.csv")
			store.ExportCSV(w, eventType, start, end)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Disposition", "attachment; filename=sunset_data.json")
			store.ExportJSON(w, eventType, start, end)
		}
	})

	addr := ":" + port
	logger.Printf("[Web] 服务启动于 http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Printf("[Web] 服务异常: %v", err)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	path := filepath.Join(dir, "templates", "index.html")

	data, err := os.ReadFile(path)
	if err != nil {
		// fallback: try relative to working dir
		data, err = os.ReadFile("templates/index.html")
		if err != nil {
			http.Error(w, fmt.Sprintf("page not found: %v", err), 500)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
```

- [ ] **Step 2: Modify `main.go` to init store and start web server**

Add to imports: `"os"`

After `LoadConfig`, add:

```go
	dbPath := getEnv("DB_PATH", "sunset.db")
	store, err := InitStore(dbPath)
	if err != nil {
		logger.Fatalf("数据库初始化失败: %v", err)
	}
	defer store.Close()
	logger.Printf("[启动] 数据库已初始化: %s", dbPath)
```

Update `NewWeatherPredictor` call to pass `store`:

```go
	predictor := NewWeatherPredictor(cfg, logger, store)
```

After cron start, start web server in goroutine:

```go
	webPort := getEnv("WEB_PORT", "8080")
	go StartWebServer(webPort, store, logger)
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add web.go main.go
git commit -m "feat: add web server with data API and export endpoints"
```

---

### Task 4: Dashboard HTML Page

**Covers:** Line charts (quality + AOD, grouped by model), data table, export buttons, filtering

**Files:**
- Create: `templates/index.html`

**Interfaces:**
- Consumes: `/api/data` and `/api/export` from Task 3

- [ ] **Step 1: Create `templates/index.html`**

```html
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>朝霞晚霞数据看板</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4"></script>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif; background: #f5f5f5; color: #333; padding: 20px; }
  .container { max-width: 1200px; margin: 0 auto; }
  h1 { text-align: center; margin-bottom: 20px; color: #e67e22; }
  .controls { display: flex; gap: 10px; margin-bottom: 20px; flex-wrap: wrap; align-items: center; }
  .controls select, .controls input { padding: 6px 10px; border: 1px solid #ddd; border-radius: 4px; }
  .controls button { padding: 6px 14px; border: none; border-radius: 4px; background: #e67e22; color: #fff; cursor: pointer; }
  .controls button:hover { background: #d35400; }
  .chart-box { background: #fff; border-radius: 8px; padding: 20px; margin-bottom: 20px; box-shadow: 0 1px 3px rgba(0,0,0,.1); }
  .chart-box h2 { font-size: 16px; margin-bottom: 10px; color: #555; }
  table { width: 100%; border-collapse: collapse; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,.1); }
  th, td { padding: 10px 12px; text-align: left; border-bottom: 1px solid #eee; font-size: 14px; }
  th { background: #e67e22; color: #fff; }
  tr:hover { background: #fdf2e9; }
  .empty { text-align: center; padding: 40px; color: #999; }
</style>
</head>
<body>
<div class="container">
  <h1>朝霞晚霞数据看板</h1>

  <div class="controls">
    <select id="eventType">
      <option value="">全部</option>
      <option value="morning">朝霞</option>
      <option value="evening">晚霞</option>
    </select>
    <input type="date" id="startDate" placeholder="开始日期">
    <input type="date" id="endDate" placeholder="结束日期">
    <button onclick="loadData()">查询</button>
    <button onclick="exportData('csv')">导出 CSV</button>
    <button onclick="exportData('json')">导出 JSON</button>
  </div>

  <div class="chart-box">
    <h2>鲜艳度趋势</h2>
    <canvas id="qualityChart"></canvas>
  </div>

  <div class="chart-box">
    <h2>气溶胶趋势</h2>
    <canvas id="aodChart"></canvas>
  </div>

  <table>
    <thead>
      <tr>
        <th>日期</th><th>时间</th><th>类型</th><th>模型</th><th>鲜艳度</th><th>气溶胶</th>
      </tr>
    </thead>
    <tbody id="tableBody"></tbody>
  </table>
  <div id="emptyMsg" class="empty" style="display:none;">暂无数据</div>
</div>

<script>
let qualityChart, aodChart;
const colors = { GFS: '#3498db', EC: '#e74c3c' };

function getFilters() {
  return {
    event_type: document.getElementById('eventType').value,
    start: document.getElementById('startDate').value,
    end: document.getElementById('endDate').value,
  };
}

function qs(params) {
  const s = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) { if (v) s.set(k, v); }
  return s.toString();
}

async function loadData() {
  const params = getFilters();
  const res = await fetch('/api/data?' + qs(params));
  const records = await res.json();
  renderTable(records);
  renderCharts(records);
}

function renderTable(records) {
  const tbody = document.getElementById('tableBody');
  const empty = document.getElementById('emptyMsg');
  if (!records || records.length === 0) {
    tbody.innerHTML = '';
    empty.style.display = 'block';
    return;
  }
  empty.style.display = 'none';
  const typeMap = { morning: '朝霞', evening: '晚霞' };
  tbody.innerHTML = records.map(r => `<tr>
    <td>${r.date}</td><td>${r.time}</td>
    <td>${typeMap[r.event_type] || r.event_type}</td><td>${r.model}</td>
    <td>${r.quality != null ? r.quality.toFixed(4) : '-'}</td>
    <td>${r.aod != null ? r.aod.toFixed(4) : '-'}</td>
  </tr>`).join('');
}

function renderCharts(records) {
  const grouped = {};
  records.forEach(r => {
    const key = r.model;
    if (!grouped[key]) grouped[key] = [];
    grouped[key].push(r);
  });

  const labels = [...new Set(records.map(r => r.date))].sort();

  const qualityDatasets = Object.entries(grouped).map(([model, items]) => ({
    label: model,
    data: labels.map(d => { const rec = items.find(i => i.date === d); return rec ? rec.quality : null; }),
    borderColor: colors[model] || '#999',
    tension: 0.3,
    fill: false,
  }));

  const aodDatasets = Object.entries(grouped).map(([model, items]) => ({
    label: model,
    data: labels.map(d => { const rec = items.find(i => i.date === d); return rec ? rec.aod : null; }),
    borderColor: colors[model] || '#999',
    tension: 0.3,
    fill: false,
  }));

  if (qualityChart) qualityChart.destroy();
  if (aodChart) aodChart.destroy();

  qualityChart = new Chart(document.getElementById('qualityChart'), {
    type: 'line', data: { labels, datasets: qualityDatasets },
    options: { responsive: true, plugins: { legend: { position: 'top' } }, scales: { y: { beginAtZero: true } } }
  });

  aodChart = new Chart(document.getElementById('aodChart'), {
    type: 'line', data: { labels, datasets: aodDatasets },
    options: { responsive: true, plugins: { legend: { position: 'top' } }, scales: { y: { beginAtZero: true } } }
  });
}

function exportData(format) {
  const params = getFilters();
  params.format = format;
  window.location.href = '/api/export?' + qs(params);
}

loadData();
</script>
</body>
</html>
```

- [ ] **Step 2: Verify the page loads**

Start the program, visit `http://localhost:8080`, confirm charts render and table shows.

- [ ] **Step 3: Commit**

```bash
git add templates/index.html
git commit -m "feat: add dashboard HTML with Chart.js line charts and data table"
```

---

### Task 5: Docker + Config Updates

**Covers:** Dockerfile EXPOSE, docker-compose ports and volume, env vars

**Files:**
- Modify: `Dockerfile`
- Modify: `docker-compose.yaml`
- Modify: `README.md`

- [ ] **Step 1: Update Dockerfile**

Add before `CMD`:

```dockerfile
EXPOSE 8080
```

Add `COPY templates` step:

```dockerfile
COPY templates /app/templates
```

- [ ] **Step 2: Update docker-compose.yaml**

```yaml
services:
  sunsetbot:
    container_name: sunsetbot
    image: ghcr.io/felix2yu/sunsetbot:latest
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data
    environment:
      - CITY=江苏省-苏州
      - BASE_URL=https://sunsetbot.top/
      - PUSH_ENABLE=true
      - NTFY_SERVER=https://ntfy.sh
      - NTFY_TOPIC=Weather
      - NTFY_TOKEN=
      - SEND_TEST_ON_START=false
      - PUSH_ERROR=true
      - MORNING_ENABLE=true
      - MORNING_TIME=18:00,00:00
      - MORNING_MODEL=GFS,EC
      - EVENING_ENABLE=true
      - EVENING_TIME=08:00,11:30,16:00
      - EVENING_MODEL=GFS,EC
      - DB_PATH=/app/data/sunset.db
      - WEB_PORT=8080
    restart: unless-stopped
```

- [ ] **Step 3: Add new env vars to README.md table**

Add rows for `DB_PATH` and `WEB_PORT`.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile docker-compose.yaml README.md
git commit -m "feat: add Docker port exposure and DB volume persistence"
```

---

### Task 6: Final Verification

**Covers:** End-to-end compilation and functionality check

- [ ] **Step 1: Full build**

```bash
go build ./...
```

- [ ] **Step 2: Vet check**

```bash
go vet ./...
```

- [ ] **Step 3: Run and test manually**

```bash
CITY=test NTFY_TOPIC=test go run . &
# Wait a few seconds, then:
curl http://localhost:8080
curl http://localhost:8080/api/data
# Kill the process
```

- [ ] **Step 4: Final commit if needed**
