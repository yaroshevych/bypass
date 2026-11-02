package run

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
)

const MaxMessage = 1 << 20

type Request struct {
	Command    []string
	Directory  string
	References []string
	// Identity is the raw bearer credential and is only populated on the client
	// side before the controlled request frame is written.
	Identity []byte `json:"-"`
}
type Response struct {
	Values map[string][]byte
	Error  string
}
type Pending struct {
	Caller   []string
	Request  Request
	Identity Identity
	Context  context.Context
	reply    chan Response
	once     sync.Once
}

func (p *Pending) Reply(r Response) {
	accepted := false
	p.once.Do(func() {
		if p.Context.Err() != nil {
			clearResponse(&r)
			return
		}
		select {
		case p.reply <- r:
			accepted = true
		case <-p.Context.Done():
			clearResponse(&r)
		}
	})
	if !accepted {
		clearResponse(&r)
	}
}

func clearResponse(r *Response) {
	for _, value := range r.Values {
		clear(value)
	}
	r.Values = nil
}

type Server struct {
	listener *net.UnixListener
	lock     *os.File
	path     string
	done     chan struct{}
	once     sync.Once
}

func SocketPath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	root := "/tmp"
	if runtime.GOOS == "darwin" {
		root = "/private/tmp"
	}
	sum := sha256.Sum256([]byte(base))
	return filepath.Join(root, fmt.Sprintf("bypass-%d-%x", os.Getuid(), sum[:8]), "run.sock"), nil
}
func Listen(path string, submit func(*Pending)) (*Server, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("invalid IPC directory")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Getuid()) {
		return nil, errors.New("IPC directory owner mismatch")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	// Hold an advisory lock before removing a socket left by a crashed TUI.
	fd, err := unix.Open(filepath.Join(dir, "server.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	lock := os.NewFile(uintptr(fd), "IPC lock")
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("another Bypass TUI owns the IPC server")
	}
	success := false
	defer func() {
		if !success {
			lock.Close()
		}
	}()
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("IPC path is not a socket")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("IPC unavailable: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	s := &Server{listener: listener, lock: lock, path: path, done: make(chan struct{})}
	success = true
	go func() {
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			go s.serve(conn, submit)
		}
	}()
	return s, nil
}
func (s *Server) Close() { s.once.Do(func() { close(s.done); s.listener.Close(); s.lock.Close() }) }
func (s *Server) serve(conn *net.UnixConn, submit func(*Pending)) {
	defer conn.Close()
	if err := authenticate(conn); err != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-s.done:
			cancel()
			conn.Close()
		case <-ctx.Done():
		}
	}()
	request, err := readRequest(conn)
	if err != nil {
		return
	}
	if len(request.Command) == 0 || len(request.References) == 0 {
		clear(request.Identity)
		return
	}
	identity, err := ParseIdentity(request.Identity)
	if err != nil && len(request.Identity) > 0 {
		clear(request.Identity)
		return
	}
	clear(request.Identity)
	request.Identity = nil
	pending := &Pending{Caller: callerAncestry(conn), Request: request, Identity: identity, Context: ctx, reply: make(chan Response)}
	// The request is one frame. Further client input or EOF cancels it.
	go func() { var b [1]byte; conn.Read(b[:]); cancel() }()
	go submit(pending)
	select {
	case response := <-pending.reply:
		defer clearResponse(&response)
		if ctx.Err() != nil {
			return
		}
		_ = writeResponse(conn, response)
	case <-ctx.Done():
	}
}
func Exchange(ctx context.Context, path string, request Request) (Response, error) {
	defer clear(request.Identity)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var response Response
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return response, errors.New("Bypass TUI unavailable; open Bypass and unlock the required vault")
	}
	defer conn.Close()
	unix, ok := conn.(*net.UnixConn)
	if !ok {
		return response, errors.New("invalid IPC connection")
	}
	if err := authenticate(unix); err != nil {
		return response, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	go func() { <-ctx.Done(); conn.Close() }()
	if err := writeRequest(conn, request); err != nil {
		return response, err
	}
	if err := readResponse(io.LimitReader(conn, MaxMessage), &response); err != nil {
		if ctx.Err() != nil {
			return response, fmt.Errorf("approval timed out: %w", ctx.Err())
		}
		return response, errors.New("Bypass request failed or timed out")
	}
	if err := ctx.Err(); err != nil {
		clearResponse(&response)
		return Response{}, fmt.Errorf("approval timed out: %w", err)
	}
	if response.Error != "" {
		err := errors.New(response.Error)
		clearResponse(&response)
		return Response{}, err
	}
	return response, nil
}

var requestMagic = [4]byte{'B', 'R', 'Q', 1}

type requestMetadata struct {
	Command    []string
	Directory  string
	References []string
}

// writeRequest emits one bounded frame and wipes both its metadata encoding
// and the assembled frame when the write finishes.
func writeRequest(w io.Writer, request Request) error {
	metadata, err := json.Marshal(requestMetadata{Command: request.Command, Directory: request.Directory, References: request.References})
	if err != nil {
		return err
	}
	defer clear(metadata)
	if len(metadata) > MaxMessage-12 || len(request.Identity) > MaxMessage-12-len(metadata) {
		return errors.New("request too large")
	}
	frame := make([]byte, 12+len(metadata)+len(request.Identity))
	defer clear(frame)
	copy(frame, requestMagic[:])
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(metadata)))
	copy(frame[8:], metadata)
	offset := 8 + len(metadata)
	binary.BigEndian.PutUint32(frame[offset:offset+4], uint32(len(request.Identity)))
	copy(frame[offset+4:], request.Identity)
	n, err := w.Write(frame)
	if err == nil && n != len(frame) {
		return io.ErrShortWrite
	}
	return err
}

