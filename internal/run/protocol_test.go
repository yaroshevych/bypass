package run

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type retainedWriter struct {
	frame []byte
	limit int
	err   error
}

type observingReader struct {
	reader  *bytes.Reader
	buffers [][]byte
}

func (r *observingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.buffers = append(r.buffers, p[:n])
	}
	return n, err
}

func (w *retainedWriter) Write(p []byte) (int, error) {
	w.frame = p
	if w.err != nil {
		return 0, w.err
	}
	if w.limit >= 0 && w.limit < len(p) {
		return w.limit, nil
	}
	return len(p), nil
}

func TestIPCExchangeAndExclusiveServer(t *testing.T) {
	tmp, err := os.MkdirTemp("/tmp", "bypass-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	root, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatal(err)
	}
	// macOS Unix socket addresses are short; fixture location must fit.
	if len(root) > 65 {
		t.Fatal("IPC fixture path too long")
	}
	path := filepath.Join(root, "run.sock")
	var mixedErrorValue []byte
	server, err := Listen(path, func(p *Pending) {
		if p.Request.References[0] == "error" {
			mixedErrorValue = []byte("secret")
			p.Reply(Response{Error: "failed", Values: map[string][]byte{"error": mixedErrorValue}})
			return
		}
		p.Reply(Response{Values: map[string][]byte{"ref": []byte("synthetic")}})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if second, err := Listen(path, func(*Pending) {}); err == nil {
		second.Close()
		t.Fatal("second server displaced active server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := Exchange(ctx, path, Request{Command: []string{"test"}, References: []string{"ref"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response.Values["ref"], []byte("synthetic")) {
		t.Fatal("response lost")
	}
	clearResponse(&response)
	if _, err := Exchange(ctx, path, Request{Command: []string{"test"}, References: []string{"error"}}); err == nil {
		t.Fatal("accepted error response")
	}
	if !bytes.Equal(mixedErrorValue, make([]byte, len(mixedErrorValue))) {
		t.Fatal("mixed error response retained secret bytes")
	}
}

func TestPendingReplyClearsResponseAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	response := []byte("secret")
	pending := &Pending{Context: ctx, reply: make(chan Response)}
	cancel()
	pending.Reply(Response{Values: map[string][]byte{"ref": response}})
	if !bytes.Equal(response, make([]byte, len(response))) {
		t.Fatal("cancelled reply retained secret bytes")
	}
}

func TestPendingReplyClearsBlockedAndDuplicateResponses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pending := &Pending{Context: ctx, reply: make(chan Response)}
	first := []byte("first")
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		pending.Reply(Response{Values: map[string][]byte{"ref": first}})
		close(done)
	}()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked reply did not stop after cancellation")
	}
	second := []byte("second")
	pending.Reply(Response{Values: map[string][]byte{"ref": second}})
	if !bytes.Equal(first, make([]byte, len(first))) || !bytes.Equal(second, make([]byte, len(second))) {
		t.Fatal("rejected reply retained secret bytes")
	}
}

func TestResponseFrameRoundTripAndVersionCheck(t *testing.T) {
	original := Response{Values: map[string][]byte{"ref": []byte("secret")}}
	var frame bytes.Buffer
	if err := writeResponse(&frame, original); err != nil {
		t.Fatal(err)
	}
	var decoded Response
	if err := readResponse(&frame, &decoded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Values["ref"], original.Values["ref"]) {
		t.Fatal("response frame lost value")
	}
	clearResponse(&decoded)
	wrongVersion := []byte{'B', 'R', 'S', 2}
	if err := readResponse(bytes.NewReader(wrongVersion), &decoded); err == nil {
		t.Fatal("accepted unsupported response protocol")
	}
}

