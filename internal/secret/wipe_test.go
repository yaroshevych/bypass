package secret

import (
	"bytes"
	"testing"
)

func TestWipeOverwritesByteAndRuneBuffers(t *testing.T) {
	byteValue := []byte("synthetic secret")
	Wipe(byteValue)
	if !bytes.Equal(byteValue, make([]byte, len(byteValue))) {
		t.Fatal("byte buffer retained data after wipe")
	}

	runeValue := []rune("synthetic secret")
	Wipe(runeValue)
	for _, r := range runeValue {
		if r != 0 {
			t.Fatal("rune buffer retained data after wipe")
		}
	}
}
