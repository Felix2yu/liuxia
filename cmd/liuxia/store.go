package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db    *sql.DB
	Cache *Cache
}

type SunsetRecord struct {
	ID          int64    `json:"id"`
	City        string   `json:"city"`
	Date        string   `json:"date"`
	Time        string   `json:"time"`
	EventType   string   `json:"event_type"`
	Model       string   `json:"model"`
	AOD         *float64 `json:"aod"`
	Score       *float64 `json:"score,omitempty"`
	Level       *int     `json:"level,omitempty"`
	Grade       string   `json:"grade,omitempty"`
	Probability *float64 `json:"probability,omitempty"`
	Clarity     string   `json:"clarity,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

func InitStore(dbPath string) (*Store, error) {
	// WAL 模式提升并发读写能力，busy_timeout 让写冲突时自动等待而非立即报
	// "database is locked"。该应用写入量低，限制单连接即可彻底避免写锁竞争。
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	schema := `
	CREATE TABLE IF NOT EXISTS sunset_data (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		city TEXT NOT NULL DEFAULT '',
		date TEXT NOT NULL,
		time TEXT NOT NULL,
		event_type TEXT NOT NULL,
		model TEXT NOT NULL,
		aod REAL,
		score REAL,
		level INTEGER,
		grade TEXT,
		probability REAL,
		clarity TEXT,
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

	// sunsetbot 旧评分已停用：旧库中的 quality 列在迁移时删除，
	// 仅保留 aod（Open-Meteo 物理量）与本地计算的 score 体系。
	var qualityExists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sunset_data') WHERE name='quality'`).Scan(&qualityExists); err != nil {
		return nil, fmt.Errorf("check quality column: %w", err)
	}
	if qualityExists > 0 {
		if _, err := db.Exec(`ALTER TABLE sunset_data DROP COLUMN quality`); err != nil {
			return nil, fmt.Errorf("drop quality column: %w", err)
		}
	}

	// 新评分列：只补缺失列，不改写任何历史数据。
	for _, col := range []struct{ name, def string }{
		{"score", "REAL"},
		{"level", "INTEGER"},
		{"grade", "TEXT"},
		{"probability", "REAL"},
		{"clarity", "TEXT"},
	} {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sunset_data') WHERE name=?`, col.name).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check %s column: %w", col.name, err)
		}
		if exists == 0 {
			if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE sunset_data ADD COLUMN %s %s`, col.name, col.def)); err != nil {
				return nil, fmt.Errorf("add %s column: %w", col.name, err)
			}
		}
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_city ON sunset_data(city)`,
		`CREATE INDEX IF NOT EXISTS idx_date ON sunset_data(date)`,
		`CREATE INDEX IF NOT EXISTS idx_event_type ON sunset_data(event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_city_date ON sunset_data(city, date)`,
		`CREATE INDEX IF NOT EXISTS idx_date_event ON sunset_data(date, event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_score ON sunset_data(score)`,
	}
	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			return nil, fmt.Errorf("create index: %w", err)
		}
	}

	return &Store{db: db, Cache: NewCache(5 * time.Minute)}, nil
}

// OpenForExport 为「导出快照」打开数据库：只读已存在的数据文件，
// 不建目录、不跑建表/加列/建索引这些迁移，也不写任何种子数据。
//
// 备份进程与运行实例指向同一个库，schema 的变更只属于正常启动路径；
// 文件不存在时直接失败，绝不允许导出进程凭空造出一个空库当"备份"。
func OpenForExport(dbPath string) (*Store, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("数据文件不可用: %w", err)
	}
	// 刻意不设 journal_mode：连接一旦改写库头部，就可能扰动运行实例的一致性判断；
	// 已存在的 WAL 数据照常读取，busy_timeout 保证撞上写锁时等待而非立即失败。
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// 导出是一次顺序读，一条连接足够，也避免多余连接各自创建 -shm 句柄。
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect db: %w", err)
	}
	// 导出路径不查询也不写数据，用不到缓存，保持 Cache 为 nil。
	return &Store{db: db}, nil
}