func TestResponseFrameWipesFrameOnWriteOutcomes(t *testing.T) {
	for name, writer := range map[string]*retainedWriter{
		"complete": {limit: -1},
		"short":    {limit: 1},
		"error":    {limit: -1, err: errors.New("write failed")},
	} {
		t.Run(name, func(t *testing.T) {
			err := writeResponse(writer, Response{Values: map[string][]byte{"ref": []byte("secret")}})
			if name == "complete" && err != nil {
				t.Fatal(err)
			}
			if name == "short" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("expected short write, got %v", err)
			}
			if name == "error" && err == nil {
				t.Fatal("expected writer error")
			}
			if len(writer.frame) == 0 || !bytes.Equal(writer.frame, make([]byte, len(writer.frame))) {
				t.Fatal("temporary response frame was not wiped")
			}
		})
	}
}

func TestResponseFramePartialReadClearsDecodedValues(t *testing.T) {
	var encoded bytes.Buffer
	if err := writeResponse(&encoded, Response{Values: map[string][]byte{"refA": []byte("secret"), "refB": []byte("secret")}}); err != nil {
		t.Fatal(err)
	}
	truncated := encoded.Bytes()[:encoded.Len()-2]
	reader := &observingReader{reader: bytes.NewReader(truncated)}
	var decoded Response
	if err := readResponse(reader, &decoded); err == nil {
		t.Fatal("accepted a truncated secret value")
	}
	if len(decoded.Values) != 0 {
		t.Fatal("partial response retained decoded values")
	}
	for _, buffer := range reader.buffers {
		if bytes.Contains(buffer, []byte("secret")) {
			t.Fatal("temporary partial-read buffer retained secret bytes")
		}
	}
}

func TestRequestFrameRoundTripAndWipe(t *testing.T) {
	identity := []byte("Luna-0123456789abcdef0123456789abcdef")
	for name, writer := range map[string]*retainedWriter{
		"complete": {limit: -1},
		"short":    {limit: 1},
		"error":    {limit: -1, err: errors.New("write failed")},
	} {
		t.Run(name, func(t *testing.T) {
			err := writeRequest(writer, Request{Command: []string{"/bin/tool"}, Directory: "/work", References: []string{"bypass://Secret"}, Identity: identity})
			if name == "complete" && err != nil {
				t.Fatal(err)
			}
			if name == "short" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("expected short write, got %v", err)
			}
			if name == "error" && err == nil {
				t.Fatal("expected writer error")
			}
			if len(writer.frame) == 0 || !bytes.Equal(writer.frame, make([]byte, len(writer.frame))) {
				t.Fatal("temporary request frame was not wiped")
			}
		})
	}
	var frame bytes.Buffer
	if err := writeRequest(&frame, Request{Command: []string{"/bin/tool"}, Directory: "/work", References: []string{"bypass://Secret"}, Identity: identity}); err != nil {
		t.Fatal(err)
	}
	decoded, err := readRequest(&frame)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Command[0] != "/bin/tool" || decoded.Directory != "/work" || decoded.References[0] != "bypass://Secret" || !bytes.Equal(decoded.Identity, identity) {
		t.Fatal("request frame lost fields")
	}
	clear(decoded.Identity)
	if !bytes.Equal(decoded.Identity, make([]byte, len(decoded.Identity))) {
		t.Fatal("decoded identity was not wipeable")
	}
}

func TestRequestMetadataDoesNotContainBearerAndTruncatedReadClearsIt(t *testing.T) {
	identity := []byte("Luna-0123456789abcdef0123456789abcdef")
	var full bytes.Buffer
	if err := writeRequest(&full, Request{Command: []string{"tool"}, References: []string{"bypass://x"}, Identity: identity}); err != nil {
		t.Fatal(err)
	}
	frame := full.Bytes()
	metadataLen := int(binary.BigEndian.Uint32(frame[4:8]))
	metadata := frame[8 : 8+metadataLen]
	if bytes.Contains(metadata, identity) || bytes.Contains(metadata, []byte("Luna")) {
		t.Fatal("request metadata contains bearer or public identity")
	}
	reader := &observingReader{reader: bytes.NewReader(frame[:len(frame)-3])}
	if _, err := readRequest(reader); err == nil {
		t.Fatal("accepted truncated identity frame")
	}
	for _, buffer := range reader.buffers {
		if bytes.Contains(buffer, []byte("Luna")) || bytes.Contains(buffer, identity[len("Luna-"):len(identity)-3]) {
			t.Fatal("truncated request retained bearer bytes")
		}
	}
	if _, err := readRequest(bytes.NewReader([]byte{'B', 'R', 'Q', 2})); err == nil {
		t.Fatal("accepted unsupported request protocol")
	}
}

