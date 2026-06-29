package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"time"
)

var (
	httpRequestsTotal int64
	httpRequestErrors int64
	cacheHits         int64
	cacheMisses       int64
	startTime         = time.Now()
	dateRegex         = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	validEventTypes   = map[string]bool{"": true, "morning": true, "evening": true}
)

func validateDate(date string) bool {
	if date == "" {
		return true
	}
	if !dateRegex.MatchString(date) {
		return false
	}
	_, err := time.Parse("2006-01-02", date)
	return err == nil
}

func validateEventType(eventType string) bool {
	return validEventTypes[eventType]
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func StartWebServer(port string, store *Store, logger *log.Logger) {
	cache := store.Cache
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		serveIndex(w, r)
	})

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"name": "流霞 - 朝霞晚霞数据看板",
			"short_name": "流霞",
			"description": "朝霞晚霞预测数据看板，支持多城市、多模型对比",
			"start_url": "/",
			"scope": "/",
			"id": "/",
			"display": "standalone",
			"background_color": "#f5f5f5",
			"theme_color": "#e67e22",
			"orientation": "any",
			"categories": ["weather", "utilities"],
			"icons": [
				{ "src": "/static/icons/icon-180x180.png", "sizes": "180x180", "type": "image/png" },
				{ "src": "/static/icons/icon-192x192.png", "sizes": "192x192", "type": "image/png" },
				{ "src": "/static/icons/icon-512x512.png", "sizes": "512x512", "type": "image/png" },
				{ "src": "/static/icons/icon-512x512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable" }
			]
		}`))
	})

	mux.HandleFunc("/service-worker.js", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Write([]byte(`const CACHE_VERSION = 'v3';
const STATIC_CACHE = 'liuxia-static-' + CACHE_VERSION;
const DATA_CACHE = 'liuxia-data-' + CACHE_VERSION;
const STATIC_ASSETS = ['/', '/manifest.json', '/static/icons/icon-180x180.png', '/static/icons/icon-192x192.png', '/static/icons/icon-512x512.png', '/offline.html'];
const CDN_ASSETS = ['https://cdn.jsdelivr.net/npm/chart.js@4'];

