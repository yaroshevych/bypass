package run

import (
	"bytes"
	"testing"
)

func TestAppendOwnedWipesPreviousAllocation(t *testing.T) {
	previous := []byte("synthetic")
	result := appendOwned(previous, bytes.Repeat([]byte{'x'}, cap(previous)+1)...)
	defer clear(result)
	if !bytes.Equal(previous, make([]byte, len(previous))) {
		t.Fatal("growth retained the old plaintext allocation")
	}
	if !bytes.HasPrefix(result, []byte("synthetic")) {
		t.Fatal("growth lost the existing value")
	}
}
