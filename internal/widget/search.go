// Package widget contains the Go backend for the Omarchy bar integration.
package widget

import (
	"sort"
	"strings"
	"unicode"

	"bypass/internal/vault"
)

// Search returns entry IDs in relevance order. The caller supplies loaded field
// details and retains ownership of the entries and their values.
func Search(query string, entries []vault.Entry) []string {
	terms := strings.Fields(strings.ToLower(query))
	type match struct {
		id    string
		score int
	}
	var matches []match
	for _, entry := range entries {
		if entry.Recycled {
			continue
		}
		if len(terms) == 0 {
			for _, tag := range entry.Tags {
				if strings.EqualFold(tag, "favorite") {
					matches = append(matches, match{id: entry.UUID})
					break
				}
			}
			continue
		}
		title := entry.Get("Title")
		path := "/" + strings.Trim(strings.TrimPrefix(strings.Trim(entry.GroupPath, "/"), "Passwords/"), "/")
		if path != "/" {
			path += "/"
		}
		path += title
		values := []string{title, entry.GroupPath, path}
		for _, tag := range entry.Tags {
			values = append(values, tag, "#"+tag)
		}
		for _, field := range entry.Fields {
			values = append(values, field.Name, field.Value, path+"#"+field.Name)
		}
		score := 0
		for _, term := range terms {
			best := 0
			for _, value := range values {
				candidate := strings.ToLower(value)
				if strings.Contains(candidate, term) {
					best = max(best, 2)
				} else if subsequence(term, candidate) {
					best = max(best, 1)
				}
			}
			if best == 0 {
				score = -1
				break
			}
			score += best
		}
		if score >= 0 {
			matches = append(matches, match{entry.UUID, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	result := make([]string, len(matches))
	for i := range matches {
		result[i] = matches[i].id
	}
	return result
}

func subsequence(needle, text string) bool {
	query := []rune(needle)
	if len(query) == 0 {
		return true
	}
	i := 0
	for _, r := range text {
		if unicode.ToLower(r) == query[i] {
			i++
			if i == len(query) {
				return true
			}
		}
	}
	return false
}
