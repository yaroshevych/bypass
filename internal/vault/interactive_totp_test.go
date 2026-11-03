package vault

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeTOTPDetailsAndReveal(t *testing.T) {
	if _, err := exec.LookPath("keepassxc-cli"); err != nil {
		t.Skip("native CLI required")
	}
	dir := t.TempDir()
	path, xmlPath := filepath.Join(dir, "totp.kdbx"), filepath.Join(dir, "fixture.xml")
	const seed = "JBSWY3DPEHPK3PXP"
	xml := `<?xml version="1.0"?><KeePassFile><Meta><DatabaseName>Fixture</DatabaseName></Meta><Root><Group><UUID>AQEBAQEBAQEBAQEBAQEBAQ==</UUID><Name>Passwords</Name><Entry><UUID>AgICAgICAgICAgICAgICAg==</UUID><String><Key>Title</Key><Value>Authenticator</Value></String><String><Key>Notes</Key><Value>synthetic notes</Value></String><String><Key>otp</Key><Value>otpauth://totp/Fixture?secret=` + seed + `&amp;period=17</Value></String></Entry><Entry><UUID>AwMDAwMDAwMDAwMDAwMDAw==</UUID><String><Key>Title</Key><Value>No TOTP</Value></String></Entry></Group></Root></KeePassFile>`
	if err := os.WriteFile(xmlPath, []byte(xml), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("keepassxc-cli", "import", "-q", "--set-password", "--decryption-time", "100", xmlPath, path)
	cmd.Stdin = strings.NewReader("fixture-password\nfixture-password\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture import: %v: %s", err, output)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	backend := NewInteractiveCLI("")
	backend.SetLogger(slog.New(slog.NewJSONHandler(&log, nil)))
	db, err := backend.Open(context.Background(), path, Credentials{Password: []byte("fixture-password")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	var configured, absent string
	for _, entry := range db.Entries {
		if entry.Get("Title") == "Authenticator" {
			configured = entry.UUID
		} else {
			absent = entry.UUID
		}
	}
	fields, err := backend.EntryDetails(context.Background(), db, configured)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || fields[1].Name != "TOTP" || !fields[1].Protected || fields[1].Value != "" {
		t.Fatal("details must contain only a masked TOTP placeholder")
	}
	code, err := backend.ResolveField(context.Background(), db, Credentials{}, configured, "TOTP")
	if err != nil || len(code) != 6 || !validTOTPCode(code) {
		t.Fatal("native TOTP unavailable", err)
	}
	if strings.Contains(log.String(), string(code)) || strings.Contains(log.String(), seed) {
		t.Fatal("TOTP data leaked into diagnostics")
	}
	zero(code)
	if _, err := backend.TOTP(context.Background(), db, Credentials{}, absent); !errors.Is(err, ErrUnsupported) {
		t.Fatal("missing TOTP was accepted", err)
	}
	fields, err = backend.EntryDetails(context.Background(), db, absent)
	if err != nil || len(fields) != 1 {
		t.Fatal("ordinary entry details failed", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("viewing changed the vault")
	}
}
