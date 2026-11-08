//go:build integration

package evals

import (
	"context"
	"path/filepath"
	"regexp"
	"testing"

	"bypass/internal/vault"
	"bypass/internal/vaulttest"
)

func TestNativeTOTPFieldRevealAndLockTmux(t *testing.T) {
	t.Parallel()
	for _, appearance := range []string{"classic", "compact"} {
		t.Run(appearance, func(t *testing.T) {
			e := newEval(t)
			defer e.close()
			const seed = "JBSWY3DPEHPK3PXP"
			e.registerSecret(seed)
			creds := vault.Credentials{Password: []byte(fixturePassword)}
			direct := vaulttest.NewBackend()
			db, err := direct.Create(context.Background(), e.realDB, creds)
			if err != nil {
				t.Fatal("create TOTP fixture:", err)
			}
			updated, err := direct.Apply(context.Background(), db, creds, vault.Change{Kind: vault.AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Authenticator", "otp": "otpauth://totp/Fixture?secret=" + seed}}, filepath.Join(e.root, "fixture-backups"))
			db.Clear()
			creds.Clear()
			if err != nil {
				t.Fatal("seed TOTP fixture:", err)
			}
			updated.Clear()
			e.writeConfig(map[string]any{"appearance": appearance})
			e.startWith("--read-only", e.realDB)
			e.mustShow("UNLOCK VAULT")
			e.typeText(fixturePassword)
			e.key("Enter")
			e.mustShow("Authenticator", "TOTP")
			e.mustNotShow(seed)
			codePattern := regexp.MustCompile(`\b[0-9]{6}\b`)
			if codePattern.MatchString(e.capture()) {
				t.Fatal("TOTP visible before reveal")
			}
			e.key("Right")
			e.key("Right")
			for i := 0; i < 4; i++ {
				e.key("Down")
			}
			e.key("v")
			e.waitFor("inline TOTP code", func() bool { return codePattern.MatchString(e.capture()) })
			e.registerSecret(codePattern.FindString(e.capture()))
			e.mustNotShow(seed)
			e.key("v")
			e.waitFor("TOTP remasked", func() bool { return !codePattern.MatchString(e.capture()) })
			e.key("v")
			e.waitFor("TOTP revealed again", func() bool { return codePattern.MatchString(e.capture()) })
			lastCode := codePattern.FindString(e.capture())
			e.registerSecret(lastCode)
			e.key("L")
			e.mustShow("Vault locked")
			e.mustNotShow(lastCode)
			e.key("q")
			e.waitForCleanQuit()
		})
	}
}
