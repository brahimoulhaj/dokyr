package api

import (
	"slices"
	"testing"
)

func TestWatchPaths(t *testing.T) {
	for _, tc := range []struct {
		patterns []string
		files    []string
		want     bool
	}{
		{nil, []string{"README.md"}, true},
		{[]string{"services/api"}, []string{"services/api/main.go"}, true},
		{[]string{"services/api"}, []string{"services/api"}, true},
		{[]string{"services/api"}, []string{"services/api2/main.go"}, false},
		{[]string{"services/api/"}, []string{"services/web/a.ts", "services/api/x/y.go"}, true},
		{[]string{"services/api/"}, []string{"services/api"}, false},
		{[]string{"*.md"}, []string{"README.md"}, true},
		{[]string{"*.md"}, []string{"docs/README.md"}, false},
		{[]string{"**/*.md"}, []string{"docs/a/README.md"}, true},
		{[]string{"**/*.md"}, []string{"README.md"}, true},
		{[]string{"docs/**/*.png"}, []string{"docs/img/deep/a.png"}, true},
		{[]string{"docs/**"}, []string{"docs/a"}, true},
		{[]string{"src/?.go"}, []string{"src/a.go"}, true},
		{[]string{"src/?.go"}, []string{"src/ab.go"}, false},
		{[]string{"a.b"}, []string{"aXb"}, false},
		{[]string{"apps/web", "packages/ui"}, []string{"packages/ui/button.tsx"}, true},
		{[]string{"apps/web", "packages/ui"}, []string{"apps/api/main.go"}, false},
		// Exclusions: alone they deploy everything else; with inclusions they narrow them.
		{[]string{"!docs", "!*.md"}, []string{"docs/a.txt", "README.md"}, false},
		{[]string{"!docs", "!*.md"}, []string{"docs/a.txt", "main.go"}, true},
		{[]string{"src", "!src/**/*_test.go"}, []string{"src/a/b_test.go"}, false},
		{[]string{"src", "!src/**/*_test.go"}, []string{"src/a/b.go"}, true},
		{[]string{"src"}, nil, false},
	} {
		if got := watchMatch(tc.patterns, tc.files); got != tc.want {
			t.Errorf("watchMatch(%v, %v) = %v, want %v", tc.patterns, tc.files, got, tc.want)
		}
	}
	if got := cleanWatchPaths([]string{" ./src/ ", "", "/docs", "!./tmp", "docs", "!"}); !slices.Equal(got, []string{"src/", "docs", "!tmp"}) {
		t.Errorf("cleanWatchPaths = %q", got)
	}
	for _, bad := range [][]string{{"../etc"}, {"a//b"}, {"a/./b"}, {"a\nb"}, {"a\\b"}, make([]string, 51)} {
		if validWatchPaths(bad) {
			t.Errorf("validWatchPaths(%q) accepted", bad)
		}
	}
	if !validWatchPaths([]string{"services/api/**", "!*.md", "docs/"}) {
		t.Error("ordinary patterns refused")
	}
}
