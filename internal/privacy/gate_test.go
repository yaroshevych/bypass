package privacy

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConfigureValidateProtectAndUnlock(t *testing.T) {
	gate := New()
	for _, pin := range [][]byte{nil, []byte("123"), []byte("1234567890123"), []byte("12a4"), []byte("１２３４")} {
		if err := gate.Configure(pin); !errors.Is(err, ErrInvalidPIN) {
			t.Fatalf("Configure(%q) = %v, want ErrInvalidPIN", pin, err)
		}
	}
	if gate.Configured() || gate.Protected() {
		t.Fatal("invalid configuration changed gate state")
	}
	if err := gate.Configure([]byte("4826")); err != nil {
		t.Fatal(err)
	}
	if !gate.Configured() || gate.Protected() || !gate.Protect() || !gate.Protected() {
		t.Fatal("configured gate did not protect")
	}
	now := time.Date(2026, 10, 1, 2, 3, 4, 0, time.UTC)
	if err := gate.Unlock([]byte("4826"), now); err != nil {
		t.Fatal(err)
	}
	if gate.Protected() || gate.FailedAttempts() != 0 || !gate.NextAttempt().IsZero() {
		t.Fatal("successful unlock did not clear protection and throttle state")
	}
}

func TestFailedAttemptsBackoffAndThrottleWithoutSleeping(t *testing.T) {
	gate := New()
	if err := gate.Configure([]byte("7531")); err != nil {
		t.Fatal(err)
	}
	gate.Protect()
	now := time.Date(2026, 10, 2, 3, 4, 5, 0, time.UTC)
	delays := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for i, delay := range delays {
		if err := gate.Unlock([]byte("1111"), now); !errors.Is(err, ErrIncorrectPIN) {
			t.Fatalf("failed attempt %d = %v", i+1, err)
		}
		if gate.FailedAttempts() != i+1 || !gate.NextAttempt().Equal(now.Add(delay)) {
			t.Fatalf("attempt %d throttle state: count=%d next=%s", i+1, gate.FailedAttempts(), gate.NextAttempt())
		}
		if err := gate.Unlock([]byte("7531"), now.Add(delay-time.Nanosecond)); !errors.Is(err, ErrThrottled) {
			t.Fatalf("attempt during backoff = %v, want ErrThrottled", err)
		}
		now = now.Add(delay)
	}
	if err := gate.Unlock([]byte("7531"), now); err != nil {
		t.Fatal(err)
	}
	if gate.FailedAttempts() != 0 || gate.Protected() {
		t.Fatal("success did not reset throttle/protection")
	}
}

func TestErrorsDoNotEchoPINAndClearZeroesPrivateBuffers(t *testing.T) {
	gate := New()
	pin := []byte("987654321042")
	if err := gate.Configure(pin); err != nil {
		t.Fatal(err)
	}
	wrong := []byte("0000")
	err := gate.Unlock(wrong, time.Now())
	if !errors.Is(err, ErrIncorrectPIN) || strings.Contains(err.Error(), string(pin)) || strings.Contains(err.Error(), string(wrong)) {
		t.Fatalf("unlock error leaked PIN data: %v", err)
	}
	salt := gate.salt
	gate.Clear()
	if gate.Configured() || gate.Protected() || gate.FailedAttempts() != 0 || !gate.NextAttempt().IsZero() {
		t.Fatal("Clear did not reset gate state")
	}
	if !bytes.Equal(salt, make([]byte, len(salt))) || gate.hash != ([32]byte{}) {
		t.Fatal("Clear retained PIN hash material")
	}
	if err := gate.Unlock(pin, time.Now()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Unlock after Clear = %v", err)
	}
}

func TestGateMethodsConcurrentSafe(t *testing.T) {
	gate := New()
	if err := gate.Configure([]byte("7319")); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				gate.Protect()
				_ = gate.Unlock([]byte("0000"), now.Add(time.Duration(i)*time.Minute))
			} else {
				_ = gate.Configured()
				_ = gate.Protected()
				_ = gate.FailedAttempts()
				_ = gate.NextAttempt()
			}
		}(i)
	}
	wg.Wait()
}
