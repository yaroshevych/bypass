package widget

import (
	"errors"
	"reflect"
	"testing"

	"bypass/internal/vault"
)

func TestSearchFavoritesAndFields(t *testing.T) {
	entries := []vault.Entry{
		{UUID: "netflix", GroupPath: "Passwords/Personal", Tags: []string{"Favorite"}, Fields: []vault.Field{{Name: "Title", Value: "Netflix"}}},
		{UUID: "amazon", GroupPath: "Passwords/Personal", Fields: []vault.Field{{Name: "Title", Value: "Amazon"}, {Name: "UserName", Value: "internetflix@example.com"}, {Name: "Password", Value: "netflix"}}},
		{UUID: "other", GroupPath: "Passwords/Work", Fields: []vault.Field{{Name: "Title", Value: "Other"}, {Name: "Notes", Value: "netflix"}}},
		{UUID: "deleted", Recycled: true, Tags: []string{"Favorite"}, Fields: []vault.Field{{Name: "Title", Value: "Netflix"}}},
	}
	for query, want := range map[string][]string{
		"": {"netflix"}, "*": {"netflix", "amazon", "other"},
		"net": {"netflix", "amazon"}, "netfl": {"netflix", "amazon"},
		"nflx": {"netflix"}, "title:netfl": {"netflix"}, "missing": {},
	} {
		got, err := Search(query, entries)
		if err != nil {
			t.Errorf("query %q: unexpected error: %v", query, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("query %q: got %v, want %v", query, got, want)
		}
	}
}

func TestSearchRejectsInvalidQueries(t *testing.T) {
	for _, query := range []string{"foo:bar", "-", `title:"unfinished`} {
		if got, err := Search(query, nil); !errors.Is(err, ErrInvalidSearch) || len(got) != 0 {
			t.Errorf("query %q: got %v, err %v; want invalid search", query, got, err)
		}
	}
}
