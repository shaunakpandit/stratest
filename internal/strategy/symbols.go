package strategy

import "github.com/traefik/yaegi/interp"

// Symbols is filled by the generated stratest-internal-*.go files.
var Symbols = interp.Exports{}

//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract -name strategy stratest/internal/ta
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract -name strategy -include ^Frame$,^Data$ stratest/internal/data

// extract mistakes the dot-less module path for stdlib and pins the files to the current Go version.
//go:generate sed -i "/build go1/d" stratest-internal-ta.go stratest-internal-data.go
//go:generate gofmt -w stratest-internal-ta.go stratest-internal-data.go
