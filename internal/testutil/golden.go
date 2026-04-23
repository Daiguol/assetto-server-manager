// Package testutil has helpers shared by tests. Right now: golden-file
// comparison with a normalizer that smooths over timestamps, uptimes, and
// build/runtime strings so the snapshots survive clock ticks and toolchain
// bumps.
package testutil

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files in testdata/golden")

// CompareGoldenJSON compares got against testdata/golden/<name>.json.
// When -update is passed it rewrites the golden instead of asserting.
//
// Both sides go through normalizeJSON, which zeroes known-volatile fields
// (timestamps, uptime, runtime/OS strings). Anything else is compared
// byte-for-byte after re-indentation so whitespace differences don't
// produce spurious failures.
func CompareGoldenJSON(t *testing.T, name string, got []byte) {
	t.Helper()

	goldenPath := filepath.Join("testdata", "golden", name+".json")

	normalizedGot, err := normalizeJSON(got)
	if err != nil {
		t.Fatalf("normalize got: %v\nraw:\n%s", err, got)
	}

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, normalizedGot, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}

	normalizedWant, err := normalizeJSON(want)
	if err != nil {
		t.Fatalf("normalize golden: %v\nraw:\n%s", err, want)
	}

	if !bytes.Equal(normalizedWant, normalizedGot) {
		t.Errorf("golden mismatch for %s.\nWANT:\n%s\nGOT:\n%s", name, normalizedWant, normalizedGot)
	}
}

// volatileFields are JSON keys (compared case-insensitively) whose values
// change between runs and must be stamped to a sentinel before comparison.
// Case-insensitive because the codebase mixes PascalCase field names on
// raw structs (SessionResults.Date) with snake_case json tags on API
// shapes (apiServerState.now).
var volatileFields = map[string]any{
	"now":        "0001-01-01T00:00:00Z",
	"updated":    "0001-01-01T00:00:00Z",
	"date":       "0001-01-01T00:00:00Z",
	"uptime":     "<uptime>",
	"version":    "<version>",
	"go_version": "<go_version>",
	"os":         "<os>",
}

func normalizeJSON(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	walk(v)
	return json.MarshalIndent(v, "", "  ")
}

func walk(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if sentinel, ok := volatileFields[strings.ToLower(k)]; ok {
				t[k] = sentinel
				continue
			}
			walk(child)
		}
	case []any:
		for _, child := range t {
			walk(child)
		}
	}
}
