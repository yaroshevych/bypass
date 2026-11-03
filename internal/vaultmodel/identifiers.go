package vaultmodel

import (
	"encoding/base64"
	"encoding/hex"
	"sort"
	"strings"
)

func SplitTags(raw string) []string {
	seen := make(map[string]struct{})
	var tags []string
	for _, tag := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\t' }) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

func NormalizeUUID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err == nil && len(b) == 16 {
		h := hex.EncodeToString(b)
		return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	}
	compact := strings.ReplaceAll(strings.ToLower(raw), "-", "")
	if len(compact) != 32 {
		return ""
	}
	decoded, err := hex.DecodeString(compact)
	if err != nil || len(decoded) != 16 {
		return ""
	}
	return compact[:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:]
}
