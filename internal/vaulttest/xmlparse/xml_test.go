package xmlparse

import (
	"encoding/base64"
	"encoding/binary"
	"testing"
	"time"
)

func TestParseKDBXBinaryTime(t *testing.T) {
	want := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, uint64(want.Unix()+62135596800))
	if got := parseTime(base64.StdEncoding.EncodeToString(data)); !got.Equal(want) {
		t.Fatalf("time = %v, want %v", got, want)
	}
}
