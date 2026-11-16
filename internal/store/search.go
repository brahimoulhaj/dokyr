package store

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// SearchResult is navigation metadata only. Never load environments or credentials
// into the global search index.
type SearchResult struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Detail      string `json:"detail"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
}

const searchLimit = 8

func searchFold(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return unicode.ToLower(r)
	}, norm.NFD.String(s))
}

// SearchResources returns up to searchLimit matches per kind, and whether any kind
// had more matches than that.
func (s *Store) SearchResources(ctx context.Context, query string) ([]SearchResult, bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT 'project', id, name, description, id, name FROM projects
		UNION ALL SELECT 'app', a.id, a.name, '', p.id, p.name FROM apps a JOIN projects p ON p.id = a.project_id
		UNION ALL SELECT 'database', d.id, d.name, d.engine || ' ' || d.version, p.id, p.name FROM db_instances d JOIN projects p ON p.id = d.project_id
		UNION ALL SELECT 'domain', d.id, d.hostname, '', p.id, p.name FROM domains d JOIN projects p ON p.id = d.project_id
		UNION ALL SELECT 'registry', id, name, server, '', '' FROM registries
		UNION ALL SELECT 'git', id, name, base_url, '', '' FROM git_sources
		UNION ALL SELECT 'storage', id, name, endpoint || ' / ' || bucket, '', '' FROM s3_connections`)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	query = strings.TrimSpace(searchFold(query))
	terms := strings.Fields(query)
	type match struct {
		result SearchResult
		score  int
	}
	matches := []match{}
	for rows.Next() {
		var item SearchResult
		if err := rows.Scan(&item.Kind, &item.ID, &item.Name, &item.Detail, &item.ProjectID, &item.ProjectName); err != nil {
			return nil, false, err
		}
		name := searchFold(item.Name)
		// IDs match by prefix only: hex words like "add" or "cafe" would otherwise
		// hit random UUIDs.
		haystack := searchFold(item.Name + " " + item.Detail + " " + item.ProjectName)
		found := query != "" && strings.HasPrefix(searchFold(item.ID), query)
		if !found {
			found = true
			for _, term := range terms {
				if !strings.Contains(haystack, term) {
					found = false
					break
				}
			}
		}
		if !found {
			continue
		}
		score := 3
		if name == query {
			score = 0
		} else if strings.HasPrefix(name, query) {
			score = 1
		} else if strings.Contains(name, query) {
			score = 2
		}
		matches = append(matches, match{item, score})
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score < matches[j].score
		}
		return searchFold(matches[i].result.Name) < searchFold(matches[j].result.Name)
	})
	// Bound each group so a large project cannot crowd out the other resource types.
	out := []SearchResult{}
	counts := map[string]int{}
	truncated := false
	for _, m := range matches {
		if counts[m.result.Kind] < searchLimit {
			out = append(out, m.result)
			counts[m.result.Kind]++
		} else {
			truncated = true
		}
	}
	return out, truncated, nil
}