// WriteSQLiteSnapshot 把数据库以 SQLite 自身的一致性快照写到 w。
//
// 用 VACUUM INTO 而不是复制文件：热拷贝正在被写入的 .db/.db-wal/.db-shm 不是原子
// 操作，容易拷出损坏副本或漏掉 WAL 里已提交的数据；VACUUM INTO 由 SQLite 在事务里
// 生成一份自洽的快照，WAL 模式下也不阻塞写入。镜像里通常也没有 sqlite3 CLI 可用。
//
// 快照先落在临时目录再流式输出：VACUUM INTO 要求目标不存在，且这样无论调用方读到
// 一半是否断开，都不会在数据目录里留下半成品。
func (s *Store) WriteSQLiteSnapshot(w io.Writer) error {
	dir, err := os.MkdirTemp("", "liuxia-snapshot-")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(dir)

	target := filepath.Join(dir, "snapshot.db")
	if _, err := s.db.Exec("VACUUM INTO ?", target); err != nil {
		return fmt.Errorf("导出数据库快照失败: %w", err)
	}

	f, err := os.Open(target)
	if err != nil {
		return fmt.Errorf("读取快照失败: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("写出快照失败: %w", err)
	}
	return nil
}

func (s *Store) UpsertRecord(r SunsetRecord) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := s.db.Exec(`
		INSERT INTO sunset_data (city, date, time, event_type, model, aod,
			score, level, grade, probability, clarity, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(city, date, event_type, model) DO UPDATE SET
			time = excluded.time,
			aod = excluded.aod,
			score = excluded.score,
			level = excluded.level,
			grade = excluded.grade,
			probability = excluded.probability,
			clarity = excluded.clarity,
			updated_at = excluded.updated_at`,
		r.City, r.Date, r.Time, r.EventType, r.Model, r.AOD,
		r.Score, r.Level, r.Grade, r.Probability, r.Clarity, now, now)
	if err == nil {
		s.Cache.Clear()
	}
	return err
}

func (s *Store) QueryRecords(city, eventType, startDate, endDate string) ([]SunsetRecord, error) {
	query := `SELECT id, city, date, time, event_type, model, aod,
		score, level, COALESCE(grade, '') AS grade, probability, COALESCE(clarity, '') AS clarity, created_at, updated_at
		FROM sunset_data WHERE date >= '2020-01-01'`
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
	query += ` ORDER BY date DESC, CASE event_type WHEN 'morning' THEN 0 ELSE 1 END ASC, model ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []SunsetRecord
	for rows.Next() {
		var r SunsetRecord
		if err := rows.Scan(&r.ID, &r.City, &r.Date, &r.Time, &r.EventType, &r.Model,
			&r.AOD, &r.Score, &r.Level, &r.Grade, &r.Probability, &r.Clarity,
			&r.CreatedAt, &r.UpdatedAt); err != nil {
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
	if err := writer.Write([]string{"city", "date", "time", "event_type", "model", "aod",
		"score", "level", "grade", "probability", "clarity", "created_at", "updated_at"}); err != nil {
		return err
	}
	for _, r := range records {
		aStr := ""
		if r.AOD != nil {
			aStr = fmt.Sprintf("%.4f", *r.AOD)
		}
		scoreStr := ""
		if r.Score != nil {
			scoreStr = fmt.Sprintf("%.2f", *r.Score)
		}
		levelStr := ""
		if r.Level != nil {
			levelStr = strconv.Itoa(*r.Level)
		}
		probStr := ""
		if r.Probability != nil {
			probStr = fmt.Sprintf("%.4f", *r.Probability)
		}
		if err := writer.Write([]string{r.City, r.Date, r.Time, r.EventType, r.Model, aStr,
			scoreStr, levelStr, r.Grade, probStr, r.Clarity, r.CreatedAt, r.UpdatedAt}); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
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
	Model     string   `json:"model"`
	EventType string   `json:"event_type"`
	Count     int      `json:"count"`
	MinAOD    *float64 `json:"min_aod"`
	MaxAOD    *float64 `json:"max_aod"`
	AvgAOD    *float64 `json:"avg_aod"`
	AvgScore  *float64 `json:"avg_score,omitempty"`
}

type MonthlyStats struct {
	Month    string   `json:"month"`
	Count    int      `json:"count"`
	AvgAOD   *float64 `json:"avg_aod"`
	AvgScore *float64 `json:"avg_score,omitempty"`
}

type Statistics struct {
	TotalRecords int            `json:"total_records"`
	Models       []ModelStats   `json:"models"`
	Monthly      []MonthlyStats `json:"monthly"`
}

func (s *Store) GetStatistics(city, eventType, startDate, endDate string) (*Statistics, error) {
	stats := &Statistics{}

	countQuery := `SELECT COUNT(*) FROM sunset_data WHERE date >= '2020-01-01'`
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
		MIN(aod), MAX(aod), AVG(aod), AVG(score)
		FROM sunset_data WHERE date >= '2020-01-01'`

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
			&m.MinAOD, &m.MaxAOD, &m.AvgAOD, &m.AvgScore); err != nil {
			return nil, err
		}
		stats.Models = append(stats.Models, m)
	}

	monthlyQuery := `SELECT substr(date, 1, 7) as month, COUNT(*), AVG(aod), AVG(score)
		FROM sunset_data WHERE date >= '2020-01-01'`

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
		if err := mRows.Scan(&m.Month, &m.Count, &m.AvgAOD, &m.AvgScore); err != nil {
			return nil, err
		}
		stats.Monthly = append(stats.Monthly, m)
	}

	if stats.Models == nil {
		stats.Models = []ModelStats{}
	}
	if stats.Monthly == nil {
		stats.Monthly = []MonthlyStats{}
	}
	return stats, nil
}

