package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"bypass/internal/clipboard"
	"bypass/internal/platform"
	runpkg "bypass/internal/run"
	"bypass/internal/widget"
)

type widgetRequest struct {
	Action   string `json:"action"`
	Path     string `json:"path"`
	Password string `json:"password"`
	Query    string `json:"query"`
	ID       string `json:"id"`
	Field    string `json:"field"`
}

type widgetResponse struct {
	Action    string          `json:"action"`
	Error     string          `json:"error,omitempty"`
	Recent    []string        `json:"recent,omitempty"`
	Items     []widget.Item   `json:"items,omitempty"`
	Fields    []widget.Field  `json:"fields,omitempty"`
	ID        string          `json:"id,omitempty"`
	Targets   []widget.Target `json:"targets,omitempty"`
	Command   []string        `json:"command,omitempty"`
	Directory string          `json:"directory,omitempty"`
	Agent     string          `json:"agent,omitempty"`
	Caller    []string        `json:"caller,omitempty"`
	Field     string          `json:"field,omitempty"`
	Value     string          `json:"value,omitempty"`
	Remaining int             `json:"remaining,omitempty"`
}

// widgetCommand communicates over the private pipes inherited from the shell.
// Passwords and copied values are never command-line arguments.
func widgetCommand(input io.Reader, output io.Writer) error {
	return widgetCommandWithApprovals(input, output, true)
}

func widgetCommandWithApprovals(input io.Reader, output io.Writer, listen bool) error {
	paths, err := platform.Resolve()
	if err != nil {
		return err
	}
	settings, err := platform.LoadSettings(paths)
	if err != nil {
		return err
	}
	store := platform.NewStore(paths)
	session := widget.NewSession(clipboard.NewSystem(), settings.ClipboardTimeout)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer session.Lock(context.Background())
	var outMu sync.Mutex
	var activityMu sync.Mutex
	lastActivity := time.Now()
	unlocked := false
	encoder := json.NewEncoder(output)
	send := func(response widgetResponse) error {
		outMu.Lock()
		defer outMu.Unlock()
		return encoder.Encode(response)
	}
	approvals := &widgetApprovals{pending: make(map[string]*widgetPending), session: session, send: send}
	if socketPath, err := runpkg.WidgetSocketPath(); listen && err == nil {
		server, err := runpkg.Listen(socketPath, approvals.submit)
		if err == nil {
			defer server.Close()
		} else {
			_ = send(widgetResponse{Action: "approval-status", Error: "approval service unavailable"})
		}
	} else if listen {
		_ = send(widgetResponse{Action: "approval-status", Error: "approval service unavailable"})
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				_ = session.ExpireClipboard(ctx, now)
				activityMu.Lock()
				if unlocked && settings.IdleTimeout > 0 && now.Sub(lastActivity) >= settings.IdleTimeout {
					_ = session.Lock(ctx)
					unlocked = false
					_ = send(widgetResponse{Action: "locked"})
				}
				activityMu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var request widgetRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			clear(scanner.Bytes())
			return errors.New("invalid widget request")
		}
		clear(scanner.Bytes())
		response := widgetResponse{Action: request.Action}
		var err error
		activityMu.Lock()
		lastActivity = time.Now()
		switch request.Action {
		case "recent":
			response.Recent, err = store.LoadRecent()
		case "open":
			password := []byte(request.Password)
			request.Password = ""
			err = session.Open(ctx, request.Path, password)
			if err == nil {
				unlocked = true
				_ = store.Remember(request.Path)
			}
		case "items":
			response.Items, err = session.Items(ctx, request.Query)
		case "fields":
			response.Fields, err = session.Fields(ctx, request.ID)
		case "reveal":
			var value []byte
			value, response.Remaining, err = session.Reveal(ctx, request.ID, request.Field)
			if err == nil {
				response.ID = request.ID
				response.Field = request.Field
				response.Value = string(value)
			}
			clear(value)
		case "copy":
			err = session.CopyValue(ctx, request.ID, request.Field)
		case "copy-reference":
			err = session.CopyReference(ctx, request.ID, request.Field)
		case "review":
			response = approvals.review(ctx, request.ID)
		case "approve":
			err = approvals.decide(ctx, request.ID, true)
		case "deny":
			err = approvals.decide(ctx, request.ID, false)
		case "lock":
			err = session.Lock(ctx)
			unlocked = false
		default:
			err = errors.New("unknown widget action")
		}
		activityMu.Unlock()
		if err != nil {
			response.Error = err.Error()
		}
		if err := send(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}
