package run

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseReference(t *testing.T) {
	tests := []struct {
		input string
		want  Reference
		bad   bool
	}{
		{"bypass://GitHub", Reference{Components: []string{"GitHub"}, Field: "password"}, false},
		{"bypass:///Users/me/vault/Work/A%2FB#api%23key", Reference{Absolute: true, Components: []string{"Users", "me", "vault", "Work", "A/B"}, Field: "api#key"}, false},
		{"bypass://x#", Reference{}, true}, {"bypass://x//y", Reference{}, true}, {"bypass://x%GG", Reference{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseReference(tt.input)
			if (err != nil) != tt.bad || (!tt.bad && !reflect.DeepEqual(got, tt.want)) {
				t.Fatalf("got %#v, error %v", got, err)
			}
		})
	}
}
func TestSubstitute(t *testing.T) {
	resolve := func(r Reference) (string, error) {
		if r.Components[0] == "bad" {
			return "", errors.New("not found")
		}
		return "secret", nil
	}
	for _, tt := range []struct {
		input, want string
		bad         bool
	}{
		{"bypass://entry", "secret", false}, {"prefix-${bypass://entry}-suffix", "prefix-secret-suffix", false}, {"plain", "plain", false}, {"${bypass://bad}", "", true}, {"${bypass://entry", "", true},
	} {
		got, err := Substitute(tt.input, resolve)
		if (err != nil) != tt.bad || got != tt.want {
			t.Errorf("%q: got %q, %v", tt.input, got, err)
		}
	}
}

func TestSubstituteBytesOwnsResolvedBuffer(t *testing.T) {
	secret := []byte("secret")
	got, err := SubstituteBytes([]byte("x-${bypass://entry}-y"), func(Reference) ([]byte, error) {
		return secret, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	clear(secret)
	if string(got) != "x-secret-y" {
		t.Fatal("substitution did not own its result")
	}
	clear(got)
	if !reflect.DeepEqual(got, make([]byte, len(got))) {
		t.Fatal("owned substitution buffer was not clearable")
	}
}
