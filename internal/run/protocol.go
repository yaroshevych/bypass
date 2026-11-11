package run

import (
	wipebuf "bypass/internal/secret"
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
	Arbitrate  bool
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
	ack      chan bool
	once     sync.Once
}

func (p *Pending) Reply(r Response) bool {
	if r.Error != "" {
		clearResponse(&r)
	}
	accepted := false
	transferred := false
	p.once.Do(func() {
		if p.Context.Err() != nil {
			clearResponse(&r)
			return
		}
		select {
		case p.reply <- r:
			transferred = true
			if p.ack == nil {
				accepted = true
			} else {
				select {
				case accepted = <-p.ack:
				case <-p.Context.Done():
					select {
					case accepted = <-p.ack:
					default:
					}
				}
			}
		case <-p.Context.Done():
			clearResponse(&r)
		}
	})
	if !transferred {
		clearResponse(&r)
	}
	return accepted
}

func clearResponse(r *Response) {
	for _, value := range r.Values {
		wipebuf.Wipe(value)
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

var ErrServerInUse = errors.New("another Bypass TUI owns the IPC server")

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

// WidgetSocketPath is distinct from the terminal server's socket.
func WidgetSocketPath() (string, error) {
	path, err := SocketPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "widget.sock"), nil
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
	lockName := "server.lock"
	if filepath.Base(path) != "run.sock" {
		lockName = filepath.Base(path) + ".lock"
	}
	fd, err := unix.Open(filepath.Join(dir, lockName), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	lock := os.NewFile(uintptr(fd), "IPC lock")
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrServerInUse
		}
		return nil, err
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
	s.serveAuthenticated(conn, callerAncestry(conn), submit)
}

func (s *Server) serveAuthenticated(conn net.Conn, caller []string, submit func(*Pending)) {
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
		wipebuf.Wipe(request.Identity)
		return
	}
	identity, err := ParseIdentity(request.Identity)
	if err != nil && len(request.Identity) > 0 {
		wipebuf.Wipe(request.Identity)
		return
	}
	wipebuf.Wipe(request.Identity)
	request.Identity = nil
	pending := &Pending{Caller: caller, Request: request, Identity: identity, Context: ctx, reply: make(chan Response)}
	if request.Arbitrate {
		pending.ack = make(chan bool, 1)
	}
	// The request is one frame. Further client input or EOF cancels it.
	clientSignal := make(chan bool, 1)
	go func() {
		var b [1]byte
		n, err := conn.Read(b[:])
		clientSignal <- err == nil && n == 1 && b[0] == 1
		if !request.Arbitrate || err != nil || n != 1 || b[0] != 1 {
			cancel()
		}
	}()
	go submit(pending)
	select {
	case response := <-pending.reply:
		defer clearResponse(&response)
		if ctx.Err() != nil {
			return
		}
		if err := writeResponse(conn, response); err != nil {
			return
		}
		if request.Arbitrate {
			select {
			case accepted := <-clientSignal:
				pending.ack <- accepted
			case <-ctx.Done():
			}
		}
	case <-ctx.Done():
	}
}
func Exchange(ctx context.Context, path string, request Request) (Response, error) {
	response, err, _ := exchange(ctx, path, request)
	return response, err
}

// ExchangeAny delivers one request to each server and accepts the first
// complete response. Transport failures do not override another server's
// pending decision. A rejection is a complete response and wins as well.
func ExchangeAny(ctx context.Context, paths []string, request Request) (Response, error) {
	return exchangeAny(ctx, paths, request, func(ctx context.Context, path string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}, func(conn net.Conn) error {
		unix, ok := conn.(*net.UnixConn)
		if !ok {
			return errors.New("invalid IPC connection")
		}
		return authenticate(unix)
	})
}

