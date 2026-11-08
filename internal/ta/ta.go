// Package ta holds the indicators exposed to strategy code. Every function returns a slice of the
// input's length with NaN where the value is not yet defined. After adding one, re-run go generate
// in internal/strategy and document it in internal/llm/prompt.md.
package ta

import (
	"math"
	"stratest/internal/data"
)

type Frame = data.Frame

func nans(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = math.NaN()
	}
	return out
}

func window(x []float64, n int, f func([]float64) float64) []float64 {
	out := nans(len(x))
	for i := n - 1; n > 0 && i < len(x); i++ {
		out[i] = f(x[i-n+1 : i+1])
	}
	return out
}

func zip(a, b []float64, f func(a, b float64) float64) []float64 {
	out := make([]float64, len(a))
	for i := range out {
		out[i] = f(a[i], b[i])
	}
	return out
}

func apply(x []float64, f func(float64) float64) []float64 {
	out := make([]float64, len(x))
	for i, v := range x {
		out[i] = f(v)
	}
	return out
}

func mean(x []float64) float64 {
	sum := 0.0
	for _, v := range x {
		sum += v
	}
	return sum / float64(len(x))
}

func stdDev(x []float64) float64 {
	m, sum := mean(x), 0.0
	for _, v := range x {
		sum += (v - m) * (v - m)
	}
	return math.Sqrt(sum / float64(len(x)))
}

func reduce(x []float64, f func(a, b float64) float64) float64 {
	out := x[0]
	for _, v := range x[1:] {
		out = f(out, v)
	}
	return out
}

// smooth is an exponential average seeded with the SMA of the first n valid values; NaN inputs give NaN.
func smooth(x []float64, n int, alpha float64) []float64 {
	out := nans(len(x))
	value, count := 0.0, 0
	for i, v := range x {
		switch {
		case math.IsNaN(v):
		case count < n:
			value, count = value+v/float64(n), count+1
			if count == n {
				out[i] = value
			}
		default:
			value = alpha*v + (1-alpha)*value
			out[i] = value
		}
	}
	return out
}

func SMA(x []float64, n int) []float64    { return window(x, n, mean) }
func StdDev(x []float64, n int) []float64 { return window(x, n, stdDev) }
func Highest(x []float64, n int) []float64 {
	return window(x, n, func(w []float64) float64 { return reduce(w, math.Max) })
}
func Lowest(x []float64, n int) []float64 {
	return window(x, n, func(w []float64) float64 { return reduce(w, math.Min) })
}
func EMA(x []float64, n int) []float64 { return smooth(x, n, 2/float64(n+1)) }
func RMA(x []float64, n int) []float64 { return smooth(x, n, 1/float64(n)) }

// RSI uses Wilder smoothing.
func RSI(x []float64, n int) []float64 {
	change := zip(x, append([]float64{math.NaN()}, x[:max(len(x)-1, 0)]...), func(a, b float64) float64 { return a - b })
	gain := RMA(apply(change, func(c float64) float64 { return math.Max(c, 0) }), n)
	loss := RMA(apply(change, func(c float64) float64 { return math.Max(-c, 0) }), n)
	return zip(gain, loss, func(g, l float64) float64 {
		if l == 0 {
			return 100
		}
		return 100 - 100/(1+g/l)
	})
}

// ATR uses Wilder smoothing of the true range.
func ATR(f Frame, n int) []float64 {
	tr := make([]float64, len(f.Close))
	for i := range tr {
		tr[i] = f.High[i] - f.Low[i]
		if i > 0 {
			tr[i] = max(tr[i], math.Abs(f.High[i]-f.Close[i-1]), math.Abs(f.Low[i]-f.Close[i-1]))
		}
	}
	return RMA(tr, n)
}

func MACD(x []float64, fast, slow, signal int) (macd, sig, hist []float64) {
	macd = zip(EMA(x, fast), EMA(x, slow), func(a, b float64) float64 { return a - b })
	sig = EMA(macd, signal)
	return macd, sig, zip(macd, sig, func(a, b float64) float64 { return a - b })
}

func Bollinger(x []float64, n int, k float64) (mid, up, lo []float64) {
	mid, sd := SMA(x, n), StdDev(x, n)
	return mid, zip(mid, sd, func(m, s float64) float64 { return m + k*s }), zip(mid, sd, func(m, s float64) float64 { return m - k*s })
}

func cross(a, b []float64, above bool) []bool {
	out := make([]bool, len(a))
	for i := 1; i < len(a); i++ {
		if above {
			out[i] = a[i] > b[i] && a[i-1] <= b[i-1]
		} else {
			out[i] = a[i] < b[i] && a[i-1] >= b[i-1]
		}
	}
	return out
}

func CrossOver(a, b []float64) []bool  { return cross(a, b, true) }
func CrossUnder(a, b []float64) []bool { return cross(a, b, false) }
