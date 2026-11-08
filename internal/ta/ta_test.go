package ta

import (
	"math"
	"testing"
)

func same(a, b []float64) bool {
	for i := range a {
		if !(a[i] == b[i] || math.IsNaN(a[i]) && math.IsNaN(b[i]) || math.Abs(a[i]-b[i]) < 1e-9) {
			return false
		}
	}
	return len(a) == len(b)
}

func TestIndicators(t *testing.T) {
	nan := math.NaN()
	x := []float64{1, 2, 3, 4, 5}
	if got := SMA(x, 3); !same(got, []float64{nan, nan, 2, 3, 4}) {
		t.Errorf("SMA %v", got)
	}
	if got := EMA(x, 3); !same(got, []float64{nan, nan, 2, 3, 4}) {
		t.Errorf("EMA %v", got)
	}
	if got := Highest([]float64{3, 1, 2, 5, 4}, 2); !same(got, []float64{nan, 3, 2, 5, 5}) {
		t.Errorf("Highest %v", got)
	}
	if got := RSI(x, 2); !math.IsNaN(got[1]) || got[2] != 100 || got[4] != 100 {
		t.Errorf("RSI of a rising series %v", got)
	}
	if got := SMA([]float64{nan, 1, 2, 3}, 2); !same(got, []float64{nan, nan, 1.5, 2.5}) {
		t.Errorf("SMA should recover after NaN warm-up: %v", got)
	}
	up := CrossOver([]float64{1, 2, 3, 2}, []float64{2, 2, 2, 2})
	down := CrossUnder([]float64{1, 2, 3, 1}, []float64{2, 2, 2, 2})
	if up[1] || !up[2] || up[3] || !down[3] || down[2] {
		t.Errorf("crosses %v %v", up, down)
	}
	mid, upper, lower := Bollinger(x, 5, 2)
	if mid[4] != 3 || !same(upper[4:], []float64{3 + 2*math.Sqrt2}) || !same(lower[4:], []float64{3 - 2*math.Sqrt2}) {
		t.Errorf("Bollinger %v %v %v", mid, upper, lower)
	}
}
