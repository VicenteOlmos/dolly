package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

type deliverDecisionKey struct{}

type deliverDecisionObserver struct {
	ch chan bool
}

func isProgressMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case dumpProgressMsg, restoreProgressMsg, cloneProgressMsg:
		return true
	default:
		return false
	}
}

func notifyDeliverDecision(ctx context.Context, delivered bool) {
	obs, ok := ctx.Value(deliverDecisionKey{}).(*deliverDecisionObserver)
	if !ok {
		return
	}
	select {
	case obs.ch <- delivered:
	default:
	}
}

// deliverResult delivers msg on ch, preferring delivery when buffer is free.
// On a live saturated channel it evicts one queued progress msg to make room.
// Returns true if delivered, false if safely dropped. Exactly one terminal per
// lifecycle is the caller invariant (runner.Run returns once; analyze cmd once).
func deliverResult(ctx context.Context, ch chan tea.Msg, msg tea.Msg) (delivered bool) {
	defer func() {
		notifyDeliverDecision(ctx, delivered)
	}()

	select {
	case ch <- msg:
		delivered = true
		return
	default:
	}

	if ctx.Err() != nil {
		return
	}

	if !evictOneProgress(ch) {
		return
	}

	select {
	case ch <- msg:
		delivered = true
	default:
	}
	return
}

func evictOneProgress(ch chan tea.Msg) bool {
	select {
	case dropped := <-ch:
		if isProgressMsg(dropped) {
			return true
		}
		select {
		case ch <- dropped:
		default:
		}
		return false
	default:
		return false
	}
}

// sendProgress delivers a non-terminal progress msg or drops it without blocking
// when the channel is full.
func sendProgress(_ context.Context, ch chan tea.Msg, msg tea.Msg) {
	select {
	case ch <- msg:
	default:
	}
}
