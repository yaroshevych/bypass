package search

import (
	"errors"
	"sort"
	"strings"
	"unicode"
)

const (
	maxQueryBytes = 4096
	maxTerms      = 64
)

var ErrInvalidQuery = errors.New("invalid search query")

// Document contains only fields that the caller has decided are safe to
// search. Passwords and protected values are intentionally absent.
type Document struct {
	ID       string
	Title    string
	Username string
	URL      string
	Notes    string
	Group    string
	Tags     []string
}

type Result struct {
	ID    string
	Score int
}

type field uint8

const (
	fieldAny field = iota
	fieldTitle
	fieldUsername
	fieldURL
	fieldNotes
	fieldTag
	fieldGroup
)

type term struct {
	value    []rune
	field    field
	negative bool
	phrase   bool
}

// Find parses query and returns matching documents in descending relevance
// order. Equal scores retain the input order.
func Find(query string, docs []Document) ([]Result, error) {
	terms, err := parse(query)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(docs))
	if len(terms) == 0 {
		for _, doc := range docs {
			results = append(results, Result{ID: doc.ID})
		}
		return results, nil
	}
	for _, doc := range docs {
		score, matched := scoreDocument(doc, terms)
		if matched {
			results = append(results, Result{ID: doc.ID, Score: score})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results, nil
}

// Positions returns rune offsets to highlight for positive query terms that
// apply to fieldName. It does not return ANSI escapes or byte offsets.
func Positions(query, fieldName, text string) []int {
	terms, err := parse(query)
	if err != nil {
		return nil
	}
	wanted, ok := parseField(fieldName)
	if !ok {
		return nil
	}
	fieldRunes := lowerRunes([]rune(text))
	positions := make(map[int]struct{})
	for _, t := range terms {
		if t.negative || (t.field != fieldAny && t.field != wanted) {
			continue
		}
		if wanted == fieldAny {
			continue
		}
		queryRunes := lowerRunes(t.value)
		if found := substringPositions(fieldRunes, queryRunes); len(found) > 0 {
			for _, pos := range found {
				positions[pos] = struct{}{}
			}
			continue
		}
		if wanted == fieldTitle && !t.phrase {
			for _, pos := range subsequencePositions(fieldRunes, queryRunes) {
				positions[pos] = struct{}{}
			}
		}
	}
	out := make([]int, 0, len(positions))
	for pos := range positions {
		out = append(out, pos)
	}
	sort.Ints(out)
	return out
}

func parse(query string) ([]term, error) {
	if len(query) > maxQueryBytes {
		return nil, ErrInvalidQuery
	}
	runes := []rune(query)
	terms := make([]term, 0, 8)
	for i := 0; i < len(runes); {
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		if i == len(runes) {
			break
		}
		negative := false
		if runes[i] == '-' {
			negative = true
			i++
			if i == len(runes) || unicode.IsSpace(runes[i]) {
				return nil, ErrInvalidQuery
			}
		}

		f := fieldAny
		if colon := nextQualifierColon(runes, i); colon >= 0 {
			name := string(runes[i:colon])
			parsed, ok := parseField(name)
			if !ok {
				if isURLScheme(runes, i, colon) {
					// A bare URL is a normal unqualified term, not a field selector.
				} else {
					return nil, ErrInvalidQuery
				}
			} else {
				f = parsed
				i = colon + 1
				if i == len(runes) || unicode.IsSpace(runes[i]) {
					return nil, ErrInvalidQuery
				}
			}
		}

		value, phrase, next, ok := parseValue(runes, i)
		if !ok || len(value) == 0 {
			return nil, ErrInvalidQuery
		}
		i = next
		terms = append(terms, term{value: value, field: f, negative: negative, phrase: phrase})
		if len(terms) > maxTerms {
			return nil, ErrInvalidQuery
		}
	}
	return terms, nil
}

func nextQualifierColon(runes []rune, start int) int {
	for i := start; i < len(runes); i++ {
		if unicode.IsSpace(runes[i]) || runes[i] == '"' {
			return -1
		}
		if runes[i] == ':' {
			return i
		}
	}
	return -1
}

func parseValue(runes []rune, start int) ([]rune, bool, int, bool) {
	if start >= len(runes) {
		return nil, false, start, false
	}
	if runes[start] == '"' {
		var value []rune
		for i := start + 1; i < len(runes); i++ {
			switch runes[i] {
			case '"':
				next := i + 1
				if next < len(runes) && !unicode.IsSpace(runes[next]) {
					return nil, false, next, false
				}
				return value, true, next, true
			case '\\':
				if i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\') {
					i++
					value = append(value, runes[i])
				} else {
					value = append(value, runes[i])
				}
			default:
				value = append(value, runes[i])
			}
		}
		return nil, false, len(runes), false
	}
	startValue := start
	for start < len(runes) && !unicode.IsSpace(runes[start]) {
		if runes[start] == '"' {
			return nil, false, start, false
		}
		start++
	}
	return runes[startValue:start], false, start, start > startValue
}

