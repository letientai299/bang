package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/bang/internal/config"
)

var (
	benchmarkTarget  string
	benchmarkMatched bool
	benchmarkPattern string
)

func BenchmarkResolveDefaultConfig(b *testing.B) {
	c := loadBenchmarkConfig(
		b,
		filepath.Join("..", "..", "deploy", "config.yaml"),
	)

	benchmarks := []struct {
		name  string
		query string
	}{
		{"literal_first", "pr"},
		{"literal_middle", "hn"},
		{"capture", "#123"},
		{"capture_last", "y Go concurrency"},
		{"fallback", "ordinary web search"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkTarget = c.Resolve(bm.query)
			}
		})
	}
}

func BenchmarkResolveAndMatchDefaultConfig(b *testing.B) {
	c := loadBenchmarkConfig(
		b,
		filepath.Join("..", "..", "deploy", "config.yaml"),
	)

	for _, query := range []string{"#123", "ordinary web search"} {
		b.Run(query, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkTarget, benchmarkMatched, benchmarkPattern = c.ResolveMatch(
					query,
				)
			}
		})
	}
}

func BenchmarkResolveRegexRules(b *testing.B) {
	const ruleCount = 32
	var yaml strings.Builder
	yaml.WriteString("fallback: https://example.com/search?q={{q}}\nrules:\n")
	for i := range ruleCount {
		fmt.Fprintf(&yaml, "  - match: 'r%d-(\\d+)'\n", i)
		fmt.Fprintf(&yaml, "    to: 'https://example.com/%d/$1'\n", i)
	}
	c := loadBenchmarkConfigText(b, yaml.String())

	benchmarks := []struct {
		name  string
		query string
	}{
		{"first", "r0-123"},
		{"last", "r31-123"},
		{"fallback", "ordinary web search"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkTarget = c.Resolve(bm.query)
			}
		})
	}
}

func loadBenchmarkConfig(b *testing.B, path string) *config.Config {
	b.Helper()
	c, err := config.Load(path)
	if err != nil {
		b.Fatal(err)
	}
	return c
}

func loadBenchmarkConfigText(b *testing.B, yaml string) *config.Config {
	b.Helper()
	path := filepath.Join(b.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		b.Fatal(err)
	}
	return loadBenchmarkConfig(b, path)
}
