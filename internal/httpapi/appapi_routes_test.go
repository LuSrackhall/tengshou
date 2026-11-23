package httpapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"sort"
	"testing"
)

var appRouteRegistration = regexp.MustCompile(`HandleFunc\("(?:GET|POST|PUT|DELETE) (/app/api/v1[^"]*)"`)

// TestAppAPIResourceListingCoversRegisteredRoutes guards against the
// handwritten /app/api/v1 discovery list drifting from the routes registered
// in server.go.
func TestAppAPIResourceListingCoversRegisteredRoutes(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodGet, "/app/api/v1", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	var payload struct {
		Resources []string `json:"resources"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	listed := map[string]bool{}
	for _, resource := range payload.Resources {
		listed[resource] = true
	}

	source, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	registered := map[string]bool{}
	for _, match := range appRouteRegistration.FindAllStringSubmatch(string(source), -1) {
		if match[1] == "/app/api/v1" {
			continue // the discovery endpoint itself
		}
		registered[match[1]] = true
	}
	if len(registered) == 0 {
		t.Fatal("no /app/api/v1 routes found in server.go")
	}

	var missing, stale []string
	for route := range registered {
		if !listed[route] {
			missing = append(missing, route)
		}
	}
	for route := range listed {
		if !registered[route] {
			stale = append(stale, route)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 || len(stale) > 0 {
		t.Fatalf("resource listing drift: missing=%v listed-but-not-registered=%v", missing, stale)
	}
}
