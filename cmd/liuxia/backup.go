package main

import (
	"errors"
	"flag"
	"io"
	"log"
	"os"
)

// runBackup 实现 `liuxia backup`：把数据库的一致性快照写到 w（由调用方给 os.Stdout）。
//
// stdout 只承载快照字节，日志一律走 stderr——外部备份工具直接把 stdout 当成产物，
// 混进一行日志就会得到一个校验失败的坏备份。
//
// 导出进程刻意只做这一件事：以 OpenForExport 打开（不建目录、不跑迁移），
// 用 VACUUM INTO 取事务级快照后流式输出，随后非零退出的唯一条件是出错。
func runBackup(args []string, w io.Writer) error {
	set := flag.NewFlagSet("liuxia backup", flag.ContinueOnError)
	// 参数用法与解析错误也不能污染 stdout。
	set.SetOutput(os.Stderr)
	dbPath := set.String("db", getDBPath(), "SQLite 数据文件路径（默认取环境变量 DB_PATH）")
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	st, err := OpenForExport(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.WriteSQLiteSnapshot(w); err != nil {
		return err
	}
	log.New(os.Stderr, "", log.LstdFlags).Printf("[备份] 已导出 SQLite 快照: %s", *dbPath)
	return nil
}
