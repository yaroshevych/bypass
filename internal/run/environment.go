package run

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode"
)

// Environment merges KEY=value records in order. Later records replace earlier ones.
func Environment(inherited []string, files ...io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	for _, item := range inherited {
		key, value, ok := strings.Cut(item, "=")
		if ok && key != "" {
			values[key] = value
		}
	}
	for _, file := range files {
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			text := strings.TrimSpace(scanner.Text())
			if text == "" || strings.HasPrefix(text, "#") {
				continue
			}
			key, value, ok := strings.Cut(text, "=")
			key = strings.TrimSpace(key)
			if !ok || !validKey(key) {
				return nil, fmt.Errorf("invalid environment entry at line %d", line)
			}
			value = strings.TrimSpace(value)
			if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") {
				if len(value) < 2 || value[len(value)-1] != value[0] {
					return nil, fmt.Errorf("unterminated environment value at line %d", line)
				}
				value = value[1 : len(value)-1]
			}
			values[key] = value
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read environment file: %w", err)
		}
	}
	return values, nil
}
func validKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if r != '_' && !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') && !(i > 0 && unicode.IsDigit(r) && r < 128) {
			return false
		}
	}
	return true
}
