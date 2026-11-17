package widget

import (
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
		if got := Search(query, entries); !reflect.DeepEqual(got, want) {
			t.Errorf("query %q: got %v, want %v", query, got, want)
		}
	}
}
