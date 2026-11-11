package main

import (
	"context"
	"fmt"
	"os/exec"
	"sync"

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
		out, err := exec.CommandContext(p.Context, "notify-send", "--app-name=Bypass", "--action=default=Review", "Bypass", "A secret access request needs review").Output()
		if err == nil && string(out) == "default\n" && p.Context.Err() == nil {
			_ = a.send(widgetResponse{Action: "show-request", ID: id})
		}
	}()
	go func() {
		<-p.Context.Done()
		a.mu.Lock()
		delete(a.pending, id)
		a.mu.Unlock()
		_ = a.send(widgetResponse{Action: "request-closed", ID: id})
	}()
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
	defer a.mu.Unlock()
	item := a.pending[id]
	if item == nil || item.pending.Context.Err() != nil {
		return fmt.Errorf("request is no longer available")
	}
	if !approve {
		item.pending.Reply(runpkg.Response{Error: "secret request denied"})
		delete(a.pending, id)
		return nil
	}
	if len(item.targets) == 0 {
		return fmt.Errorf("review the request before approving")
	}
	values, err := a.session.ResolveApproved(ctx, item.targets)
	if err != nil {
		return err
	}
	if !item.pending.Reply(runpkg.Response{Values: values}) {
		return fmt.Errorf("request is no longer available")
	}
	delete(a.pending, id)
	return nil
}