func (s *Store) GetCities() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT city FROM sunset_data WHERE city != '' AND date >= '2020-01-01' ORDER BY city`)
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
	if cities == nil {
		cities = []string{}
	}
	return cities, rows.Err()
}

func (s *Store) GetTotalRecords() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sunset_data`).Scan(&count)
	return count, err
}

func (s *Store) DeleteOldRecords(daysToKeep int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -daysToKeep).Format("2006-01-02")
	result, err := s.db.Exec(`
		DELETE FROM sunset_data
		WHERE date < ?`,
		cutoff)
	if err != nil {
		return 0, err
	}
	// 数据已变更，失效缓存以保证看板立即反映最新状态。
	s.Cache.Clear()
	return result.RowsAffected()
}

func (s *Store) DeleteAbnormalDates() (int64, error) {
	result, err := s.db.Exec(`DELETE FROM sunset_data WHERE date < '2020-01-01'`)
	if err != nil {
		return 0, err
	}
	s.Cache.Clear()
	return result.RowsAffected()
}

type CityComparison struct {
	City     string   `json:"city"`
	AvgAOD   *float64 `json:"avg_aod"`
	AvgScore *float64 `json:"avg_score,omitempty"`
	Count    int      `json:"count"`
}

func (s *Store) GetCityComparison(eventType, startDate, endDate string) ([]CityComparison, error) {
	query := `SELECT city, AVG(aod), AVG(score), COUNT(*)
		FROM sunset_data WHERE city != '' AND date >= '2020-01-01'`

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

	query += ` GROUP BY city ORDER BY AVG(score) DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []CityComparison
	for rows.Next() {
		var c CityComparison
		if err := rows.Scan(&c.City, &c.AvgAOD, &c.AvgScore, &c.Count); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	if results == nil {
		results = []CityComparison{}
	}
	return results, rows.Err()
}

type DateRanking struct {
	Date      string   `json:"date"`
	City      string   `json:"city"`
	Time      string   `json:"time"`
	EventType string   `json:"event_type"`
	Model     string   `json:"model"`
	AOD       *float64 `json:"aod"`
	Score     *float64 `json:"score,omitempty"`
	Level     *int     `json:"level,omitempty"`
	Grade     string   `json:"grade,omitempty"`
}

type MonthRanking struct {
	Month    string   `json:"month"`
	AvgAOD   *float64 `json:"avg_aod"`
	AvgScore *float64 `json:"avg_score,omitempty"`
	Count    int      `json:"count"`
}

type SeasonRanking struct {
	Season   string   `json:"season"`
	AvgAOD   *float64 `json:"avg_aod"`
	AvgScore *float64 `json:"avg_score,omitempty"`
	Count    int      `json:"count"`
}

type Rankings struct {
	BestDates []DateRanking   `json:"best_dates"`
	Monthly   []MonthRanking  `json:"monthly"`
	Seasonal  []SeasonRanking `json:"seasonal"`
}

func (s *Store) GetTodayTomorrowData(city string) ([]SunsetRecord, error) {
	today := time.Now().Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

	query := `SELECT id, city, date, time, event_type, model, aod,
		score, level, COALESCE(grade, '') AS grade, probability, COALESCE(clarity, '') AS clarity, created_at, updated_at
		FROM sunset_data WHERE date IN (?, ?)`
	args := []interface{}{today, tomorrow}

	if city != "" {
		query += ` AND city = ?`
		args = append(args, city)
	}

	query += ` ORDER BY date ASC, CASE event_type WHEN 'morning' THEN 0 ELSE 1 END ASC, model ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []SunsetRecord
	for rows.Next() {
		var r SunsetRecord
		if err := rows.Scan(&r.ID, &r.City, &r.Date, &r.Time, &r.EventType, &r.Model,
			&r.AOD, &r.Score, &r.Level, &r.Grade, &r.Probability, &r.Clarity,
			&r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *Store) GetRankings(city, eventType, startDate, endDate string, limit int) (*Rankings, error) {
	rankings := &Rankings{}

	dateQuery := `SELECT city, date, time, event_type, model, aod, score, level, grade
		FROM sunset_data WHERE score IS NOT NULL AND date >= '2020-01-01'`
	args := []interface{}{}
	if city != "" {
		dateQuery += ` AND city = ?`
		args = append(args, city)
	}
	if eventType != "" {
		dateQuery += ` AND event_type = ?`
		args = append(args, eventType)
	}
	if startDate != "" {
		dateQuery += ` AND date >= ?`
		args = append(args, startDate)
	}
	if endDate != "" {
		dateQuery += ` AND date <= ?`
		args = append(args, endDate)
	}
	dateQuery += ` ORDER BY score DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(dateQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d DateRanking
		if err := rows.Scan(&d.City, &d.Date, &d.Time, &d.EventType, &d.Model,
			&d.AOD, &d.Score, &d.Level, &d.Grade); err != nil {
			return nil, err
		}
		rankings.BestDates = append(rankings.BestDates, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	monthQuery := `SELECT substr(date, 1, 7) as month, AVG(aod), AVG(score), COUNT(*)
		FROM sunset_data WHERE score IS NOT NULL AND date >= '2020-01-01'`
	mArgs := []interface{}{}
	if city != "" {
		monthQuery += ` AND city = ?`
		mArgs = append(mArgs, city)
	}
	if eventType != "" {
		monthQuery += ` AND event_type = ?`
		mArgs = append(mArgs, eventType)
	}
	if startDate != "" {
		monthQuery += ` AND date >= ?`
		mArgs = append(mArgs, startDate)
	}
	if endDate != "" {
		monthQuery += ` AND date <= ?`
		mArgs = append(mArgs, endDate)
	}
	monthQuery += ` GROUP BY month ORDER BY month`

	mRows, err := s.db.Query(monthQuery, mArgs...)
	if err != nil {
		return nil, err
	}
	defer mRows.Close()
	for mRows.Next() {
		var m MonthRanking
		if err := mRows.Scan(&m.Month, &m.AvgAOD, &m.AvgScore, &m.Count); err != nil {
			return nil, err
		}
		rankings.Monthly = append(rankings.Monthly, m)
	}
	if err := mRows.Err(); err != nil {
		return nil, err
	}

	seasonQuery := `SELECT
		CASE
			WHEN CAST(substr(date, 6, 2) AS INTEGER) IN (3,4,5) THEN '春季'
			WHEN CAST(substr(date, 6, 2) AS INTEGER) IN (6,7,8) THEN '夏季'
			WHEN CAST(substr(date, 6, 2) AS INTEGER) IN (9,10,11) THEN '秋季'
			ELSE '冬季'
		END as season,
		AVG(aod), AVG(score), COUNT(*)
		FROM sunset_data WHERE score IS NOT NULL AND date >= '2020-01-01'`
	sArgs := []interface{}{}
	if city != "" {
		seasonQuery += ` AND city = ?`
		sArgs = append(sArgs, city)
	}
	if eventType != "" {
		seasonQuery += ` AND event_type = ?`
		sArgs = append(sArgs, eventType)
	}
	if startDate != "" {
		seasonQuery += ` AND date >= ?`
		sArgs = append(sArgs, startDate)
	}
	if endDate != "" {
		seasonQuery += ` AND date <= ?`
		sArgs = append(sArgs, endDate)
	}
	seasonQuery += ` GROUP BY season ORDER BY AVG(score) DESC`

	sRows, err := s.db.Query(seasonQuery, sArgs...)
	if err != nil {
		return nil, err
	}
	defer sRows.Close()
	for sRows.Next() {
		var s SeasonRanking
		if err := sRows.Scan(&s.Season, &s.AvgAOD, &s.AvgScore, &s.Count); err != nil {
			return nil, err
		}
		rankings.Seasonal = append(rankings.Seasonal, s)
	}
	if err := sRows.Err(); err != nil {
		return nil, err
	}

	if rankings.BestDates == nil {
		rankings.BestDates = []DateRanking{}
	}
	if rankings.Monthly == nil {
		rankings.Monthly = []MonthRanking{}
	}
	if rankings.Seasonal == nil {
		rankings.Seasonal = []SeasonRanking{}
	}
	return rankings, nil
}
