package ui

import (
	"context"
	"sync"
)

// jobResources clears inputs of a command that is canceled before it starts.
// Once started, the command owns them until it calls done; cancellation must
// not clear buffers concurrently with a backend using them.
type jobResources struct {
	mu               sync.Mutex
	ctx              context.Context
	started, cleared bool
	clear            func()
	stop             func() bool
}

func newJobResources(ctx context.Context, clear func()) *jobResources {
	j := &jobResources{ctx: ctx, clear: clear}
	j.stop = context.AfterFunc(ctx, j.abandon)
	return j
}

func (j *jobResources) abandon() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.started {
		j.clearLocked()
	}
}

func (j *jobResources) start() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cleared || j.ctx.Err() != nil {
		j.clearLocked()
		return false
	}
	j.started = true
	j.stop()
	return true
}

func (j *jobResources) done() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.clearLocked()
}

func (j *jobResources) clearLocked() {
	if !j.cleared {
		j.cleared = true
		j.clear()
		j.clear = nil
	}
}
