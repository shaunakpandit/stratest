package data

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
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

func getJSON(url string, header http.Header, out any) error {
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	request.Header = header
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s: %.300s", strings.Split(url, "?")[0], response.Status, body)
	}
	return json.Unmarshal(body, out)
}
