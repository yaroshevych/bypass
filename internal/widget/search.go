// Package widget contains the Go backend for the Omarchy bar integration.
package widget

import (
	"errors"
	"strings"

	"bypass/internal/search"
	"bypass/internal/vault"
)

var ErrInvalidSearch = errors.New("Invalid search. Check fields and quotes.")

// Search returns entry IDs in relevance order using the projected list fields.
func Search(query string, entries []vault.Entry) ([]string, error) {
	var docs []search.Document
	result := make([]string, 0)
	for _, entry := range entries {
		if entry.Recycled {
			continue
		}
		if query == "*" {
			result = append(result, entry.UUID)
			continue
		}
		if strings.TrimSpace(query) == "" {
			for _, tag := range entry.Tags {
				if strings.EqualFold(tag, "favorite") {
					result = append(result, entry.UUID)
					break
				}
			}
			continue
		}
		get := func(name string) string {
			if entry.IsProtected(name) {
				return ""
			}
			return entry.Get(name)
		}
		docs = append(docs, search.Document{ID: entry.UUID, Title: get("Title"), Username: get("UserName"), URL: get("URL"), Group: entry.GroupPath, Tags: entry.Tags})
	}
	if query == "*" || strings.TrimSpace(query) == "" {
		return result, nil
	}
	matches, err := search.Find(query, docs)
	if err != nil {
		return nil, ErrInvalidSearch
	}
	for _, match := range matches {
		result = append(result, match.ID)
	}
	return result, nil
}