func TestServerDerivesIdentityAndClearsBearer(t *testing.T) {
	tmp, err := os.MkdirTemp("/tmp", "bypass-id-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	root, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "run.sock")
	got := make(chan *Pending, 1)
	server, err := Listen(path, func(p *Pending) {
		got <- p
		p.Reply(Response{})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	credential := []byte("Luna-0123456789abcdef0123456789abcdef")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Exchange(ctx, path, Request{Command: []string{"tool"}, References: []string{"bypass://x"}, Identity: credential}); err != nil {
		t.Fatal("identity exchange failed")
	}
	var pending *Pending
	select {
	case pending = <-got:
	case <-ctx.Done():
		t.Fatal("server did not submit identity request")
	}
	if pending.Identity.Name != "Luna" || pending.Identity.Digest == "" || len(pending.Request.Identity) != 0 {
		t.Fatal("server did not reduce bearer to its public principal")
	}
	if !bytes.Equal(credential, make([]byte, len(credential))) {
		t.Fatal("client bearer was not wiped after exchange")
	}
}

func TestGrantScopeAndRename(t *testing.T) {
	grant := Grant{Version: 1, Entry: "uuid", Name: "name", Group: "group", Field: "Password", Executable: "/app", Directory: "/project"}
	decoded, ok := DecodeGrant(grant.Encode())
	if !ok || !decoded.Allows(grant) {
		t.Fatal("grant did not round trip")
	}
	for _, change := range []func(*Grant){func(g *Grant) { g.Name = "new" }, func(g *Grant) { g.Group = "other" }, func(g *Grant) { g.Directory = "/other" }, func(g *Grant) { g.Field = "Notes" }, func(g *Grant) { g.Executable = "/other" }, func(g *Grant) { g.Entry = "new" }} {
		want := grant
		change(&want)
		if grant.Allows(want) {
			t.Fatal("grant escaped scope")
		}
	}
	global := grant
	global.Directory = ""
	if !global.Allows(grant) {
		t.Fatal("global grant did not apply")
	}
}

func TestIdentityValidationAndGrantIsolation(t *testing.T) {
	credential := []byte("dev-rw-0123456789abcdef0123456789abcdef")
	identity, err := ParseIdentity(credential)
	if err != nil || identity.Name != "dev-rw" || identity.Digest == "" || strings.Contains(identity.Digest, string(credential)) {
		t.Fatalf("unexpected identity principal: %#v, %v", identity, err)
	}
	for _, invalid := range []string{
		"-0123456789abcdef0123456789abcdef",
		"Luna-0123456789abcdef0123456789abc",
		"Luna-0123456789abcdef0123456789abcg",
		"Luna/secret-0123456789abcdef0123456789abcdef",
	} {
		if _, err := ParseIdentity([]byte(invalid)); err == nil || strings.Contains(err.Error(), invalid) {
			t.Fatalf("invalid identity diagnostic exposed input: %v", err)
		}
	}
	base := Grant{Version: 1, Entry: "entry", Field: "Password", Executable: "/tool", Identity: identity.Digest}
	if !base.Allows(base) {
		t.Fatal("same identity grant did not match")
	}
	other, _ := ParseIdentity([]byte("dev-rw-fedcba9876543210fedcba9876543210"))
	want := base
	want.Identity = other.Digest
	if base.Allows(want) {
		t.Fatal("identity grant crossed to another bearer")
	}
	anonymous := Grant{Version: 1, Entry: base.Entry, Field: base.Field, Executable: base.Executable}
	legacy, ok := DecodeGrant(anonymous.Encode())
	if !ok || !legacy.Allows(anonymous) || legacy.Allows(base) {
		t.Fatal("legacy grant did not remain anonymous-only")
	}
}
