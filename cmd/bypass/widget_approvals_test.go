package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	runpkg "bypass/internal/run"
	"bypass/internal/run/runtest"
)

func TestWidgetApprovalRejectsCancelledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &widgetApprovals{pending: map[string]*widgetPending{"1": {pending: &runpkg.Pending{Context: ctx}}}}
	if result := a.review(context.Background(), "1"); result.Error == "" {
		t.Fatal("reviewed a cancelled request")
	}
	if err := a.decide(context.Background(), "1", true); err == nil {
		t.Fatal("approved a cancelled request")
	}
	if err := a.decide(context.Background(), "1", false); err == nil {
		t.Fatal("denied a cancelled request")
	}
}

func TestWidgetNotificationWaitsForPendingRequest(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cancel     bool
		remove     bool
		wantNotify bool
	}{
		{name: "closed", cancel: true},
		{name: "decided", remove: true},
		{name: "still pending", wantNotify: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &runpkg.Pending{Context: ctx}
			a := &widgetApprovals{pending: map[string]*widgetPending{"1": {pending: p}}}
			ready := make(chan time.Time, 1)
			called := make(chan struct{}, 1)
			finished := make(chan struct{})
			go func() {
				a.notifyAfter(ctx, "1", ready, func(context.Context) ([]byte, error) {
					called <- struct{}{}
					return nil, nil
				})
				close(finished)
			}()
			select {
			case <-called:
				t.Fatal("notification sent before the grace period")
			default:
			}
			if tc.cancel {
				cancel()
			}
			if tc.remove {
				a.mu.Lock()
				delete(a.pending, "1")
				a.mu.Unlock()
			}
			ready <- time.Now()
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("notification did not finish")
			}
			select {
			case <-called:
				if !tc.wantNotify {
					t.Fatal("notification sent for a closed request")
				}
			default:
				if tc.wantNotify {
					t.Fatal("notification missing for pending request")
				}
			}
		})
	}
}

func TestWidgetStalledClientDoesNotBlockSecondDecision(t *testing.T) {
	dir, err := os.MkdirTemp("", "bw-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "widget.sock")
	pending := make(chan *runpkg.Pending, 2)
	server, err := runpkg.Listen(path, func(p *runpkg.Pending) { pending <- p })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	first, err := runtest.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	p1 := <-pending
	second, err := runtest.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	p2 := <-pending
	a := &widgetApprovals{pending: map[string]*widgetPending{"1": {pending: p1}, "2": {pending: p2}}}
	done1, done2 := make(chan error, 1), make(chan error, 1)
	go func() { done1 <- a.decide(context.Background(), "1", false) }()
	if err := runtest.ReadDecision(first); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done1:
		t.Fatal("first decision finished before acknowledgement")
	default:
	}
	go func() { done2 <- a.decide(context.Background(), "2", false) }()
	if err := runtest.ReadDecision(second); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done2:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("second decision blocked")
	}
	first.Close()
	select {
	case err := <-done1:
		if err == nil {
			t.Fatal("disconnected client accepted decision")
		}
	case <-time.After(time.Second):
		t.Fatal("first decision did not stop")
	}
}

func TestWidgetDecisionDoesNotBlockOtherApprovals(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &runpkg.Pending{Context: ctx}
	a := &widgetApprovals{pending: map[string]*widgetPending{"1": {pending: p}}}
	done := make(chan error, 1)
	go func() { done <- a.decide(context.Background(), "1", false) }()
	removed := make(chan struct{})
	go func() {
		for {
			a.mu.Lock()
			_, pending := a.pending["1"]
			a.mu.Unlock()
			if !pending {
				close(removed)
				return
			}
			if ctx.Err() != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	select {
	case <-removed:
	case <-time.After(time.Second):
		t.Fatal("decision did not release the approval mutex")
	}
	if err := a.decide(context.Background(), "1", false); err == nil {
		t.Fatal("accepted a duplicate decision")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("decision did not stop after cancellation")
	}
}
