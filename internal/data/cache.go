package data

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type TimeRange struct{ From, To time.Time }

// cacheFile records the ranges already requested, not just bar times, so weekends and
// holidays never look like gaps worth refetching.
type cacheFile struct {
	Bars   []Bar
	Ranges []TimeRange
}

func cachePath(dir, provider, interval, ticker string) string {
	return filepath.Join(dir, provider, interval, ticker+".gob")
}

func ReadGob(path string, out any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return gob.NewDecoder(file).Decode(out)
}

func WriteGob(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return gob.NewEncoder(file).Encode(value)
}

func mergeRanges(ranges []TimeRange) []TimeRange {
	sorted := slices.SortedFunc(slices.Values(ranges), func(a, b TimeRange) int { return a.From.Compare(b.From) })
	var out []TimeRange
	for _, r := range sorted {
		if n := len(out); n > 0 && !r.From.After(out[n-1].To) {
			out[n-1].To = later(out[n-1].To, r.To)
			continue
		}
		out = append(out, r)
	}
	return out
}

// missing returns the parts of want not covered by the (merged) ranges.
func missing(ranges []TimeRange, want TimeRange) []TimeRange {
	var out []TimeRange
	cursor := want.From
	for _, r := range ranges {
		if r.From.After(cursor) && cursor.Before(want.To) {
			out = append(out, TimeRange{cursor, earlier(r.From, want.To)})
		}
		cursor = later(cursor, r.To)
	}
	if cursor.Before(want.To) {
		out = append(out, TimeRange{cursor, want.To})
	}
	return out
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// coveredUntil stops coverage at the start of today: futures trade nearly around the clock, so
// anything later may hold a still-forming bar.
func coveredUntil(now time.Time) time.Time {
	n := now.In(NY)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, NY)
}

var Now = time.Now

// Load returns bars in [from, to), downloading only the sub-ranges not already cached.
// fetched reports whether any network call was made.
func Load(dir string, p Provider, ticker, interval string, from, to time.Time, refresh bool) (bars []Bar, fetched bool, err error) {
	path := cachePath(dir, p.Name(), interval, ticker)
	var cache cacheFile
	ReadGob(path, &cache)
	gaps := missing(cache.Ranges, TimeRange{from, to})
	if refresh {
		gaps = []TimeRange{{from, to}}
	}
	for _, gap := range gaps {
		fresh, err := p.Fetch(ticker, gap.From, gap.To, interval)
		if err != nil {
			return nil, true, err
		}
		cache.Bars = slices.DeleteFunc(cache.Bars, func(b Bar) bool { return !b.Time.Before(gap.From) && b.Time.Before(gap.To) })
		cache.Bars = SessionOf(ticker).Normalize(append(cache.Bars, fresh...), interval)
	}
	if len(gaps) > 0 {
		if until := earlier(to, coveredUntil(Now())); until.After(from) {
			cache.Ranges = mergeRanges(append(cache.Ranges, TimeRange{from, until}))
		}
		if err := WriteGob(path, cache); err != nil {
			return nil, true, err
		}
	}
	for _, b := range cache.Bars {
		if !b.Time.Before(from) && b.Time.Before(to) {
			bars = append(bars, b)
		}
	}
	return bars, len(gaps) > 0, nil
}

type CachedSeries struct {
	Ticker, Provider, Interval string
	From, To                   time.Time
	Bars                       int
	Size                       int64
}

// ListCached walks the price cache: <dir>/<provider>/<interval>/<ticker>.gob.
func ListCached(dir string) []CachedSeries {
	paths, _ := filepath.Glob(filepath.Join(dir, "*", "*", "*.gob"))
	var out []CachedSeries
	for _, path := range paths {
		var cache cacheFile
		info, err := os.Stat(path)
		if err != nil || ReadGob(path, &cache) != nil || len(cache.Bars) == 0 {
			continue
		}
		parts := strings.Split(filepath.ToSlash(path), "/")
		out = append(out, CachedSeries{
			Ticker: strings.TrimSuffix(parts[len(parts)-1], ".gob"), Interval: parts[len(parts)-2], Provider: parts[len(parts)-3],
			From: cache.Bars[0].Time, To: cache.Bars[len(cache.Bars)-1].Time, Bars: len(cache.Bars), Size: info.Size(),
		})
	}
	return out
}
