package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func StartWebServer(port string, store *Store, logger *log.Logger) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
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
		json.NewEncoder(w).Encode(records)
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
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	path := filepath.Join(dir, "templates", "index.html")

	data, err := os.ReadFile(path)
	if err != nil {
		data, err = os.ReadFile("templates/index.html")
		if err != nil {
			http.Error(w, fmt.Sprintf("page not found: %v", err), 500)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