func parseField(name string) (field, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "title":
		return fieldTitle, true
	case "user", "username":
		return fieldUsername, true
	case "url":
		return fieldURL, true
	case "notes":
		return fieldNotes, true
	case "tag", "tags":
		return fieldTag, true
	case "group":
		return fieldGroup, true
	default:
		return fieldAny, false
	}
}

func isURLScheme(runes []rune, start, colon int) bool {
	return colon-start > 0 && colon+2 < len(runes) && runes[colon+1] == '/' && runes[colon+2] == '/'
}

func scoreDocument(doc Document, terms []term) (int, bool) {
	score := 0
	for _, t := range terms {
		termScore, matched := matchTerm(doc, t)
		if t.negative {
			if matched {
				return 0, false
			}
			continue
		}
		if !matched {
			return 0, false
		}
		score += termScore
	}
	if exactTitleQuery(doc.Title, terms) {
		score += 10000
	}
	return score, true
}

func exactTitleQuery(title string, terms []term) bool {
	var values []string
	for _, t := range terms {
		if t.negative {
			continue
		}
		if t.field != fieldAny && t.field != fieldTitle {
			return false
		}
		values = append(values, string(t.value))
	}
	if len(values) == 0 {
		return false
	}
	return equalFoldRunes([]rune(title), []rune(strings.Join(values, " ")))
}

func matchTerm(doc Document, t term) (int, bool) {
	if t.field == fieldAny || t.field == fieldTitle {
		if score, ok := titleScore(doc.Title, t); ok {
			if t.field == fieldTitle || t.field == fieldAny {
				return score, true
			}
		}
	}
	if t.field == fieldTitle {
		return 0, false
	}
	best := 0
	matched := false
	matchText := func(value string, base int) {
		if ok, position := hasSubstring(value, t.value); ok {
			s := base - minInt(position, base/4)
			if !matched || s > best {
				best, matched = s, true
			}
		}
	}
	if t.field == fieldAny || t.field == fieldUsername {
		matchText(doc.Username, 400)
	}
	if t.field == fieldAny || t.field == fieldURL {
		matchText(doc.URL, 400)
	}
	if t.field == fieldAny || t.field == fieldTag {
		for _, tag := range doc.Tags {
			matchText(tag, 400)
		}
	}
	if t.field == fieldAny || t.field == fieldGroup {
		matchText(doc.Group, 400)
	}
	if t.field == fieldAny || t.field == fieldNotes {
		matchText(doc.Notes, 200)
	}
	return best, matched
}

func titleScore(title string, t term) (int, bool) {
	textRunes := lowerRunes([]rune(title))
	queryRunes := lowerRunes(t.value)
	if len(queryRunes) == 0 {
		return 0, false
	}
	if equalRunes(textRunes, queryRunes) {
		return 1000, true
	}
	if hasPrefixRunes(textRunes, queryRunes) {
		return 800, true
	}
	if pos := wordPrefixPosition(textRunes, queryRunes); pos >= 0 {
		return 700 - minInt(pos, 49), true
	}
	if ok, position := hasSubstringRunes(textRunes, queryRunes); ok {
		return 650 - minInt(position, 49), true
	}
	if positions := subsequencePositions(textRunes, queryRunes); len(positions) > 0 && !t.phrase {
		gaps := positions[len(positions)-1] - positions[0] + 1 - len(positions)
		return 600 - minInt(gaps, 100), true
	}
	return 0, false
}

func wordPrefixPosition(text, query []rune) int {
	for i := range text {
		if i > 0 && (unicode.IsLetter(text[i-1]) || unicode.IsDigit(text[i-1])) {
			continue
		}
		if hasPrefixRunes(text[i:], query) {
			return i
		}
	}
	return -1
}

func hasSubstring(text string, query []rune) (bool, int) {
	return hasSubstringRunes(lowerRunes([]rune(text)), lowerRunes(query))
}

func hasSubstringRunes(text, query []rune) (bool, int) {
	if len(query) == 0 || len(query) > len(text) {
		return false, -1
	}
	for i := 0; i <= len(text)-len(query); i++ {
		if equalRunes(text[i:i+len(query)], query) {
			return true, i
		}
	}
	return false, -1
}

func substringPositions(text, query []rune) []int {
	if len(query) == 0 || len(query) > len(text) {
		return nil
	}
	var positions []int
	for i := 0; i <= len(text)-len(query); i++ {
		if equalRunes(text[i:i+len(query)], query) {
			for j := range query {
				positions = append(positions, i+j)
			}
		}
	}
	return positions
}

func subsequencePositions(text, query []rune) []int {
	if len(query) == 0 {
		return nil
	}
	positions := make([]int, 0, len(query))
	needle := 0
	for i, r := range text {
		if r == query[needle] {
			positions = append(positions, i)
			needle++
			if needle == len(query) {
				return positions
			}
		}
	}
	return nil
}

func lowerRunes(runes []rune) []rune {
	lowered := make([]rune, len(runes))
	for i, r := range runes {
		lowered[i] = unicode.ToLower(r)
	}
	return lowered
}

func equalRunes(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalFoldRunes(a, b []rune) bool {
	return equalRunes(lowerRunes(a), lowerRunes(b))
}

func hasPrefixRunes(a, b []rune) bool {
	return len(b) <= len(a) && equalRunes(a[:len(b)], b)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
