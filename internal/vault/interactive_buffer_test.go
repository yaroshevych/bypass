package vault

import (
	"bufio"
	"bytes"
	"testing"
)

func TestInteractiveReadUntilWipesConsumedReaderStorage(t *testing.T) {
	reader := bufio.NewReader(bytes.NewReader([]byte("synthetic-secret\nENDnext")))
	storage, err := reader.Peek(len("synthetic-secret\nENDnext"))
	if err != nil {
		t.Fatal(err)
	}
	s := &interactiveSession{reader: reader}
	result, err := s.readUntil([]byte("END"), 128)
	defer zero(result)
	if err != nil || !bytes.Equal(result, []byte("synthetic-secret\nEND")) {
		t.Fatal("framed read failed")
	}
	if !bytes.Equal(storage[:len(result)], make([]byte, len(result))) {
		t.Fatal("consumed plaintext remained in reader storage")
	}
	if !bytes.Equal(storage[len(result):], []byte("next")) {
		t.Fatal("unconsumed frame was modified")
	}
}

func TestInteractiveReadUntilAcrossGrowthAndReadBoundaries(t *testing.T) {
	payload := bytes.Repeat([]byte{'s'}, 10000)
	payload = append(payload, []byte("END")...)
	s := &interactiveSession{reader: bufio.NewReaderSize(bytes.NewReader(payload), 32)}
	result, err := s.readUntil([]byte("END"), len(payload))
	defer zero(result)
	if err != nil || !bytes.Equal(result, payload) {
		t.Fatal("large framed read failed")
	}
}
