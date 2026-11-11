package main

import (
	"context"
	"testing"

	runpkg "bypass/internal/run"
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