self.addEventListener('install', e => {
  e.waitUntil(
    caches.open(STATIC_CACHE).then(c => c.addAll([...STATIC_ASSETS, ...CDN_ASSETS])).then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', e => {
  e.waitUntil(
    caches.keys().then(keys => Promise.all(
      keys.filter(k => k !== STATIC_CACHE && k !== DATA_CACHE).map(k => caches.delete(k))
    )).then(() => self.clients.claim())
  );
});

self.addEventListener('message', e => {
  if (e.data && e.data.type === 'SKIP_WAITING') {
    self.skipWaiting();
  }
});

self.addEventListener('fetch', e => {
  const url = new URL(e.request.url);
  if (url.pathname.startsWith('/api/')) {
    e.respondWith(
      caches.open(DATA_CACHE).then(cache =>
        fetch(e.request).then(res => {
          cache.put(e.request, res.clone());
          return res;
        }).catch(() => cache.match(e.request))
      )
    );
  } else if (url.hostname === 'cdn.jsdelivr.net') {
    e.respondWith(
      caches.match(e.request).then(cached => {
        const fetchPromise = fetch(e.request).then(res => {
          if (res.ok) {
            const clone = res.clone();
            caches.open(STATIC_CACHE).then(c => c.put(e.request, clone));
          }
          return res;
        }).catch(() => cached);
        return cached || fetchPromise;
      })
    );
  } else {
    e.respondWith(
      caches.match(e.request).then(cached => {
        if (cached) return cached;
        return fetch(e.request).then(res => {
          if (res.ok) {
            const clone = res.clone();
            caches.open(STATIC_CACHE).then(c => c.put(e.request, clone));
          }
          return res;
        }).catch(() => {
          if (e.request.mode === 'navigate') {
            return caches.match('/offline.html');
          }
          return new Response('Offline', { status: 503 });
        });
      })
    );
  }
});

self.addEventListener('sync', e => {
  if (e.tag === 'sync-data') {
    e.waitUntil(syncPendingData());
  }
});

async function syncPendingData() {
  const cache = await caches.open(DATA_CACHE);
  const keys = await cache.keys();
  for (const req of keys) {
    try { await fetch(req); } catch (_) {}
  }
}`))
	})

	mux.HandleFunc("/offline.html", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<meta name="theme-color" content="#e67e22">
<title>离线 - 流霞</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif; background: #f5f5f5; color: #333; display: flex; justify-content: center; align-items: center; min-height: 100vh; }
  .offline-box { text-align: center; padding: 40px; }
  .offline-box svg { width: 80px; height: 80px; margin-bottom: 20px; fill: none; stroke: #e67e22; stroke-width: 1.5; }
  h1 { font-size: 20px; margin-bottom: 10px; color: #e67e22; }
  p { color: #666; margin-bottom: 20px; }
  button { padding: 10px 24px; border: none; border-radius: 20px; background: #e67e22; color: #fff; font-size: 14px; cursor: pointer; }
  button:hover { background: #d35400; }
</style>
</head>
<body>
<div class="offline-box">
  <svg viewBox="0 0 24 24"><path d="M1 1l22 22"/><path d="M16.72 11.06A10.94 10.94 0 0 1 19 12.55"/><path d="M5 12.55a10.94 10.94 0 0 1 5.17-2.39"/><path d="M10.71 5.05A16 16 0 0 1 22.56 9"/><path d="M1.42 9a15.91 15.91 0 0 1 4.7-2.88"/><path d="M8.53 16.11a6 6 0 0 1 6.95 0"/><line x1="12" y1="20" x2="12.01" y2="20"/></svg>
  <h1>当前处于离线状态</h1>
  <p>请检查网络连接后重试</p>
  <button onclick="location.reload()">重新加载</button>
</div>
</body>
</html>`))
	})

	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		atomic.AddInt64(&httpRequestsTotal, 1)
		city := r.URL.Query().Get("city")
		eventType := r.URL.Query().Get("event_type")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")

		if !validateEventType(eventType) {
			atomic.AddInt64(&httpRequestErrors, 1)
			http.Error(w, "invalid event_type", http.StatusBadRequest)
			return
		}
		if !validateDate(start) || !validateDate(end) {
			atomic.AddInt64(&httpRequestErrors, 1)
			http.Error(w, "invalid date format", http.StatusBadRequest)
			return
		}

		cacheKey := fmt.Sprintf("data:%s:%s:%s:%s", city, eventType, start, end)
		if cached, ok := cache.Get(cacheKey); ok {
			atomic.AddInt64(&cacheHits, 1)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			json.NewEncoder(w).Encode(cached)
			return
		}
		atomic.AddInt64(&cacheMisses, 1)

		records, err := store.QueryRecords(city, eventType, start, end)
		if err != nil {
			atomic.AddInt64(&httpRequestErrors, 1)
			logger.Printf("[Web] QueryRecords error: %v", err)
			http.Error(w, "internal server error", 500)
			return
		}
		if records == nil {
			records = []SunsetRecord{}
		}
		cache.Set(cacheKey, records)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "MISS")
		json.NewEncoder(w).Encode(records)
	})

	mux.HandleFunc("/api/export", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		format := r.URL.Query().Get("format")
		city := r.URL.Query().Get("city")
		eventType := r.URL.Query().Get("event_type")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")

		if !validateEventType(eventType) {
			http.Error(w, "invalid event_type", http.StatusBadRequest)
			return
		}
		if !validateDate(start) || !validateDate(end) {
			http.Error(w, "invalid date format", http.StatusBadRequest)
			return
		}

		if format == "csv" {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", "attachment; filename=liuxia_data.csv")
			store.ExportCSV(w, city, eventType, start, end)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Disposition", "attachment; filename=liuxia_data.json")
			store.ExportJSON(w, city, eventType, start, end)
		}
	})

	mux.HandleFunc("/api/statistics", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		atomic.AddInt64(&httpRequestsTotal, 1)
		city := r.URL.Query().Get("city")
		eventType := r.URL.Query().Get("event_type")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")

		if !validateEventType(eventType) {
			atomic.AddInt64(&httpRequestErrors, 1)
			http.Error(w, "invalid event_type", http.StatusBadRequest)
			return
		}
		if !validateDate(start) || !validateDate(end) {
			atomic.AddInt64(&httpRequestErrors, 1)
			http.Error(w, "invalid date format", http.StatusBadRequest)
			return
		}

		cacheKey := fmt.Sprintf("stats:%s:%s:%s:%s", city, eventType, start, end)
		if cached, ok := cache.Get(cacheKey); ok {
			atomic.AddInt64(&cacheHits, 1)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			json.NewEncoder(w).Encode(cached)
			return
		}
		atomic.AddInt64(&cacheMisses, 1)

		stats, err := store.GetStatistics(city, eventType, start, end)
		if err != nil {
			atomic.AddInt64(&httpRequestErrors, 1)
			logger.Printf("[Web] GetStatistics error: %v", err)
			http.Error(w, "internal server error", 500)
			return
		}
		cache.Set(cacheKey, stats)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "MISS")
		json.NewEncoder(w).Encode(stats)
	})

	mux.HandleFunc("/api/cities", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		atomic.AddInt64(&httpRequestsTotal, 1)
		cacheKey := "cities"
		if cached, ok := cache.Get(cacheKey); ok {
			atomic.AddInt64(&cacheHits, 1)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			json.NewEncoder(w).Encode(cached)
			return
		}
		atomic.AddInt64(&cacheMisses, 1)

		cities, err := store.GetCities()
		if err != nil {
			atomic.AddInt64(&httpRequestErrors, 1)
			logger.Printf("[Web] GetCities error: %v", err)
			http.Error(w, "internal server error", 500)
			return
		}
		cache.Set(cacheKey, cities)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "MISS")
		json.NewEncoder(w).Encode(cities)
	})

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		totalRecords, _ := store.GetTotalRecords()
		cities, _ := store.GetCities()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "ok",
			"totalRecords": totalRecords,
			"cities":       cities,
		})
	})

	mux.HandleFunc("/api/city-comparison", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		atomic.AddInt64(&httpRequestsTotal, 1)
		eventType := r.URL.Query().Get("event_type")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")

		if !validateEventType(eventType) {
			atomic.AddInt64(&httpRequestErrors, 1)
			http.Error(w, "invalid event_type", http.StatusBadRequest)
			return
		}
		if !validateDate(start) || !validateDate(end) {
			atomic.AddInt64(&httpRequestErrors, 1)
			http.Error(w, "invalid date format", http.StatusBadRequest)
			return
		}

		cacheKey := fmt.Sprintf("citycomp:%s:%s:%s", eventType, start, end)
		if cached, ok := cache.Get(cacheKey); ok {
			atomic.AddInt64(&cacheHits, 1)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			json.NewEncoder(w).Encode(cached)
			return
		}
		atomic.AddInt64(&cacheMisses, 1)

		comparison, err := store.GetCityComparison(eventType, start, end)
		if err != nil {
			atomic.AddInt64(&httpRequestErrors, 1)
			logger.Printf("[Web] GetCityComparison error: %v", err)
			http.Error(w, "internal server error", 500)
			return
		}
		cache.Set(cacheKey, comparison)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "MISS")
		json.NewEncoder(w).Encode(comparison)
	})

	mux.HandleFunc("/api/rankings", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		atomic.AddInt64(&httpRequestsTotal, 1)
		city := r.URL.Query().Get("city")
		eventType := r.URL.Query().Get("event_type")

		if !validateEventType(eventType) {
			atomic.AddInt64(&httpRequestErrors, 1)
			http.Error(w, "invalid event_type", http.StatusBadRequest)
			return
		}

		cacheKey := fmt.Sprintf("rankings:%s:%s", city, eventType)
		if cached, ok := cache.Get(cacheKey); ok {
			atomic.AddInt64(&cacheHits, 1)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			json.NewEncoder(w).Encode(cached)
			return
		}
		atomic.AddInt64(&cacheMisses, 1)

		rankings, err := store.GetRankings(city, eventType, 10)
		if err != nil {
			atomic.AddInt64(&httpRequestErrors, 1)
			logger.Printf("[Web] GetRankings error: %v", err)
			http.Error(w, "internal server error", 500)
			return
		}
		cache.Set(cacheKey, rankings)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "MISS")
		json.NewEncoder(w).Encode(rankings)
	})

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		totalRecords, err := store.GetTotalRecords()
		if err != nil {
			logger.Printf("[Web] GetTotalRecords error: %v", err)
			totalRecords = 0
		}
		cities, err := store.GetCities()
		if err != nil {
			logger.Printf("[Web] GetCities error for metrics: %v", err)
			cities = nil
		}

		uptime := time.Since(startTime).Seconds()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "# HELP liuxia_http_requests_total Total HTTP requests\n")
		fmt.Fprintf(w, "# TYPE liuxia_http_requests_total counter\n")
		fmt.Fprintf(w, "liuxia_http_requests_total %d\n", atomic.LoadInt64(&httpRequestsTotal))

		fmt.Fprintf(w, "# HELP liuxia_http_request_errors Total HTTP request errors\n")
		fmt.Fprintf(w, "# TYPE liuxia_http_request_errors counter\n")
		fmt.Fprintf(w, "liuxia_http_request_errors %d\n", atomic.LoadInt64(&httpRequestErrors))

		fmt.Fprintf(w, "# HELP liuxia_cache_hits Total cache hits\n")
		fmt.Fprintf(w, "# TYPE liuxia_cache_hits counter\n")
		fmt.Fprintf(w, "liuxia_cache_hits %d\n", atomic.LoadInt64(&cacheHits))

		fmt.Fprintf(w, "# HELP liuxia_cache_misses Total cache misses\n")
		fmt.Fprintf(w, "# TYPE liuxia_cache_misses counter\n")
		fmt.Fprintf(w, "liuxia_cache_misses %d\n", atomic.LoadInt64(&cacheMisses))

		fmt.Fprintf(w, "# HELP liuxia_total_records Total records in database\n")
		fmt.Fprintf(w, "# TYPE liuxia_total_records gauge\n")
		fmt.Fprintf(w, "liuxia_total_records %d\n", totalRecords)

		fmt.Fprintf(w, "# HELP liuxia_total_cities Total cities tracked\n")
		fmt.Fprintf(w, "# TYPE liuxia_total_cities gauge\n")
		fmt.Fprintf(w, "liuxia_total_cities %d\n", len(cities))

		fmt.Fprintf(w, "# HELP liuxia_uptime_seconds Uptime in seconds\n")
		fmt.Fprintf(w, "# TYPE liuxia_uptime_seconds gauge\n")
		fmt.Fprintf(w, "liuxia_uptime_seconds %.0f\n", uptime)
	})

	addr := ":" + port
	logger.Printf("[Web] 服务启动于 http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Printf("[Web] 服务异常: %v", err)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	path := filepath.Join(dir, "templates", "index.html")

	data, err := os.ReadFile(path)
	if err != nil {
		data, err = os.ReadFile("templates/index.html")
		if err != nil {
			http.Error(w, "page not found", 500)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(data)
}
