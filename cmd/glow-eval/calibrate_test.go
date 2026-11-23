package main

import (
	"math"
	"testing"
)

func TestLevelFromBounds(t *testing.T) {
	bounds := []float64{20, 35, 50, 65, 80}
	cases := []struct {
		score float64
		want  int
	}{
		{0, 0}, {19.9, 0}, {20, 1}, {34.9, 1}, {35, 2},
		{50, 3}, {65, 4}, {80, 5}, {100, 5},
	}
	for _, c := range cases {
		if got := levelFromBounds(c.score, bounds); got != c.want {
			t.Errorf("levelFromBounds(%v) = %d, want %d", c.score, got, c.want)
		}
	}
}

func TestMetricsWithBounds(t *testing.T) {
	rows := []evalRow{
		{Score: 10, Actual: 0},
		{Score: 30, Actual: 1},
		{Score: 90, Actual: 3},
	}
	m := metricsWithBounds(rows, []float64{20, 35, 50, 65, 80})
	if m.N != 3 || m.Exact != 2 || m.Within1 != 2 {
		t.Errorf("unexpected counts: %+v", m)
	}
	if math.Abs(m.MAE-2.0/3.0) > 1e-9 {
		t.Errorf("MAE = %v, want %.6f", m.MAE, 2.0/3.0)
	}
	if math.Abs(m.MSE-4.0/3.0) > 1e-9 {
		t.Errorf("MSE = %v, want %.6f", m.MSE, 4.0/3.0)
	}
}

func TestOptimizeBoundsThreeLevels(t *testing.T) {
	var rows []evalRow
	for _, c := range []struct {
		score float64
		level int
	}{
		{10, 0}, {50, 1}, {90, 2},
	} {
		for i := 0; i < 20; i++ {
			rows = append(rows, evalRow{Score: c.score, Actual: c.level})
		}
	}
	bounds := optimizeBounds(rows, "mse")
	if bounds[0] < 10 || bounds[0] > 50 {
		t.Errorf("first bound = %v, want between 10 and 50", bounds[0])
	}
	if bounds[1] < 50 || bounds[1] > 90 {
		t.Errorf("second bound = %v, want between 50 and 90", bounds[1])
	}
	for i := 2; i < len(bounds); i++ {
		if !math.IsInf(bounds[i], 1) {
			t.Errorf("bound[%d] = %v, want +Inf", i, bounds[i])
		}
	}
	m := metricsWithBounds(rows, bounds)
	if m.Exact != len(rows) {
		t.Errorf("exact = %d, want %d", m.Exact, len(rows))
	}
}

func TestOptimizeBoundsMAE(t *testing.T) {
	rows := []evalRow{
		{Score: 10, Actual: 0}, {Score: 10, Actual: 0},
		{Score: 90, Actual: 1}, {Score: 90, Actual: 1},
	}
	bounds := optimizeBounds(rows, "mae")
	if bounds[0] <= 10 || bounds[0] > 90 {
		t.Errorf("MAE bound = %v, want between 10 and 90", bounds[0])
	}
}

func TestAUCWithTies(t *testing.T) {
	values := []float64{0.1, 0.1, 0.2}
	binary := []bool{true, false, true}
	if got := auc(values, binary); math.Abs(got-0.75) > 1e-9 {
		t.Errorf("auc = %v, want 0.75", got)
	}
	values = []float64{0.1, 0.2, 0.3, 0.4}
	binary = []bool{false, false, true, true}
	if got := auc(values, binary); math.Abs(got-1) > 1e-9 {
		t.Errorf("auc = %v, want 1", got)
	}
}

func TestFitLogisticVecDeterministic(t *testing.T) {
	x := [][]float64{{0}, {0.5}, {1}, {1.5}}
	y := []float64{0, 0, 1, 1}
	b1, w1 := fitLogisticVec(x, y, 1, 0.1, 200)
	b2, w2 := fitLogisticVec(x, y, 1, 0.1, 200)
	if b1 != b2 || w1[0] != w2[0] {
		t.Fatalf("fit not deterministic: (%v,%v) vs (%v,%v)", b1, w1, b2, w2)
	}
	for _, z := range []float64{b1 + w1[0]*x[0][0], b1 + w1[0]*x[3][0]} {
		p := sigmoid(z)
		if p <= 0 || p >= 1 {
			t.Errorf("prediction out of range: %v", p)
		}
	}
}

func TestFitProbModelSlopePositive(t *testing.T) {
	var rows []evalRow
	for i := 0; i < 40; i++ {
		rows = append(rows, evalRow{Score: 10, Actual: 0})
		rows = append(rows, evalRow{Score: 90, Actual: 1})
	}
	m := fitProbModel(rows)
	if m.b <= 0 {
		t.Errorf("slope = %v, want > 0", m.b)
	}
	if m.p(90) <= m.p(10) {
		t.Errorf("p(90)=%v should exceed p(10)=%v", m.p(90), m.p(10))
	}
}

func TestComponentStatsMissingFilledNeutral(t *testing.T) {
	rows := []evalRow{
		{Components: map[string]float64{"curtainV2": 40}},
		{Components: map[string]float64{}},
	}
	means, stds := componentStats(rows)
	if math.Abs(means[0]-45) > 1e-9 {
		t.Errorf("mean = %v, want 45", means[0])
	}
	if stds[0] <= 0 {
		t.Errorf("std = %v, want > 0", stds[0])
	}
	if got := componentValuesFor(rows[1], "curtainV2"); got != 50 {
		t.Errorf("missing component fill = %v, want 50", got)
	}
}
