package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAUC(t *testing.T) {
	cases := []struct {
		name   string
		values []float64
		binary []bool
		want   float64
	}{
		{"perfect", []float64{0.1, 0.2, 0.3, 0.4}, []bool{false, false, true, true}, 1},
		{"tie", []float64{1, 2, 2, 3}, []bool{false, true, false, true}, 0.875},
		{"reversed", []float64{4, 3, 2, 1}, []bool{true, true, false, false}, 1},
		{"single class", []float64{1, 2}, []bool{false, false}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := auc(c.values, c.binary)
			if math.Abs(got-c.want) > 1e-9 {
				t.Fatalf("auc = %v, want %v", got, c.want)
			}
		})
	}
}

func TestMetricsAndConfusion(t *testing.T) {
	rows := []evalRow{
		{Actual: 0, Predicted: 0},
		{Actual: 1, Predicted: 2},
		{Actual: 5, Predicted: 3},
	}
	m := basicMetrics(rows)
	if m.N != 3 || m.Exact != 1 || m.Within1 != 2 {
		t.Fatalf("metrics = %+v", m)
	}
	if math.Abs(m.MAE-1) > 1e-9 {
		t.Fatalf("MAE = %v, want 1", m.MAE)
	}
	cm := confusionMatrix(rows)
	if cm[0][0] != 1 || cm[1][2] != 1 || cm[5][3] != 1 {
		t.Fatalf("confusion = %v", cm)
	}
}

func TestFetchLabelsUsesCache(t *testing.T) {
	dir := t.TempDir()
	want := []label{{Date: "2022-06-24", Event: "日落", Actual: "大烧", Predicted: "大烧"}}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "labels.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fetchLabels(nil, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Actual != "大烧" {
		t.Fatalf("got %+v", got)
	}
}

func TestGroupEvents(t *testing.T) {
	labels := []label{
		{Date: "2022-06-24", Event: "日落", Actual: "大烧", Predicted: "大烧"},
		{Date: "2022-06-25", Event: "日出", Actual: "微烧", Predicted: "微烧"},
		{Date: "2022-06-25", Event: "日落", Actual: "小烧", Predicted: "小烧"},
	}
	groups := groupEvents(labels)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if groups["2022-W25-set"] == nil || len(groups["2022-W25-set"].Events) != 2 {
		t.Fatalf("set group = %+v", groups["2022-W25-set"])
	}
	if groups["2022-W25-rise"] == nil || len(groups["2022-W25-rise"].Events) != 1 {
		t.Fatalf("rise group = %+v", groups["2022-W25-rise"])
	}
}

func TestEvaluateEventFixture(t *testing.T) {
	times := []string{"2022-06-24T18:00", "2022-06-24T19:00", "2022-06-24T20:00"}
	cloud := []*float64{ptrF(40), ptrF(35), ptrF(30)}
	val := func(v float64) *float64 { return &v }
	mkLoc := func() location {
		var l location
		l.UtcOffsetSeconds = 8 * 3600
		l.Hourly.Time = times
		l.Hourly.CloudCover = cloud
		l.Hourly.CloudCoverLow = []*float64{val(20), val(15), val(10)}
		l.Hourly.CloudCoverMid = []*float64{val(30), val(35), val(40)}
		l.Hourly.CloudCoverHigh = []*float64{val(50), val(55), val(60)}
		l.Hourly.Precipitation = []*float64{val(0), val(0), val(0)}
		l.Hourly.Visibility = []*float64{val(20000), val(20000), val(20000)}
		l.Hourly.Temperature2m = []*float64{val(28), val(27), val(26)}
		l.Hourly.DewPoint2m = []*float64{val(22), val(22), val(21)}
		l.Hourly.WindSpeed10m = []*float64{val(10), val(10), val(9)}
		l.Hourly.WindDirection10m = []*float64{val(180), val(180), val(180)}
		l.Hourly.Cape = []*float64{val(500), val(500), val(450)}
		l.Daily.Time = []string{"2022-06-24"}
		l.Daily.Sunrise = []string{"2022-06-24T04:51"}
		l.Daily.Sunset = []string{"2022-06-24T19:01"}
		return l
	}
	var airData air
	airData.UtcOffsetSeconds = 8 * 3600
	airData.Hourly.Time = times
	airData.Hourly.AOD = []*float64{val(0.15), val(0.15), val(0.15)}
	airData.Hourly.PM25 = []*float64{val(20), val(20), val(18)}

	ev := &event{
		Label: label{Date: "2022-06-24", Event: "日落", Actual: "大烧", Predicted: "大烧"},
		Date:  time.Date(2022, 6, 24, 0, 0, 0, 0, time.UTC),
	}
	data := bundle{Locations: []location{mkLoc(), mkLoc()}, Air: airData}
	row, err := evaluateEvent(ev, data)
	if err != nil {
		t.Fatal(err)
	}
	if row.EventTime != "19:01" {
		t.Fatalf("event time = %q, want 19:01", row.EventTime)
	}
	if row.Predicted < 0 || row.Predicted > 5 {
		t.Fatalf("predicted level = %d", row.Predicted)
	}
	if math.IsNaN(row.Score) || math.IsInf(row.Score, 0) {
		t.Fatalf("score = %v", row.Score)
	}
}

func ptrF(v float64) *float64 { return &v }