func readRequest(r io.Reader) (Request, error) {
	var request Request
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return request, err
	}
	if string(header[:4]) != string(requestMagic[:]) {
		return request, errors.New("unsupported request protocol")
	}
	metadataLen := int(binary.BigEndian.Uint32(header[4:]))
	if metadataLen > MaxMessage-12 {
		return request, errors.New("request too large")
	}
	metadata := make([]byte, metadataLen)
	defer clear(metadata)
	if _, err := io.ReadFull(r, metadata); err != nil {
		return request, err
	}
	var decoded requestMetadata
	if err := json.Unmarshal(metadata, &decoded); err != nil {
		return request, err
	}
	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return request, err
	}
	identityLen := int(binary.BigEndian.Uint32(length[:]))
	if identityLen > MaxMessage-12-metadataLen {
		return request, errors.New("request too large")
	}
	request.Command, request.Directory, request.References = decoded.Command, decoded.Directory, decoded.References
	if identityLen > 0 {
		request.Identity = make([]byte, identityLen)
		if _, err := io.ReadFull(r, request.Identity); err != nil {
			clear(request.Identity)
			request.Identity = nil
			return Request{}, err
		}
	}
	return request, nil
}

// The response frame keeps secret values out of JSON strings. The versioned,
// bounded frame is: magic, error length + bytes, value count, then key/value
// lengths + bytes. Protocol v1 requires the TUI and client to be upgraded
// together; mixed versions fail closed. Returned values are caller-owned and
// must be cleared.
var responseMagic = [4]byte{'B', 'R', 'S', 1}

func writeResponse(w io.Writer, response Response) error {
	total := 12 + len(response.Error)
	if len(response.Error) > MaxMessage || total > MaxMessage || len(response.Values) > MaxMessage/8 {
		return errors.New("response too large")
	}
	for key, value := range response.Values {
		if len(key) > MaxMessage || len(value) > MaxMessage || len(key)+len(value) > MaxMessage-8 || total > MaxMessage-8-len(key)-len(value) {
			return errors.New("response too large")
		}
		total += 8 + len(key) + len(value)
	}
	frame := make([]byte, total)
	defer clear(frame)
	offset := 0
	copy(frame[offset:], responseMagic[:])
	offset += 4
	binary.BigEndian.PutUint32(frame[offset:], uint32(len(response.Error)))
	offset += 4
	copy(frame[offset:], response.Error)
	offset += len(response.Error)
	binary.BigEndian.PutUint32(frame[offset:], uint32(len(response.Values)))
	offset += 4
	for key, value := range response.Values {
		binary.BigEndian.PutUint32(frame[offset:], uint32(len(key)))
		offset += 4
		binary.BigEndian.PutUint32(frame[offset:], uint32(len(value)))
		offset += 4
		copy(frame[offset:], key)
		offset += len(key)
		copy(frame[offset:], value)
		offset += len(value)
	}
	n, err := w.Write(frame)
	if err == nil && n != len(frame) {
		return io.ErrShortWrite
	}
	return err
}

func readResponse(r io.Reader, response *Response) error {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:4]); err != nil {
		return err
	}
	if string(header[:4]) != string(responseMagic[:]) {
		return errors.New("unsupported response protocol")
	}
	if _, err := io.ReadFull(r, header[4:]); err != nil {
		return err
	}
	errorLen := int(binary.BigEndian.Uint32(header[4:]))
	if errorLen > MaxMessage-12 {
		return errors.New("response too large")
	}
	frame := make([]byte, errorLen+4)
	defer clear(frame)
	if _, err := io.ReadFull(r, frame); err != nil {
		return err
	}
	response.Error = string(frame[:errorLen])
	count := int(binary.BigEndian.Uint32(frame[errorLen:]))
	remaining := MaxMessage - errorLen - 12
	if count > remaining/8 {
		return errors.New("response too large")
	}
	values := make(map[string][]byte, count)
	response.Values = values
	for i := 0; i < count; i++ {
		var lengths [8]byte
		if remaining < len(lengths) {
			clearResponse(response)
			return errors.New("response too large")
		}
		if _, err := io.ReadFull(r, lengths[:]); err != nil {
			clearResponse(response)
			return err
		}
		remaining -= len(lengths)
		keyLen := int(binary.BigEndian.Uint32(lengths[:4]))
		valueLen := int(binary.BigEndian.Uint32(lengths[4:]))
		if keyLen > remaining || valueLen > remaining-keyLen {
			clearResponse(response)
			return errors.New("response too large")
		}
		remaining -= keyLen + valueLen
		entry := make([]byte, keyLen+valueLen)
		if _, err := io.ReadFull(r, entry); err != nil {
			clear(entry)
			clearResponse(response)
			return err
		}
		key := string(entry[:keyLen])
		if _, exists := values[key]; exists {
			clear(entry)
			clearResponse(response)
			return errors.New("duplicate response key")
		}
		value := append([]byte(nil), entry[keyLen:]...)
		clear(entry)
		values[key] = value
	}
	return nil
}
