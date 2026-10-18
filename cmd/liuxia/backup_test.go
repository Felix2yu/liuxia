package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// sqliteMagic 是 SQLite 数据库文件头 16 字节，备份工具按它校验产物。
const sqliteMagic = "SQLite format 3\x00"

// TestRunBackupSnapshot 走通 `liuxia backup` 的全链路：
// 源库仍在被写入（WAL 里有刚提交的数据）时导出，产物应是带魔数、可独立打开、
// 且包含最近一次提交内容的库。
func TestRunBackupSnapshot(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "liuxia.db")

	store, err := InitStore(dbPath)
	if err != nil {
		t.Fatalf("InitStore() 失败: %v", err)
	}
	// 故意不在导出前 Close：模拟运行实例持着 WAL 的常态。
	defer store.Close()

	quality := 0.85
	if err := store.UpsertRecord(SunsetRecord{
		City: "江苏省-苏州", Date: "2026-01-01", Time: "17:30",
		EventType: "evening", Model: "GFS", Quality: &quality,
	}); err != nil {
		t.Fatalf("UpsertRecord(旧数据) 失败: %v", err)
	}
	if err := store.UpsertRecord(SunsetRecord{
		City: "江苏省-苏州", Date: "2026-07-15", Time: "19:02",
		EventType: "evening", Model: "EC", Quality: &quality,
	}); err != nil {
		t.Fatalf("UpsertRecord(最新数据) 失败: %v", err)
	}

	var buf bytes.Buffer
	if err := runBackup([]string{"-db", dbPath}, &buf); err != nil {
		t.Fatalf("runBackup() 失败: %v", err)
	}

	got := buf.Bytes()
	if len(got) < 16 {
		t.Fatalf("快照仅 %d 字节，过短", len(got))
	}
	if string(got[:16]) != sqliteMagic {
		t.Errorf("快照头部 16 字节 = %q, want %q", got[:16], sqliteMagic)
	}

	// 落盘后按只读方式重新打开：内容必须自洽，且刚提交（还在 -wal 里）的行也在。
	snapshot := filepath.Join(dir, "snapshot.db")
	if err := os.WriteFile(snapshot, got, 0o644); err != nil {
		t.Fatalf("写快照文件失败: %v", err)
	}
	restored, err := OpenForExport(snapshot)
	if err != nil {
		t.Fatalf("打开快照失败: %v", err)
	}
	defer restored.Close()

	var integrity string
	if err := restored.db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		t.Fatalf("integrity_check 失败: %v", err)
	}
	if integrity != "ok" {
		t.Errorf("integrity_check = %q, want ok", integrity)
	}

	total, err := restored.GetTotalRecords()
	if err != nil {
		t.Fatalf("GetTotalRecords() 失败: %v", err)
	}
	if total != 2 {
		t.Errorf("快照内记录数 = %d, want 2", total)
	}
	records, err := restored.QueryRecords("江苏省-苏州", "evening", "2026-07-01", "2026-07-31")
	if err != nil {
		t.Fatalf("QueryRecords() 失败: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("7 月快照记录数 = %d, want 1", len(records))
	}
	if records[0].Model != "EC" || records[0].Date != "2026-07-15" {
		t.Errorf("最新一行未进入快照: %+v", records[0])
	}
}

// TestRunBackupRejectsMissingDB 数据文件不存在时报错退出，绝不新建一个空库当备份。
func TestRunBackupRejectsMissingDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "liuxia.db")

	var buf bytes.Buffer
	if err := runBackup([]string{"-db", dbPath}, &buf); err == nil {
		t.Error("runBackup() 在数据文件缺失时应该返回错误")
	}
	if buf.Len() != 0 {
		t.Errorf("失败时不应有任何产物字节，实际 %d 字节", buf.Len())
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Errorf("不应创建数据文件, stat err=%v", err)
	}
}

// TestOpenForExportSkipsMigration 导出打开只做读取：不建目录、不跑建表与建索引迁移。
func TestOpenForExportSkipsMigration(t *testing.T) {
	dir := t.TempDir()

	// 只含无关表的老库：业务表必须原样缺席，说明导出路径没跑迁移。
	dbPath := filepath.Join(dir, "raw.db")
	raw, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("创建老库失败: %v", err)
	}
	if _, err := raw.Exec("CREATE TABLE dummy(id INTEGER)"); err != nil {
		t.Fatalf("创建老表失败: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("关闭老库失败: %v", err)
	}

	store, err := OpenForExport(dbPath)
	if err != nil {
		t.Fatalf("OpenForExport() 失败: %v", err)
	}
	defer store.Close()

	var tables int
	err = store.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sunset_data'`).Scan(&tables)
	if err != nil {
		t.Fatalf("读 sqlite_master 失败: %v", err)
	}
	if tables != 0 {
		t.Error("OpenForExport() 不应执行迁移创建业务表")
	}
}
