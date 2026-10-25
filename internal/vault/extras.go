package vault

import (
	"net/url"
	"strconv"
	"strings"
)

const maxAttachmentBytes = 64 << 20

// TOTPResult contains an ephemeral code. Callers own Code and should clear it
// as soon as they finish displaying or copying it.
type TOTPResult struct {
	Code             []byte
	PeriodSeconds    int
	RemainingSeconds int
}

func totpMetadata(fields []Field) (bool, int) {
	values := make(map[string]string, len(fields))
	for _, f := range fields {
		values[strings.ToLower(strings.TrimSpace(f.Name))] = strings.TrimSpace(f.Value)
	}
	secret := ""
	for _, key := range []string{
		"totp seed", "timeotp-secret-base32", "timeotp-secret-hex", "timeotp-secret-base64", "timeotp-secret-utf8",
		"keepass2timeotp-secret-base32", "keepass2timeotp-secret-hex", "keepass2timeotp-secret-base64", "keepass2timeotp-secret-utf8",
	} {
		if values[key] != "" {
			secret = values[key]
			break
		}
	}
	period := 0
	for _, key := range []string{"timeotp-period", "timeotp-step", "keepass2timeotp-step"} {
		if period = parsePositivePeriod(values[key]); period > 0 {
			break
		}
	}
	settings := values["totp settings"]
	if settings != "" {
		if parts := strings.Split(settings, ";"); len(parts) > 0 && period == 0 {
			period = parsePositivePeriod(parts[0])
		}
	}
	otp := values["otp"]
	if otp != "" {
		if parsed, err := url.Parse(otp); err == nil {
			query := parsed.Query()
			if secret == "" {
				secret = query.Get("secret")
				if secret == "" {
					secret = query.Get("key")
				}
			}
			if period == 0 {
				period = parsePositivePeriod(query.Get("period"))
				if period == 0 {
					period = parsePositivePeriod(query.Get("step"))
				}
			}
		}
		if secret == "" && strings.Contains(otp, "=") {
			if query, err := url.ParseQuery(otp); err == nil {
				secret = query.Get("secret")
				if secret == "" {
					secret = query.Get("key")
				}
				if period == 0 {
					period = parsePositivePeriod(query.Get("period"))
					if period == 0 {
						period = parsePositivePeriod(query.Get("step"))
					}
				}
			}
		}
	}
	hasTOTP := secret != "" || otp != ""
	if !hasTOTP {
		return false, 0
	}
	if period == 0 {
		period = 30
	}
	return true, period
}

func parsePositivePeriod(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 || n > 86400 {
		return 0
	}
	return n
}

func validAttachmentName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "\r\n\x00")
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
