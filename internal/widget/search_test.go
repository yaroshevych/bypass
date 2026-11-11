package widget

import (
	"reflect"
	"testing"

	"bypass/internal/vault"
)

func TestSearchFavoritesAndFields(t *testing.T) {
	entries := []vault.Entry{
		{UUID: "a", GroupPath: "Passwords/Work/Cloud", Tags: []string{"Favorite", "production"}, Fields: []vault.Field{{Name: "Title", Value: "Example"}, {Name: "API Key", Value: "synthetic-value"}}},
		{UUID: "b", GroupPath: "Passwords/Personal", Fields: []vault.Field{{Name: "Title", Value: "Other"}, {Name: "Notes", Value: "cloud test"}}},
		{UUID: "c", Recycled: true, Tags: []string{"Favorite"}, Fields: []vault.Field{{Name: "Title", Value: "Example"}}},
	}
	for query, want := range map[string][]string{
		"": {"a"}, "#production": {"a"}, "/Work/Cloud/Example#API": {"a"},
		"synthetic-value": {"a"}, "wrk cld": {"a"}, "cloud": {"a", "b"}, "missing": {},
	} {
		if got := Search(query, entries); !reflect.DeepEqual(got, want) {
			t.Errorf("query %q: got %v, want %v", query, got, want)
		}
	}
}
