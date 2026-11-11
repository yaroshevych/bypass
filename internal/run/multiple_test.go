package run

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArbitratedServerWaitsForClientDecision(t *testing.T) {
	client, serverConn := net.Pipe()
	defer client.Close()
	defer serverConn.Close()
	server := &Server{done: make(chan struct{})}
	accepted := make(chan bool, 1)
	go server.serveAuthenticated(serverConn, nil, func(p *Pending) {
		accepted <- p.Reply(Response{Values: map[string][]byte{"ref": []byte("synthetic")}})
	})
	if err := writeRequest(client, Request{Command: []string{"test"}, References: []string{"ref"}, Arbitrate: true}); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := readResponse(client, &response); err != nil {
		t.Fatal(err)
	}
	defer clearResponse(&response)
	select {
	case <-accepted:
		t.Fatal("server accepted before client decision")
	default:
	}
	if _, err := client.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-accepted:
		if !result {
			t.Fatal("selected response was not accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("acknowledgement not delivered")
	}
}

func TestArbitratedServerRejectsDisconnectedClient(t *testing.T) {
	client, serverConn := net.Pipe()
	defer serverConn.Close()
	server := &Server{done: make(chan struct{})}
	accepted := make(chan bool, 1)
	go server.serveAuthenticated(serverConn, nil, func(p *Pending) { accepted <- p.Reply(Response{}) })
	if err := writeRequest(client, Request{Command: []string{"test"}, References: []string{"ref"}, Arbitrate: true}); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := readResponse(client, &response); err != nil {
		t.Fatal(err)
	}
	client.Close()
	select {
	case result := <-accepted:
		if result {
			t.Fatal("disconnected response was accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("disconnect not delivered")
	}
}

func TestArbitratedReplyRequiresAcknowledgement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &Pending{Context: ctx, reply: make(chan Response), ack: make(chan bool, 1)}
	value := []byte("synthetic")
	done := make(chan bool, 1)
	go func() { done <- p.Reply(Response{Values: map[string][]byte{"ref": value}}) }()
	response := <-p.reply
	select {
	case <-done:
		t.Fatal("reply completed before arbitration")
	default:
	}
	p.ack <- true
	if !<-done {
		t.Fatal("acknowledged reply not accepted")
	}
	if !bytes.Equal(response.Values["ref"], []byte("synthetic")) {
		t.Fatal("reply was cleared before delivery")
	}
	clearResponse(&response)
}

func TestArbitratedReplyCancelledAfterTransfer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Pending{Context: ctx, reply: make(chan Response), ack: make(chan bool, 1)}
	done := make(chan bool, 1)
	go func() { done <- p.Reply(Response{Values: map[string][]byte{"ref": []byte("synthetic")}}) }()
	response := <-p.reply
	cancel()
	if <-done {
		t.Fatal("cancelled reply was accepted")
	}
	if string(response.Values["ref"]) != "synthetic" {
		t.Fatal("reply owner lost its buffer")
	}
	clearResponse(&response)
}

func TestExchangeAnyCancelsOtherRequest(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "bypass-multi-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	paths := []string{filepath.Join(dir, "run.sock"), filepath.Join(dir, "widget.sock")}
	arrived := make(chan *Pending, 2)
	for _, path := range paths {
		server, err := Listen(path, func(p *Pending) { arrived <- p })
		if err != nil {
			t.Fatal(err)
		}
		defer server.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		response, err := ExchangeAny(ctx, paths, Request{Command: []string{"test"}, References: []string{"ref"}})
		clearResponse(&response)
		done <- err
	}()
	var first, second *Pending
	select {
	case first = <-arrived:
	case <-ctx.Done():
		t.Fatal("first request missing")
	}
	select {
	case second = <-arrived:
	case <-ctx.Done():
		t.Fatal("second request missing")
	}
	if !first.Reply(Response{Error: "secret request denied"}) {
		t.Fatal("reply rejected")
	}
	select {
	case err := <-done:
		if err == nil || err.Error() != "secret request denied" {
			t.Fatalf("decision: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("decision missing")
	}
	select {
	case <-second.Context.Done():
	case <-ctx.Done():
		t.Fatal("other request not cancelled")
	}
}

func TestExchangeAnyWithUnavailableServer(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "bypass-multi-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "run.sock")
	server, err := Listen(path, func(p *Pending) { p.Reply(Response{Values: map[string][]byte{"ref": []byte("synthetic")}}) })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response, err := ExchangeAny(ctx, []string{filepath.Join(dir, "missing.sock"), path}, Request{Command: []string{"test"}, References: []string{"ref"}})
	if err != nil {
		t.Fatal(err)
	}
	defer clearResponse(&response)
	if string(response.Values["ref"]) != "synthetic" {
		t.Fatal("missing response")
	}
}
