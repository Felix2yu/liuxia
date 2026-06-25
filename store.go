package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type SunsetRecord struct {
	ID        int64    `json:"id"`
	Date      string   `json:"date"`
	Time      string   `json:"time"`
	EventType string   `json:"event_type"`
	Model     string   `json:"model"`
	Quality   *float64 `json:"quality"`
	AOD       *float64 `json:"aod"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
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
