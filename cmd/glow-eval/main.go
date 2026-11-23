// Command glow-eval 离线评估 glow 评分模型在 sunsetbot 上海标注集上的表现。
//
// 该工具不修改线上取数与推送链路：它读取（或抓取）标注与历史气象输入，
// 交给 internal/glow 的纯函数评分，再输出混淆矩阵与分层指标。
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	cityLat   = 31.2304
	cityLon   = 121.4737
	firstDate = "2022-06-24"
	lastDate  = "2023-12-30"
)

var upstreamKM = []float64{50, 100, 200, 300, 400}

var labelLevel = map[string]int{
	"基本不烧": 0,
	"微烧":   1,
	"小烧":   2,
	"小到中烧": 3,
	"中到大烧": 4,
	"大烧":   5,
}

var levelName = []string{"基本不烧", "微烧", "小烧", "小到中烧", "中到大烧", "大烧"}

type label struct {
	Date       string `json:"date"`
	Event      string `json:"event"`
	Cloud      string `json:"cloud"`
	Color      string `json:"color"`
	Actual     string `json:"actual"`
	Predicted  string `json:"predicted"`
	Consistent string `json:"consistent"`
	Phenomena  string `json:"phenomena"`
	CaseURL    string `json:"case_url"`
	MapURL     string `json:"map_url"`
	SectionURL string `json:"section_url"`
}

type event struct {
	Label   label
	Date    time.Time
	Morning bool
	Bearing float64
}

type batch struct {
	Key     string
	Start   string
	End     string
	Bearing float64
	Events  []*event
}

type evalRow struct {
	Date       string
	Event      string
	EventTime  string
	Actual     int
	Predicted  int
	Sunset     int
	Score      float64
	Prob       float64
	Missing    []string
	Components map[string]float64
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "calibrate" {
		calibrateMain(os.Args[2:])
		return
	}
	cacheDir := flag.String("cache", ".cache/glow-eval", "标注与历史输入缓存目录")
	refresh := flag.Bool("refresh", false, "忽略缓存重新抓取")
	limit := flag.Int("limit", 0, "只评估前 N 条标注（0 表示全部）")
	out := flag.String("out", "", "报告输出文件；为空则输出到标准输出")
	flag.Parse()

	client := &http.Client{Timeout: 45 * time.Second}
	labels, err := fetchLabels(client, *cacheDir, *refresh)
	if err != nil {
		fmt.Fprintln(os.Stderr, "抓取标注失败:", err)
		os.Exit(1)
	}
	if *limit > 0 && *limit < len(labels) {
		labels = labels[:*limit]
	}
	fmt.Fprintf(os.Stderr, "标注 %d 条，按周分组 %d 批\n", len(labels), len(groupEvents(labels)))

	rows, err := evaluateAll(client, *cacheDir, *refresh, labels)
	if err != nil {
		fmt.Fprintln(os.Stderr, "评估失败:", err)
		os.Exit(1)
	}

	report := buildReport(labels, rows)
	if *out == "" {
		fmt.Print(report)
		return
	}
	if err := os.MkdirAll(dirOf(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "创建输出目录失败:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写入报告失败:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "报告已写入 %s\n", *out)
}

func dirOf(path string) string {
	i := strings.LastIndexAny(path, "/\\")
	if i < 0 {
		return "."
	}
	return path[:i]
}

func evaluateAll(client *http.Client, cacheDir string, refresh bool, labels []label) ([]evalRow, error) {
	batches := groupEvents(labels)
	keys := make([]string, 0, len(batches))
	for k := range batches {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	rows := make([]evalRow, 0, len(labels))
	for i, key := range keys {
		b := batches[key]
		bundle, err := fetchBundle(client, cacheDir, b, refresh)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		for _, ev := range b.Events {
			row, err := evaluateEvent(ev, bundle)
			if err != nil {
				return nil, fmt.Errorf("%s %s %s: %w", ev.Label.Date, ev.Label.Event, key, err)
			}
			rows = append(rows, row)
		}
		fmt.Fprintf(os.Stderr, "[%d/%d] %s 完成 %d 场\n", i+1, len(keys), key, len(b.Events))
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Date != rows[j].Date {
			return rows[i].Date < rows[j].Date
		}
		return rows[i].Event < rows[j].Event
	})
	return rows, nil
}
