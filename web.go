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
	cache := NewCache(5 * time.Minute)
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

	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"name": "朝霞晚霞数据看板",
			"short_name": "SunsetBot",
			"start_url": "/",
			"display": "standalone",
			"background_color": "#f5f5f5",
			"theme_color": "#e67e22"
		}`))
	})

	mux.HandleFunc("/service-worker.js", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(`self.addEventListener('fetch', e => e.respondWith(fetch(e.request)));`))
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
			w.Header().Set("Content-Disposition", "attachment; filename=sunset_data.csv")
			store.ExportCSV(w, city, eventType, start, end)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Disposition", "attachment; filename=sunset_data.json")
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
		fmt.Fprintf(w, "# HELP sunsetbot_http_requests_total Total HTTP requests\n")
		fmt.Fprintf(w, "# TYPE sunsetbot_http_requests_total counter\n")
		fmt.Fprintf(w, "sunsetbot_http_requests_total %d\n", atomic.LoadInt64(&httpRequestsTotal))

		fmt.Fprintf(w, "# HELP sunsetbot_http_request_errors Total HTTP request errors\n")
		fmt.Fprintf(w, "# TYPE sunsetbot_http_request_errors counter\n")
		fmt.Fprintf(w, "sunsetbot_http_request_errors %d\n", atomic.LoadInt64(&httpRequestErrors))

		fmt.Fprintf(w, "# HELP sunsetbot_cache_hits Total cache hits\n")
		fmt.Fprintf(w, "# TYPE sunsetbot_cache_hits counter\n")
		fmt.Fprintf(w, "sunsetbot_cache_hits %d\n", atomic.LoadInt64(&cacheHits))

		fmt.Fprintf(w, "# HELP sunsetbot_cache_misses Total cache misses\n")
		fmt.Fprintf(w, "# TYPE sunsetbot_cache_misses counter\n")
		fmt.Fprintf(w, "sunsetbot_cache_misses %d\n", atomic.LoadInt64(&cacheMisses))

		fmt.Fprintf(w, "# HELP sunsetbot_total_records Total records in database\n")
		fmt.Fprintf(w, "# TYPE sunsetbot_total_records gauge\n")
		fmt.Fprintf(w, "sunsetbot_total_records %d\n", totalRecords)

		fmt.Fprintf(w, "# HELP sunsetbot_total_cities Total cities tracked\n")
		fmt.Fprintf(w, "# TYPE sunsetbot_total_cities gauge\n")
		fmt.Fprintf(w, "sunsetbot_total_cities %d\n", len(cities))

		fmt.Fprintf(w, "# HELP sunsetbot_uptime_seconds Uptime in seconds\n")
		fmt.Fprintf(w, "# TYPE sunsetbot_uptime_seconds gauge\n")
		fmt.Fprintf(w, "sunsetbot_uptime_seconds %.0f\n", uptime)
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
	w.Write(data)
}
