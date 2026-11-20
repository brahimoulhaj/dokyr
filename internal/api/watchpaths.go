package api

import (
	"regexp"
	"strings"
)

// Watch paths narrow a Git app's deploy-on-push to the pushes that touch its part of a
// repository. A pattern is a path from the repository's root:
//
//	services/api     the file or everything under the folder
//	services/api/    everything under the folder
//	*.md             * and ? stay inside one path segment
//	docs/**/*.png    ** crosses folders
//
// A pattern starting with ! takes paths back out: with only such patterns, every other
// path deploys.

const (
	maxWatchPaths   = 50
	maxWatchPathLen = 200
)

// cleanWatchPaths trims the patterns and drops empty and repeated ones.
func cleanWatchPaths(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, p := range in {
		p = strings.TrimSpace(p)
		not := strings.HasPrefix(p, "!")
		p = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(p, "!"), "./"), "/")
		if p == "" {
			continue
		}
		if not {
			p = "!" + p
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func validWatchPaths(paths []string) bool {
	if len(paths) > maxWatchPaths {
		return false
	}
	for _, p := range paths {
		p = strings.TrimPrefix(p, "!")
		if p == "" || len(p) > maxWatchPathLen || strings.ContainsAny(p, "\x00\r\n\\") {
			return false
		}
		for _, seg := range strings.Split(strings.TrimSuffix(p, "/"), "/") {
			if seg == "" || seg == "." || seg == ".." {
				return false
			}
		}
	}
	return true
}

// watchPattern turns one pattern into a regular expression over a whole path.
func watchPattern(p string) *regexp.Regexp {
	dir := strings.HasSuffix(p, "/")
	p = strings.TrimSuffix(p, "/")
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(p); {
		switch {
		case strings.HasPrefix(p[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 3
		case strings.HasPrefix(p[i:], "**"):
			b.WriteString(".*")
			i += 2
		case p[i] == '*':
			b.WriteString("[^/]*")
			i++
		case p[i] == '?':
			b.WriteString("[^/]")
			i++
		default:
			b.WriteString(regexp.QuoteMeta(p[i : i+1]))
			i++
		}
	}
	if dir {
		b.WriteString("/.*$")
	} else {
		b.WriteString("(?:/.*)?$") // a folder named without the slash takes what is under it
	}
	return regexp.MustCompile(b.String())
}

// watchMatch tells whether any of the changed files is one the patterns watch. No
// pattern watches everything.
func watchMatch(patterns, files []string) bool {
	var include, exclude []*regexp.Regexp
	for _, p := range patterns {
		if rest, not := strings.CutPrefix(p, "!"); not {
			exclude = append(exclude, watchPattern(rest))
		} else {
			include = append(include, watchPattern(p))
		}
	}
	if len(include) == 0 && len(exclude) == 0 {
		return true
	}
	matches := func(res []*regexp.Regexp, f string) bool {
		for _, re := range res {
			if re.MatchString(f) {
				return true
			}
		}
		return false
	}
	for _, f := range files {
		f = strings.TrimPrefix(f, "/")
		if (len(include) == 0 || matches(include, f)) && !matches(exclude, f) {
			return true
		}
	}
	return false
}
