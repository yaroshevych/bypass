package run

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	Identity   string `json:"identity,omitempty"`
	Agent      string `json:"agent,omitempty"`
}

// Identity is the non-secret principal derived from a validated run identity.
// Digest is hex SHA-256 of the complete bearer credential, never the credential.
type Identity struct {
	Name   string `json:"-"`
	Digest string `json:"-"`
}

// ParseIdentity accepts NAME-<32 to 64 hex characters>. The final hyphen
// separates a readable name from at least 128 bits of random secret material.
func ParseIdentity(value []byte) (Identity, error) {
	var identity Identity
	if len(value) < 1+1+32 || len(value) > 64+1+64 {
		return identity, errors.New("invalid run identity")
	}
	separator := bytes.LastIndexByte(value, '-')
	if separator < 1 || len(value)-separator-1 < 32 || len(value)-separator-1 > 64 {
		return identity, errors.New("invalid run identity")
	}
	name := value[:separator]
	if len(name) > 64 || name[0] == ' ' || name[len(name)-1] == ' ' {
		return identity, errors.New("invalid run identity")
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == ' ' || c == '_' || c == '.' || c == '-') {
			return identity, errors.New("invalid run identity")
		}
	}
	for _, c := range value[separator+1:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return identity, errors.New("invalid run identity")
		}
	}
	sum := sha256.Sum256(value)
	identity.Name = string(name)
	identity.Digest = hex.EncodeToString(sum[:])
	return identity, nil
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
	if g.Agent != "" {
		label = "agent " + g.Agent + " " + label
	}
	label = strings.NewReplacer("/", "-", "\\", "-", "\n", " ", "\r", " ").Replace(label)
	runes := []rune(label)
	if len(runes) > 64 {
		label = string(runes[:64])
	}
	return label + " " + hex.EncodeToString(sum[:8])
}
func (g Grant) Allows(want Grant) bool {
	return g.Version == 1 && g.Entry == want.Entry && g.Name == want.Name && g.Group == want.Group && g.Field == want.Field && g.Executable == want.Executable && g.Identity == want.Identity && (g.Directory == "" || g.Directory == want.Directory)
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
