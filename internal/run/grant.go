package run

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
)

const GrantGroup = "Bypass Run Permissions"

type Grant struct {
	Version    int    `json:"version"`
	Entry      string `json:"entry"`
	Name       string `json:"name"`
	Group      string `json:"group"`
	Field      string `json:"field"`
	Executable string `json:"executable"`
	Directory  string `json:"directory,omitempty"`
}

func (g Grant) Encode() string {
	data, _ := json.Marshal(g)
	return "bypass-run-grant-v1:" + base64.RawURLEncoding.EncodeToString(data)
}
func (g Grant) Title() string {
	sum := sha256.Sum256([]byte(g.Encode()))
	scope := filepath.Base(g.Directory)
	if g.Directory == "" {
		scope = "all"
	}
	label := g.Name + " " + g.Field + " " + filepath.Base(g.Executable) + " " + scope
	label = strings.NewReplacer("/", "-", "\\", "-", "\n", " ", "\r", " ").Replace(label)
	runes := []rune(label)
	if len(runes) > 64 {
		label = string(runes[:64])
	}
	return label + " " + hex.EncodeToString(sum[:8])
}
func (g Grant) Allows(want Grant) bool {
	return g.Version == 1 && g.Entry == want.Entry && g.Name == want.Name && g.Group == want.Group && g.Field == want.Field && g.Executable == want.Executable && (g.Directory == "" || g.Directory == want.Directory)
}
func DecodeGrant(value string) (Grant, bool) {
	var g Grant
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "bypass-run-grant-v1:"))
	if err != nil {
		return g, false
	}
	err = json.Unmarshal(data, &g)
	return g, err == nil && g.Version == 1
}
