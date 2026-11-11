package data

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"
)

type Provider interface {
	Name() string
	Fetch(ticker string, from, to time.Time, interval string) ([]Bar, error)
	MaxHistory(interval string) time.Duration // lets us fail early with a helpful message
}

// Providers is the registry; adding a provider is one file plus one line here.
var Providers = map[string]func() Provider{
	"yahoo":   func() Provider { return Yahoo{} },
	"polygon": func() Provider { return Polygon{} },
}

func NewProvider(name string) (Provider, error) {
	constructor, ok := Providers[name]
	if !ok {
		names := slices.Sorted(maps.Keys(Providers))
		return nil, fmt.Errorf("unknown provider %q (available: %s)", name, strings.Join(names, ", "))
	}
	return constructor(), nil
}

const day = 24 * time.Hour

// Rate limits (Polygon's free plan allows 5 calls a minute) are waited out rather than failed.
const rateLimitWait, rateLimitTries = 15 * time.Second, 12

func getJSON(url string, header http.Header, out any) error {
	for try := 1; ; try++ {
		request, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}
		request.Header = header
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode == http.StatusTooManyRequests && try < rateLimitTries {
			fmt.Fprintf(os.Stderr, "note: rate limited by %s, retrying in %s\n", request.URL.Host, rateLimitWait)
			time.Sleep(rateLimitWait)
			continue
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("GET %s: %s: %.300s", strings.Split(url, "?")[0], response.Status, body)
		}
		return json.Unmarshal(body, out)
	}
}
