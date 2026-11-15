package main

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	runpkg "bypass/internal/run"
	"bypass/internal/widget"
)

type widgetPending struct {
	pending *runpkg.Pending
	targets []widget.Target
}

type widgetApprovals struct {
	mu      sync.Mutex
	next    uint64
	pending map[string]*widgetPending
	session *widget.Session
	send    func(widgetResponse) error
}

func (a *widgetApprovals) submit(p *runpkg.Pending) {
	a.mu.Lock()
	if len(a.pending) >= 32 {
		a.mu.Unlock()
		p.Reply(runpkg.Response{Error: "too many requests"})
		return
	}
	a.next++
	id := fmt.Sprint(a.next)
	a.pending[id] = &widgetPending{pending: p}
	a.mu.Unlock()
	_ = a.send(widgetResponse{Action: "request", ID: id})
	go func() {
		timer := time.NewTimer(500 * time.Millisecond)
		defer timer.Stop()
		a.notifyAfter(p.Context, id, timer.C, func(ctx context.Context) ([]byte, error) {
			return exec.CommandContext(ctx, "notify-send", "--app-name=Bypass", "--action=default=Review", "Bypass", "A secret access request needs review").Output()
		})
	}()
	go func() {
		<-p.Context.Done()
		a.mu.Lock()
		delete(a.pending, id)
		a.mu.Unlock()
		_ = a.send(widgetResponse{Action: "request-closed", ID: id})
	}()
}

func (a *widgetApprovals) notifyAfter(ctx context.Context, id string, ready <-chan time.Time, notify func(context.Context) ([]byte, error)) {
	select {
	case <-ctx.Done():
		return
	case <-ready:
	}
	a.mu.Lock()
	_, pending := a.pending[id]
	a.mu.Unlock()
	if !pending || ctx.Err() != nil {
		return
	}
	out, err := notify(ctx)
	if err == nil && string(out) == "default\n" && ctx.Err() == nil {
		a.mu.Lock()
		_, pending = a.pending[id]
		a.mu.Unlock()
		if pending && ctx.Err() == nil {
			_ = a.send(widgetResponse{Action: "show-request", ID: id})
		}
	}
}

func (a *widgetApprovals) review(ctx context.Context, id string) widgetResponse {
	a.mu.Lock()
	defer a.mu.Unlock()
	response := widgetResponse{Action: "review", ID: id}
	item := a.pending[id]
	if item == nil || item.pending.Context.Err() != nil {
		response.Error = "request is no longer available"
		return response
	}
	targets, err := a.session.Targets(ctx, item.pending.Request.References)
	if err != nil {
		response.Error = err.Error()
		return response
	}
	item.targets = targets
	response.Targets = targets
	response.Command = item.pending.Request.Command
	response.Directory = item.pending.Request.Directory
	response.Agent = item.pending.Identity.Name
	response.Caller = item.pending.Caller
	return response
}

func (a *widgetApprovals) decide(ctx context.Context, id string, approve bool) error {
	a.mu.Lock()
	item := a.pending[id]
	if item == nil || item.pending.Context.Err() != nil {
		a.mu.Unlock()
		return fmt.Errorf("request is no longer available")
	}
	if approve && len(item.targets) == 0 {
		a.mu.Unlock()
		return fmt.Errorf("review the request before approving")
	}
	delete(a.pending, id)
	targets := append([]widget.Target(nil), item.targets...)
	a.mu.Unlock()
	if !approve {
		if !item.pending.Reply(runpkg.Response{Error: "secret request denied"}) {
			return fmt.Errorf("request is no longer available")
		}
		return nil
	}
	values, err := a.session.ResolveApproved(ctx, targets)
	if err != nil {
		item.pending.Reply(runpkg.Response{Error: "could not resolve requested secrets: " + err.Error()})
		return err
	}
	if !item.pending.Reply(runpkg.Response{Values: values}) {
		return fmt.Errorf("request is no longer available")
	}
	return nil
}
