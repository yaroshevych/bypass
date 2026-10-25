//go:build integration

package evals

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

func TestNativeTOTPFieldRevealAndLockTmux(t *testing.T) {
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli required")
	}
	for _, appearance := range []string{"classic", "compact"} {
		t.Run(appearance, func(t *testing.T) {
			e := newEval(t)
			defer e.close()
			const seed = "JBSWY3DPEHPK3PXP"
			e.registerSecret(seed)
			xml := `<?xml version="1.0"?><KeePassFile><Meta><DatabaseName>Fixture</DatabaseName></Meta><Root><Group><UUID>AQEBAQEBAQEBAQEBAQEBAQ==</UUID><Name>Passwords</Name><Entry><UUID>AgICAgICAgICAgICAgICAg==</UUID><String><Key>Title</Key><Value>Authenticator</Value></String><String><Key>otp</Key><Value>otpauth://totp/Fixture?secret=` + seed + `</Value></String></Entry></Group></Root></KeePassFile>`
			xmlPath := filepath.Join(e.root, "totp-fixture.xml")
			if err := os.WriteFile(xmlPath, []byte(xml), 0600); err != nil {
				t.Fatal(err)
			}
			runCLI(e, backend, fixturePassword+"\n"+fixturePassword+"\n", "import", "-q", "--set-password", "--decryption-time", "100", xmlPath, e.realDB)
			e.writeConfig(map[string]any{"appearance": appearance})
			e.startWith(backend, "--read-only", e.realDB)
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
			e.key("C-l")
			e.mustShow("Vault locked")
			if codePattern.MatchString(e.capture()) {
				t.Fatal("TOTP survived full lock")
			}
			e.key("q")
			e.waitForCleanQuit()
		})
	}
}