func exchangeAny(ctx context.Context, paths []string, request Request, dial func(context.Context, string) (net.Conn, error), verify func(net.Conn) error) (Response, error) {
	defer wipebuf.Wipe(request.Identity)
	if len(paths) == 0 {
		return Response{}, errors.New("Bypass unavailable; open Bypass and unlock the required vault")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		response  Response
		err       error
		complete  bool
		decision  chan bool
		committed chan error
	}
	results := make(chan result, len(paths))
	for _, path := range paths {
		copyRequest := request
		copyRequest.Identity = append([]byte(nil), request.Identity...)
		copyRequest.Arbitrate = true
		go func() {
			defer wipebuf.Wipe(copyRequest.Identity)
			conn, err := dial(ctx, path)
			if err != nil {
				results <- result{err: errors.New("Bypass unavailable; open Bypass and unlock the required vault")}
				return
			}
			defer conn.Close()
			if err := verify(conn); err != nil {
				results <- result{err: err}
				return
			}
			if deadline, ok := ctx.Deadline(); ok {
				_ = conn.SetDeadline(deadline)
			}
			stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
			defer stop()
			if err := writeRequest(conn, copyRequest); err != nil {
				results <- result{err: err}
				return
			}
			var response Response
			if err := readResponse(io.LimitReader(conn, MaxMessage), &response); err != nil {
				clearResponse(&response)
				results <- result{err: errors.New("Bypass request failed or timed out")}
				return
			}
			var responseErr error
			if response.Error != "" {
				responseErr = errors.New(response.Error)
				clearResponse(&response)
			}
			if responseErr != nil && response.Error != "secret request denied" {
				results <- result{err: responseErr}
				return
			}
			decision := make(chan bool, 1)
			committed := make(chan error, 1)
			results <- result{response: response, err: responseErr, complete: true, decision: decision, committed: committed}
			select {
			case selected := <-decision:
				if selected {
					_, err = conn.Write([]byte{1})
					committed <- err
				}
			case <-ctx.Done():
				committed <- ctx.Err()
			}
		}()
	}
	var lastErr error
	for remaining := len(paths); remaining > 0; remaining-- {
		result := <-results
		if result.complete {
			result.decision <- true
			if err := <-result.committed; err != nil {
				clearResponse(&result.response)
				cancel()
				go func(n int) {
					for ; n > 0; n-- {
						other := <-results
						clearResponse(&other.response)
					}
				}(remaining - 1)
				return Response{}, err
			}
			cancel()
			// Drain and wipe any concurrently completed response after the winner.
			go func(n int) {
				for ; n > 0; n-- {
					other := <-results
					clearResponse(&other.response)
				}
			}(remaining - 1)
			return result.response, result.err
		}
		lastErr = result.err
	}
	return Response{}, lastErr
}

func exchange(ctx context.Context, path string, request Request) (Response, error, bool) {
	defer wipebuf.Wipe(request.Identity)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var response Response
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return response, errors.New("Bypass unavailable; open Bypass and unlock the required vault"), false
	}
	defer conn.Close()
	unix, ok := conn.(*net.UnixConn)
	if !ok {
		return response, errors.New("invalid IPC connection"), false
	}
	if err := authenticate(unix); err != nil {
		return response, err, false
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	go func() { <-ctx.Done(); conn.Close() }()
	if err := writeRequest(conn, request); err != nil {
		return response, err, false
	}
	if err := readResponse(io.LimitReader(conn, MaxMessage), &response); err != nil {
		if ctx.Err() != nil {
			return response, fmt.Errorf("approval timed out: %w", ctx.Err()), false
		}
		return response, errors.New("Bypass request failed or timed out"), false
	}
	if err := ctx.Err(); err != nil {
		clearResponse(&response)
		return Response{}, fmt.Errorf("approval timed out: %w", err), false
	}
	if response.Error != "" {
		err := errors.New(response.Error)
		clearResponse(&response)
		return Response{}, err, true
	}
	return response, nil, true
}

var requestMagic = [4]byte{'B', 'R', 'Q', 1}

type requestMetadata struct {
	Command    []string
	Directory  string
	References []string
	Arbitrate  bool `json:",omitempty"`
}

// writeRequest emits one bounded frame and wipes both its metadata encoding
// and the assembled frame when the write finishes.
func writeRequest(w io.Writer, request Request) error {
	metadata, err := json.Marshal(requestMetadata{Command: request.Command, Directory: request.Directory, References: request.References, Arbitrate: request.Arbitrate})
	if err != nil {
		return err
	}
	defer wipebuf.Wipe(metadata)
	if len(metadata) > MaxMessage-12 || len(request.Identity) > MaxMessage-12-len(metadata) {
		return errors.New("request too large")
	}
	frame := make([]byte, 12+len(metadata)+len(request.Identity))
	defer wipebuf.Wipe(frame)
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
	defer wipebuf.Wipe(metadata)
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
	request.Command, request.Directory, request.References, request.Arbitrate = decoded.Command, decoded.Directory, decoded.References, decoded.Arbitrate
	if identityLen > 0 {
		request.Identity = make([]byte, identityLen)
		if _, err := io.ReadFull(r, request.Identity); err != nil {
			wipebuf.Wipe(request.Identity)
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
	defer wipebuf.Wipe(frame)
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
	defer wipebuf.Wipe(frame)
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
			wipebuf.Wipe(entry)
			clearResponse(response)
			return err
		}
		key := string(entry[:keyLen])
		if _, exists := values[key]; exists {
			wipebuf.Wipe(entry)
			clearResponse(response)
			return errors.New("duplicate response key")
		}
		value := append([]byte(nil), entry[keyLen:]...)
		wipebuf.Wipe(entry)
		values[key] = value
	}
	return nil
}
