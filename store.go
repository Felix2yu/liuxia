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
	City      string   `json:"city"`
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
		city TEXT NOT NULL DEFAULT '',
		date TEXT NOT NULL,
		time TEXT NOT NULL,
		event_type TEXT NOT NULL,
		model TEXT NOT NULL,
		quality REAL,
		aod REAL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(city, date, event_type, model)
	);`

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create table: %w", err)
	}

	var cityExists int
	err = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sunset_data') WHERE name='city'`).Scan(&cityExists)
	if err != nil {
		return nil, fmt.Errorf("check city column: %w", err)
	}
	if cityExists == 0 {
		if _, err := db.Exec(`ALTER TABLE sunset_data ADD COLUMN city TEXT NOT NULL DEFAULT ''`); err != nil {
			return nil, fmt.Errorf("add city column: %w", err)
		}
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_city ON sunset_data(city)`,
		`CREATE INDEX IF NOT EXISTS idx_date ON sunset_data(date)`,
		`CREATE INDEX IF NOT EXISTS idx_event_type ON sunset_data(event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_city_date ON sunset_data(city, date)`,
		`CREATE INDEX IF NOT EXISTS idx_date_event ON sunset_data(date, event_type)`,
	}
	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			return nil, fmt.Errorf("create index: %w", err)
		}
	}

	return &Store{db: db}, nil
}

func (s *Store) UpsertRecord(r SunsetRecord) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := s.db.Exec(`
		INSERT INTO sunset_data (city, date, time, event_type, model, quality, aod, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(city, date, event_type, model) DO UPDATE SET
			time = excluded.time,
			quality = excluded.quality,
			aod = excluded.aod,
			updated_at = excluded.updated_at`,
		r.City, r.Date, r.Time, r.EventType, r.Model, r.Quality, r.AOD, now, now)
	return err
}

func (s *Store) QueryRecords(city, eventType, startDate, endDate string) ([]SunsetRecord, error) {
	query := `SELECT id, city, date, time, event_type, model, quality, aod, created_at, updated_at
		FROM sunset_data WHERE 1=1`
	args := []interface{}{}

	if city != "" {
		query += ` AND city = ?`
		args = append(args, city)
	}
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
	query += ` ORDER BY city ASC, date ASC, event_type ASC, model ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []SunsetRecord
	for rows.Next() {
		var r SunsetRecord
		if err := rows.Scan(&r.ID, &r.City, &r.Date, &r.Time, &r.EventType, &r.Model,
			&r.Quality, &r.AOD, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *Store) ExportCSV(w io.Writer, city, eventType, startDate, endDate string) error {
	records, err := s.QueryRecords(city, eventType, startDate, endDate)
	if err != nil {
		return err
	}

	writer := csv.NewWriter(w)
	defer writer.Flush()

	writer.Write([]string{"city", "date", "time", "event_type", "model", "quality", "aod", "created_at", "updated_at"})
	for _, r := range records {
		qStr := ""
		if r.Quality != nil {
			qStr = fmt.Sprintf("%.4f", *r.Quality)
		}
		aStr := ""
		if r.AOD != nil {
			aStr = fmt.Sprintf("%.4f", *r.AOD)
		}
		writer.Write([]string{r.City, r.Date, r.Time, r.EventType, r.Model, qStr, aStr, r.CreatedAt, r.UpdatedAt})
	}
	return nil
}

func (s *Store) ExportJSON(w io.Writer, city, eventType, startDate, endDate string) error {
	records, err := s.QueryRecords(city, eventType, startDate, endDate)
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

type ModelStats struct {
	Model        string   `json:"model"`
	EventType    string   `json:"event_type"`
	Count        int      `json:"count"`
	MinQuality   *float64 `json:"min_quality"`
	MaxQuality   *float64 `json:"max_quality"`
	AvgQuality   *float64 `json:"avg_quality"`
	MinAOD       *float64 `json:"min_aod"`
	MaxAOD       *float64 `json:"max_aod"`
	AvgAOD       *float64 `json:"avg_aod"`
}

type MonthlyStats struct {
	Month      string   `json:"month"`
	Count      int      `json:"count"`
	AvgQuality *float64 `json:"avg_quality"`
	AvgAOD     *float64 `json:"avg_aod"`
}

type Statistics struct {
	TotalRecords int           `json:"total_records"`
	Models       []ModelStats  `json:"models"`
	Monthly      []MonthlyStats `json:"monthly"`
}

func (s *Store) GetStatistics(city, eventType, startDate, endDate string) (*Statistics, error) {
	stats := &Statistics{}

	countQuery := `SELECT COUNT(*) FROM sunset_data WHERE 1=1`
	args := []interface{}{}

	if city != "" {
		countQuery += ` AND city = ?`
		args = append(args, city)
	}
	if eventType != "" {
		countQuery += ` AND event_type = ?`
		args = append(args, eventType)
	}
	if startDate != "" {
		countQuery += ` AND date >= ?`
		args = append(args, startDate)
	}
	if endDate != "" {
		countQuery += ` AND date <= ?`
		args = append(args, endDate)
	}

	if err := s.db.QueryRow(countQuery, args...).Scan(&stats.TotalRecords); err != nil {
		return nil, err
	}

	modelQuery := `SELECT model, event_type, COUNT(*),
		MIN(quality), MAX(quality), AVG(quality),
		MIN(aod), MAX(aod), AVG(aod)
		FROM sunset_data WHERE 1=1`

	modelArgs := []interface{}{}

	if city != "" {
		modelQuery += ` AND city = ?`
		modelArgs = append(modelArgs, city)
	}
	if eventType != "" {
		modelQuery += ` AND event_type = ?`
		modelArgs = append(modelArgs, eventType)
	}
	if startDate != "" {
		modelQuery += ` AND date >= ?`
		modelArgs = append(modelArgs, startDate)
	}
	if endDate != "" {
		modelQuery += ` AND date <= ?`
		modelArgs = append(modelArgs, endDate)
	}
	modelQuery += ` GROUP BY model, event_type ORDER BY model, event_type`

	rows, err := s.db.Query(modelQuery, modelArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var m ModelStats
		if err := rows.Scan(&m.Model, &m.EventType, &m.Count,
			&m.MinQuality, &m.MaxQuality, &m.AvgQuality,
			&m.MinAOD, &m.MaxAOD, &m.AvgAOD); err != nil {
			return nil, err
		}
		stats.Models = append(stats.Models, m)
	}

	monthlyQuery := `SELECT substr(date, 1, 7) as month, COUNT(*), AVG(quality), AVG(aod)
		FROM sunset_data WHERE 1=1`

	monthlyArgs := []interface{}{}

	if city != "" {
		monthlyQuery += ` AND city = ?`
		monthlyArgs = append(monthlyArgs, city)
	}
	if eventType != "" {
		monthlyQuery += ` AND event_type = ?`
		monthlyArgs = append(monthlyArgs, eventType)
	}
	if startDate != "" {
		monthlyQuery += ` AND date >= ?`
		monthlyArgs = append(monthlyArgs, startDate)
	}
	if endDate != "" {
		monthlyQuery += ` AND date <= ?`
		monthlyArgs = append(monthlyArgs, endDate)
	}
	monthlyQuery += ` GROUP BY month ORDER BY month`

	mRows, err := s.db.Query(monthlyQuery, monthlyArgs...)
	if err != nil {
		return nil, err
	}
	defer mRows.Close()

	for mRows.Next() {
		var m MonthlyStats
		if err := mRows.Scan(&m.Month, &m.Count, &m.AvgQuality, &m.AvgAOD); err != nil {
			return nil, err
		}
		stats.Monthly = append(stats.Monthly, m)
	}

	return stats, nil
}

func (s *Store) GetCities() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT city FROM sunset_data WHERE city != '' ORDER BY city`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cities []string
	for rows.Next() {
		var city string
		if err := rows.Scan(&city); err != nil {
			return nil, err
		}
		cities = append(cities, city)
	}
	return cities, rows.Err()
}

func (s *Store) GetTotalRecords() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sunset_data`).Scan(&count)
	return count, err
}

func (s *Store) DeleteOldRecords(daysToKeep int) (int64, error) {
	result, err := s.db.Exec(`
		DELETE FROM sunset_data 
		WHERE date < date('now', '-' || ? || ' days')`,
		daysToKeep)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type CityComparison struct {
	City       string   `json:"city"`
	AvgQuality *float64 `json:"avg_quality"`
	AvgAOD     *float64 `json:"avg_aod"`
	Count      int      `json:"count"`
}

func (s *Store) GetCityComparison(eventType, startDate, endDate string) ([]CityComparison, error) {
	query := `SELECT city, AVG(quality), AVG(aod), COUNT(*)
		FROM sunset_data WHERE city != ''`

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

	query += ` GROUP BY city ORDER BY AVG(quality) DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []CityComparison
	for rows.Next() {
		var c CityComparison
		if err := rows.Scan(&c.City, &c.AvgQuality, &c.AvgAOD, &c.Count); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, rows.Err()
}
