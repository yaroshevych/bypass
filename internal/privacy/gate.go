package privacy

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"sync"
	"time"
)

var (
	ErrInvalidPIN    = errors.New("PIN must contain 4 to 12 digits")
	ErrIncorrectPIN  = errors.New("incorrect PIN")
	ErrThrottled     = errors.New("PIN attempts are temporarily throttled")
	ErrNotConfigured = errors.New("privacy PIN is not configured")
	ErrUnavailable   = errors.New("privacy PIN could not be configured")
)

const saltSize = 32

type Gate struct {
	mu          sync.RWMutex
	salt        []byte
	hash        [sha256.Size]byte
	configured  bool
	protected   bool
	failed      int
	nextAttempt time.Time
}

func New() *Gate { return &Gate{} }

func (g *Gate) Configured() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.configured
}

func (g *Gate) Protected() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.protected
}

func (g *Gate) Configure(pin []byte) error {
	if g == nil {
		return ErrUnavailable
	}
	if !validPIN(pin) {
		return ErrInvalidPIN
	}
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		clear(salt)
		return ErrUnavailable
	}
	hash := pinHash(salt, pin)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.clearLocked()
	g.salt = salt
	g.hash = hash
	clear(hash[:])
	g.configured = true
	return nil
}

func (g *Gate) Protect() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.configured {
		return false
	}
	g.protected = true
	return true
}

func (g *Gate) Unlock(pin []byte, now time.Time) error {
	if g == nil {
		return ErrNotConfigured
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.configured {
		return ErrNotConfigured
	}
	if now.Before(g.nextAttempt) {
		return ErrThrottled
	}
	provided := pinHash(g.salt, pin)
	match := subtle.ConstantTimeCompare(g.hash[:], provided[:])
	clear(provided[:])
	if match != 1 {
		if g.failed < int(^uint(0)>>1) {
			g.failed++
		}
		seconds := 1 << min(g.failed-1, 5)
		if seconds > 30 {
			seconds = 30
		}
		g.nextAttempt = now.Add(time.Duration(seconds) * time.Second)
		return ErrIncorrectPIN
	}
	g.failed = 0
	g.nextAttempt = time.Time{}
	g.protected = false
	return nil
}

func (g *Gate) NextAttempt() time.Time {
	if g == nil {
		return time.Time{}
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.nextAttempt
}

func (g *Gate) FailedAttempts() int {
	if g == nil {
		return 0
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.failed
}

func (g *Gate) Clear() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.clearLocked()
}

func (g *Gate) clearLocked() {
	clear(g.salt)
	g.salt = nil
	clear(g.hash[:])
	g.configured = false
	g.protected = false
	g.failed = 0
	g.nextAttempt = time.Time{}
}

func validPIN(pin []byte) bool {
	if len(pin) < 4 || len(pin) > 12 {
		return false
	}
	for _, digit := range pin {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func pinHash(salt, pin []byte) [sha256.Size]byte {
	material := make([]byte, 0, len(salt)+len(pin))
	material = append(material, salt...)
	material = append(material, pin...)
	hash := sha256.Sum256(material)
	clear(material)
	return hash
}
