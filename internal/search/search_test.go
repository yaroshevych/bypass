package search

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestFindRanksTitleMatchesAheadOfOtherFields(t *testing.T) {
	docs := []Document{
		{ID: "notes", Title: "other", Notes: "alpha"},
		{ID: "username", Title: "other", Username: "alpha"},
		{ID: "subsequence", Title: "a-l-p-h-a"},
		{ID: "word", Title: "my alpha site"},
		{ID: "prefix", Title: "alphabet"},
		{ID: "exact", Title: "alpha"},
	}
	results, err := Find("alpha", docs)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"exact", "prefix", "word", "subsequence", "username", "notes"}
	if got := resultIDs(results); !reflect.DeepEqual(got, want) {
		t.Fatalf("ranked IDs = %v, want %v", got, want)
	}
	for i := 1; i < len(results); i++ {
		if results[i-1].Score < results[i].Score {
			t.Fatalf("scores are not descending: %#v", results)
		}
	}
}

func TestFindTermsAndFieldsAndExclusions(t *testing.T) {
	docs := []Document{
		{ID: "match", Title: "Red key", Username: "alice", Group: "work", Tags: []string{"work"}},
		{ID: "wrong-user", Title: "Red key", Username: "bob"},
		{ID: "excluded", Title: "Red key", Username: "alice", Tags: []string{"archived"}},
		{ID: "wrong-title", Title: "Blue key", Username: "alice"},
	}
	results, err := Find("title:red user:alice -tag:archived", docs)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resultIDs(results), []string{"match"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AND/exclusion IDs = %v, want %v", got, want)
	}
	results, err = Find("username:alice group:work", docs)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resultIDs(results), []string{"match"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped fields IDs = %v, want %v", got, want)
	}
}

func TestFindQuotedPhrasesAndFieldAliases(t *testing.T) {
	docs := []Document{
		{ID: "phrase", Title: "Alpha Beta"},
		{ID: "apart", Title: "Alpha Big Beta"},
		{ID: "tag", Title: "Other", Tags: []string{"Alpha Beta"}},
	}
	for query, want := range map[string][]string{
		"\"alpha beta\"":       {"phrase", "tag"},
		"title:\"alpha beta\"": {"phrase"},
		"tags:\"alpha beta\"":  {"tag"},
		"username:missing":     {},
		"title:other":          {"tag"},
	} {
		results, err := Find(query, docs)
		if err != nil {
			t.Fatalf("Find(%q): %v", query, err)
		}
		if got := resultIDs(results); !reflect.DeepEqual(got, want) {
			t.Errorf("Find(%q) IDs = %v, want %v", query, got, want)
		}
	}
}

func TestEmptyQueryPreservesInputOrder(t *testing.T) {
	docs := []Document{{ID: "one"}, {ID: "two"}, {ID: "three"}}
	for _, query := range []string{"", "   \t\n"} {
		results, err := Find(query, docs)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := resultIDs(results), []string{"one", "two", "three"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("Find(%q) IDs = %v, want %v", query, got, want)
		}
		for _, result := range results {
			if result.Score != 0 {
				t.Fatalf("empty query score = %d, want zero", result.Score)
			}
		}
	}
}

func TestFindKeepsStableTies(t *testing.T) {
	docs := []Document{{ID: "first", Title: "my alpha"}, {ID: "second", Title: "your alpha"}}
	results, err := Find("alpha", docs)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resultIDs(results), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tie order = %v, want %v", got, want)
	}
}

func TestPositionsReturnsRuneOffsetsAndSubsequenceMatches(t *testing.T) {
	if got, want := Positions("title:café", "Title", "🔑 Café login"), []int{2, 3, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Unicode positions = %v, want %v", got, want)
	}
	if got, want := Positions("kps -title:secret", "title", "KeePass"), []int{0, 3, 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("subsequence positions = %v, want %v", got, want)
	}
	if got, want := Positions("title:alpha notes:beta -tag:old", "Title", "alpha beta"), []int{0, 1, 2, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped positions = %v, want %v", got, want)
	}
	if got := Positions("title:alpha", "Notes", "alpha"); len(got) != 0 {
		t.Fatalf("field-scoped notes positions = %v, want none", got)
	}
}

func TestInvalidQueriesReturnGenericError(t *testing.T) {
	for _, query := range []string{"unknown:needle", "password:ignored", "\"unclosed phrase", "title:", "-", "title:\"\""} {
		_, err := Find(query, []Document{{ID: "entry", Title: "needle"}})
		if !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("Find(%q) error = %v, want ErrInvalidQuery", query, err)
		}
		if err != nil && strings.Contains(err.Error(), query) {
			t.Errorf("error echoed query %q: %v", query, err)
		}
	}
}

func TestParserBounds(t *testing.T) {
	_, err := Find(strings.Repeat("x", maxQueryBytes+1), nil)
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("oversized query error = %v", err)
	}
	_, err = Find(strings.Repeat("x ", maxTerms+1), nil)
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("too-many-terms error = %v", err)
	}
}

func resultIDs(results []Result) []string {
	ids := make([]string, len(results))
	for i, result := range results {
		ids[i] = result.ID
	}
	return ids
}
