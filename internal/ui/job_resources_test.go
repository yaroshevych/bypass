package ui

import (
	"context"
	"sync"
	"testing"
)

func TestJobResourcesCancellationBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	secret := []byte("secret")
	j := newJobResources(ctx, func() { clear(secret) })
	cancel()
	if j.start() {
		t.Fatal("canceled command started")
	}
	for _, b := range secret {
		if b != 0 {
			t.Fatal("canceled command retained its input")
		}
	}
}

func TestJobResourcesWaitForRunningCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	cleared := false
	j := newJobResources(ctx, func() { mu.Lock(); cleared = true; mu.Unlock() })
	if !j.start() {
		t.Fatal("command did not start")
	}
	cancel()
	mu.Lock()
	if cleared {
		t.Fatal("input cleared while command was running")
	}
	mu.Unlock()
	j.done()
	mu.Lock()
	defer mu.Unlock()
	if !cleared {
		t.Fatal("command did not release its input")
	}
}

func TestUndeliveredSecretResultClearsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	value := []byte("secret")
	owner := holdSecret(ctx, value)
	cancel()
	<-owner.done
	for _, b := range value {
		if b != 0 {
			t.Fatal("undelivered secret remained in memory")
		}
	}
	if got := takeSecret(owner, value); got != nil {
		t.Fatal("canceled secret result could still be taken")
	}
}
