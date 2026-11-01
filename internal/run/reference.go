// Package run contains the building blocks for approved secret injection.
package run

import (
	"fmt"
	"net/url"
	"strings"
)

// Reference is a parsed secret reference. Components are decoded separately so
// encoded slashes remain part of a name rather than becoming separators.
type Reference struct {
	Absolute   bool
	Components []string
	Field      string
}

// ParseReference parses a standalone bypass URI. Vault identification and
// ambiguity are resolved against open vaults, not by the parser.
func ParseReference(s string) (Reference, error) {
	var r Reference
	if !strings.HasPrefix(s, "bypass://") {
		return r, fmt.Errorf("invalid secret reference")
	}
	rest := strings.TrimPrefix(s, "bypass://")
	path, field, hasField := strings.Cut(rest, "#")
	if strings.Contains(field, "#") || path == "" {
		return r, fmt.Errorf("invalid secret reference")
	}
	r.Absolute = strings.HasPrefix(path, "/")
	if r.Absolute {
		path = strings.TrimPrefix(path, "/")
	}
	if path == "" {
		return r, fmt.Errorf("empty secret path")
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" {
			return Reference{}, fmt.Errorf("empty secret path component")
		}
		decoded, err := url.PathUnescape(part)
		if err != nil || decoded == "" || strings.ContainsRune(decoded, 0) {
			return Reference{}, fmt.Errorf("invalid secret path component")
		}
		r.Components = append(r.Components, decoded)
	}
	r.Field = "password"
	if hasField {
		decoded, err := url.PathUnescape(field)
		if err != nil || decoded == "" || strings.ContainsRune(decoded, 0) {
			return Reference{}, fmt.Errorf("invalid secret field")
		}
		r.Field = decoded
	}
	return r, nil
}

// Substitute replaces standalone or explicitly braced references. It never
// includes reference values in errors.
func Substitute(value string, resolve func(Reference) (string, error)) (string, error) {
	if strings.HasPrefix(value, "bypass://") {
		ref, err := ParseReference(value)
		if err != nil {
			return "", err
		}
		return resolve(ref)
	}
	var out strings.Builder
	for {
		i := strings.Index(value, "${bypass://")
		if i < 0 {
			out.WriteString(value)
			return out.String(), nil
		}
		out.WriteString(value[:i])
		value = value[i+2:]
		end := strings.IndexByte(value, '}')
		if end < 0 {
			return "", fmt.Errorf("unterminated secret reference")
		}
		ref, err := ParseReference(value[:end])
		if err != nil {
			return "", err
		}
		secret, err := resolve(ref)
		if err != nil {
			return "", err
		}
		out.WriteString(secret)
		value = value[end+1:]
	}
}
