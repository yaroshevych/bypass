package widget

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

// FieldReference produces an absolute Bypass URI for one vault entry field.
// Group paths follow the same root convention as the terminal resolver.
func FieldReference(vaultPath, groupPath, title, field string) (string, error) {
	if !filepath.IsAbs(vaultPath) || filepath.Clean(vaultPath) != vaultPath || title == "" || field == "" {
		return "", errors.New("invalid field reference")
	}
	components := strings.Split(strings.TrimPrefix(vaultPath, "/"), "/")
	groupPath = strings.Trim(groupPath, "/")
	if groupPath == "Passwords" {
		groupPath = ""
	} else {
		groupPath = strings.TrimPrefix(groupPath, "Passwords/")
	}
	if groupPath != "" {
		components = append(components, strings.Split(groupPath, "/")...)
	}
	components = append(components, title)
	for i, component := range components {
		if component == "" || strings.ContainsRune(component, 0) {
			return "", errors.New("invalid field reference")
		}
		components[i] = url.PathEscape(component)
	}
	if strings.ContainsRune(field, 0) {
		return "", errors.New("invalid field reference")
	}
	return "bypass:///" + strings.Join(components, "/") + "#" + url.PathEscape(field), nil
}

// relativeFieldReference matches the concise references shown by the widget.
func relativeFieldReference(groupPath, title, field string) (string, error) {
	if title == "" || field == "" || strings.ContainsRune(title, 0) || strings.ContainsRune(field, 0) {
		return "", errors.New("invalid field reference")
	}
	components := []string{}
	if groupPath != "" {
		components = append(components, strings.Split(groupPath, "/")...)
	}
	components = append(components, title)
	for i, component := range components {
		if component == "" || strings.ContainsRune(component, 0) {
			return "", errors.New("invalid field reference")
		}
		components[i] = url.PathEscape(component)
	}
	return "bypass://" + strings.Join(components, "/") + "#" + url.PathEscape(field), nil
}
