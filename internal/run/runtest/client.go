// Package runtest provides a controllable IPC client for approval tests.
package runtest

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"time"

	runpkg "bypass/internal/run"
)

// Connect sends an arbitrated request but leaves its decision under test control.
func Connect(path string) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", path, 3*time.Second)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	metadata, err := json.Marshal(runpkg.Request{Command: []string{"test"}, References: []string{"ref"}, Arbitrate: true})
	if err != nil {
		conn.Close()
		return nil, err
	}
	frame := make([]byte, 12+len(metadata))
	copy(frame, []byte{'B', 'R', 'Q', 1})
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(metadata)))
	copy(frame[8:], metadata)
	if _, err = conn.Write(frame); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// ReadDecision consumes a response without acknowledging it.
func ReadDecision(conn net.Conn) error {
	var header [8]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(header[4:])
	if _, err := io.CopyN(io.Discard, conn, int64(n)); err != nil {
		return err
	}
	var count [4]byte
	if _, err := io.ReadFull(conn, count[:]); err != nil {
		return err
	}
	for i := uint32(0); i < binary.BigEndian.Uint32(count[:]); i++ {
		var lengths [8]byte
		if _, err := io.ReadFull(conn, lengths[:]); err != nil {
			return err
		}
		n := uint64(binary.BigEndian.Uint32(lengths[:4])) + uint64(binary.BigEndian.Uint32(lengths[4:]))
		if _, err := io.CopyN(io.Discard, conn, int64(n)); err != nil {
			return err
		}
	}
	return nil
}
