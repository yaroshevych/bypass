package run

import (
	"context"
	"crypto/sha256"
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
}
type Response struct {
	Values map[string]string
	Error  string
}
type Pending struct {
	Caller  []string
	Request Request
	Context context.Context
	reply   chan Response
	once    sync.Once
}

func (p *Pending) Reply(r Response) {
	p.once.Do(func() {
		select {
		case p.reply <- r:
		case <-p.Context.Done():
		}
	})
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
	var request Request
	if err := json.NewDecoder(io.LimitReader(conn, MaxMessage)).Decode(&request); err != nil {
		return
	}
	if len(request.Command) == 0 || len(request.References) == 0 {
		return
	}
	pending := &Pending{Caller: callerAncestry(conn), Request: request, Context: ctx, reply: make(chan Response, 1)}
	// The request is one frame. Further client input or EOF cancels it.
	go func() { var b [1]byte; conn.Read(b[:]); cancel() }()
	submit(pending)
	select {
	case response := <-pending.reply:
		json.NewEncoder(conn).Encode(response)
	case <-ctx.Done():
	}
}
func Exchange(ctx context.Context, path string, request Request) (Response, error) {
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
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return response, err
	}
	if err := json.NewDecoder(io.LimitReader(conn, MaxMessage)).Decode(&response); err != nil {
		if ctx.Err() != nil {
			return response, fmt.Errorf("approval timed out: %w", ctx.Err())
		}
		return response, errors.New("Bypass request failed or timed out")
	}
	if response.Error != "" {
		return response, errors.New(response.Error)
	}
	return response, nil
}
