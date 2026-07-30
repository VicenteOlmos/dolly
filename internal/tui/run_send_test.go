package tui

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/dbanalyze"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

const workerChannelCap = 32

const deadlockGuard = 2 * time.Second

func withDeliverDecisionObserver(ctx context.Context) (context.Context, <-chan bool) {
	ch := make(chan bool, 1)
	obs := &deliverDecisionObserver{ch: ch}
	return context.WithValue(ctx, deliverDecisionKey{}, obs), ch
}

func waitDeliverDecision(t *testing.T, decisionCh <-chan bool) bool {
	t.Helper()
	select {
	case delivered := <-decisionCh:
		return delivered
	case <-time.After(deadlockGuard):
		t.Fatal("deliver decision timeout")
		return false
	}
}

type testMsg struct{ id int }

type saturateBarrier struct {
	saturated   chan struct{}
	release     chan struct{}
	blocked     chan struct{}
	runReturned chan struct{}
}

func newSaturateBarrier() *saturateBarrier {
	return &saturateBarrier{
		saturated:   make(chan struct{}),
		release:     make(chan struct{}),
		blocked:     make(chan struct{}),
		runReturned: make(chan struct{}),
	}
}

func (b *saturateBarrier) waitSaturated(t *testing.T) {
	t.Helper()
	select {
	case <-b.saturated:
	case <-time.After(deadlockGuard):
		t.Fatal("saturation timeout")
	}
}

func (b *saturateBarrier) releaseRunner() {
	close(b.release)
}

func (b *saturateBarrier) waitBlocked(t *testing.T) {
	t.Helper()
	select {
	case <-b.blocked:
	case <-time.After(deadlockGuard):
		t.Fatal("blocked wait timeout")
	}
}

func (b *saturateBarrier) waitRunReturned(t *testing.T) {
	t.Helper()
	select {
	case <-b.runReturned:
	case <-time.After(deadlockGuard):
		t.Fatal("run return timeout")
	}
}

func assertChannelFull(t *testing.T, ch <-chan tea.Msg) {
	t.Helper()
	if got := len(ch); got != workerChannelCap {
		t.Fatalf("channel len=%d want=%d saturated", got, workerChannelCap)
	}
}

type barrierDumpRunner struct {
	barrier *saturateBarrier
	err     error
}

func (r barrierDumpRunner) Run(ctx context.Context, _ *sql.DB, _ string, _ DumpDraft, _ []string, _, _ string, onProgress func(dump.ProgressEvent)) error {
	for i := 0; i < workerChannelCap; i++ {
		if onProgress != nil {
			onProgress(dump.ProgressEvent{Phase: "x", Table: fmt.Sprintf("t%d", i)})
		}
	}
	close(r.barrier.saturated)
	close(r.barrier.blocked)
	var err error
	select {
	case <-r.barrier.release:
		err = r.err
	case <-ctx.Done():
		err = ctx.Err()
	}
	close(r.barrier.runReturned)
	return err
}

type barrierRestoreRunner struct {
	barrier *saturateBarrier
	err     error
}

func (r barrierRestoreRunner) Run(ctx context.Context, _ *sql.DB, _ string, _ []string, _ bool, _ string, onProgress func(restore.ProgressEvent)) error {
	for i := 0; i < workerChannelCap; i++ {
		if onProgress != nil {
			onProgress(restore.ProgressEvent{Phase: "x", Table: fmt.Sprintf("t%d", i)})
		}
	}
	close(r.barrier.saturated)
	close(r.barrier.blocked)
	var err error
	select {
	case <-r.barrier.release:
		err = r.err
	case <-ctx.Done():
		err = ctx.Err()
	}
	close(r.barrier.runReturned)
	return err
}

type barrierCloneRunner struct {
	barrier *saturateBarrier
	err     error
}

func (r barrierCloneRunner) Run(ctx context.Context, _ CloneDraft, _ []string, onProgress func(CloneProgressEvent)) error {
	for i := 0; i < workerChannelCap; i++ {
		if onProgress != nil {
			onProgress(CloneProgressEvent{Phase: "x", Step: fmt.Sprintf("step-%d", i)})
		}
	}
	close(r.barrier.saturated)
	close(r.barrier.blocked)
	var err error
	select {
	case <-r.barrier.release:
		err = r.err
	case <-ctx.Done():
		err = ctx.Err()
	}
	close(r.barrier.runReturned)
	return err
}

func TestDeliverResultSendProgress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		setup       func() (context.Context, chan tea.Msg)
		run         func(context.Context, chan tea.Msg) (sent, blocked bool)
		wantSent    bool
		wantNoBlock bool
	}{
		{
			name: "live_deliver",
			setup: func() (context.Context, chan tea.Msg) {
				return context.Background(), make(chan tea.Msg, workerChannelCap)
			},
			run: func(ctx context.Context, ch chan tea.Msg) (bool, bool) {
				return deliverResult(ctx, ch, testMsg{}), false
			},
			wantSent: true,
		},
		{
			name: "cancel_free_buffer_prefer_deliver",
			setup: func() (context.Context, chan tea.Msg) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, make(chan tea.Msg, 1)
			},
			run: func(ctx context.Context, ch chan tea.Msg) (bool, bool) {
				return deliverResult(ctx, ch, testMsg{}), false
			},
			wantSent: true,
		},
		{
			name: "cancel_full_drop_no_block",
			setup: func() (context.Context, chan tea.Msg) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, make(chan tea.Msg)
			},
			run: func(ctx context.Context, ch chan tea.Msg) (bool, bool) {
				return nonBlockingCall(func() { deliverResult(ctx, ch, testMsg{}) })
			},
			wantNoBlock: true,
		},
		{
			name: "live_saturated_evicts_progress",
			setup: func() (context.Context, chan tea.Msg) {
				ch := make(chan tea.Msg, workerChannelCap)
				for i := 0; i < workerChannelCap; i++ {
					ch <- dumpProgressMsg{line: fmt.Sprintf("p%d", i)}
				}
				return context.Background(), ch
			},
			run: func(ctx context.Context, ch chan tea.Msg) (bool, bool) {
				sent := deliverResult(ctx, ch, dumpResultMsg{})
				if !sent {
					return false, false
				}
				gotTerminal := false
				for len(ch) > 0 {
					switch msg := (<-ch).(type) {
					case dumpResultMsg:
						gotTerminal = true
					case dumpProgressMsg:
					default:
						t.Fatalf("unexpected msg %T", msg)
					}
				}
				return gotTerminal, false
			},
			wantSent: true,
		},
		{
			name: "cancel_saturated_drop_returns_false",
			setup: func() (context.Context, chan tea.Msg) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				ch := make(chan tea.Msg, workerChannelCap)
				for i := 0; i < workerChannelCap; i++ {
					ch <- dumpProgressMsg{line: fmt.Sprintf("p%d", i)}
				}
				return ctx, ch
			},
			run: func(ctx context.Context, ch chan tea.Msg) (bool, bool) {
				sent, blocked := nonBlockingCall(func() {
					if deliverResult(ctx, ch, dumpResultMsg{}) {
						t.Fatal("deliverResult returned true on cancelled saturated channel")
					}
				})
				return sent, blocked
			},
			wantNoBlock: true,
		},
		{
			name: "live_saturated_drop_no_block",
			setup: func() (context.Context, chan tea.Msg) {
				ch := make(chan tea.Msg, workerChannelCap)
				for i := 0; i < workerChannelCap; i++ {
					ch <- testMsg{id: i}
				}
				return context.Background(), ch
			},
			run: func(ctx context.Context, ch chan tea.Msg) (bool, bool) {
				return nonBlockingCall(func() { deliverResult(ctx, ch, testMsg{id: 99}) })
			},
			wantNoBlock: true,
		},
		{
			name: "sendProgress_live",
			setup: func() (context.Context, chan tea.Msg) {
				return context.Background(), make(chan tea.Msg, workerChannelCap)
			},
			run: func(ctx context.Context, ch chan tea.Msg) (bool, bool) {
				sendProgress(ctx, ch, testMsg{})
				select {
				case <-ch:
					return true, false
				default:
					return false, false
				}
			},
			wantSent: true,
		},
		{
			name: "sendProgress_cancel_full_drop_no_block",
			setup: func() (context.Context, chan tea.Msg) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, make(chan tea.Msg)
			},
			run: func(ctx context.Context, ch chan tea.Msg) (bool, bool) {
				return nonBlockingCall(func() { sendProgress(ctx, ch, testMsg{}) })
			},
			wantNoBlock: true,
		},
		{
			name: "sendProgress_live_saturated_drop_no_block",
			setup: func() (context.Context, chan tea.Msg) {
				ch := make(chan tea.Msg, workerChannelCap)
				for i := 0; i < workerChannelCap; i++ {
					ch <- testMsg{id: i}
				}
				return context.Background(), ch
			},
			run: func(ctx context.Context, ch chan tea.Msg) (bool, bool) {
				return nonBlockingCall(func() { sendProgress(ctx, ch, testMsg{id: 99}) })
			},
			wantNoBlock: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, ch := tt.setup()
			sent, blocked := tt.run(ctx, ch)
			if tt.wantNoBlock && blocked {
				t.Fatal("blocked, want non-blocking drop")
			}
			if sent != tt.wantSent {
				t.Fatalf("sent=%v want=%v", sent, tt.wantSent)
			}
			if tt.name == "sendProgress_live_saturated_drop_no_block" && len(ch) != workerChannelCap {
				t.Fatalf("channel len=%d want=%d after saturated drop", len(ch), workerChannelCap)
			}
		})
	}
}

func nonBlockingCall(fn func()) (sent, blocked bool) {
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
		return false, false
	case <-time.After(200 * time.Millisecond):
		return false, true
	}
}

func countTerminal(msgs []tea.Msg) int {
	n := 0
	for _, msg := range msgs {
		switch msg.(type) {
		case dumpResultMsg, restoreResultMsg, cloneResultMsg, analyzeResultMsg:
			n++
		}
	}
	return n
}

func drainWithWait(cmd tea.Cmd, ch <-chan tea.Msg, waitFn func(<-chan tea.Msg) tea.Cmd) []tea.Msg {
	var out []tea.Msg
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		out = append(out, msg)
		cmd = waitFn(ch)
	}
	return out
}

func drainDumpWorker(runner DumpRunner, ctx context.Context) []tea.Msg {
	cmd, ch, cancel := startDumpCmd(runner, ctx, nil, "", DumpDraft{}, nil, "", "")
	defer cancel()
	return drainWithWait(cmd, ch, waitDumpCmd)
}

func drainRestoreWorker(runner RestoreRunner, ctx context.Context) []tea.Msg {
	cmd, ch, cancel := startRestoreCmd(runner, ctx, nil, "", nil, false, "")
	defer cancel()
	return drainWithWait(cmd, ch, waitRestoreCmd)
}

func drainCloneWorker(runner CloneRunner, ctx context.Context) []tea.Msg {
	cmd, ch, cancel := startCloneCmd(runner, ctx, CloneDraft{}, nil)
	defer cancel()
	return drainWithWait(cmd, ch, waitCloneCmd)
}

func drainUntilClose(ch <-chan tea.Msg) ([]tea.Msg, error) {
	var out []tea.Msg
	done := make(chan struct{})
	go func() {
		for msg := range ch {
			out = append(out, msg)
		}
		close(done)
	}()
	select {
	case <-done:
		return out, nil
	case <-time.After(deadlockGuard):
		return nil, errors.New("worker stuck")
	}
}

func waitWorkerExit(msgCh <-chan tea.Msg) error {
	_, err := drainUntilClose(msgCh)
	return err
}

func goschedYield() {
	for i := 0; i < 20; i++ {
		runtime.Gosched()
	}
}

func TestExactlyOneTerminalOutcome(t *testing.T) {
	workers := []struct {
		name   string
		live   func() []tea.Msg
		cancel func() []tea.Msg
	}{
		{
			name: "dump",
			live: func() []tea.Msg {
				return drainDumpWorker(mockDumpRunner{}, context.Background())
			},
			cancel: func() []tea.Msg {
				ctx, cancel := context.WithCancel(context.Background())
				cmd, ch, workerCancel := startDumpCmd(mockDumpRunner{blockCtx: true}, ctx, nil, "", DumpDraft{}, nil, "", "")
				defer workerCancel()
				cancel()
				return drainWithWait(cmd, ch, waitDumpCmd)
			},
		},
		{
			name: "restore",
			live: func() []tea.Msg {
				return drainRestoreWorker(mockRestoreRunner{}, context.Background())
			},
			cancel: func() []tea.Msg {
				ctx, cancel := context.WithCancel(context.Background())
				cmd, ch, workerCancel := startRestoreCmd(mockRestoreRunner{blockCtx: true}, ctx, nil, "", nil, false, "")
				defer workerCancel()
				cancel()
				return drainWithWait(cmd, ch, waitRestoreCmd)
			},
		},
		{
			name: "clone",
			live: func() []tea.Msg {
				return drainCloneWorker(mockCloneRunner{}, context.Background())
			},
			cancel: func() []tea.Msg {
				ctx, cancel := context.WithCancel(context.Background())
				cmd, ch, workerCancel := startCloneCmd(mockCloneRunner{blockCtx: true}, ctx, CloneDraft{}, nil)
				defer workerCancel()
				cancel()
				return drainWithWait(cmd, ch, waitCloneCmd)
			},
		},
	}
	for _, tc := range workers {
		tc := tc
		t.Run(tc.name+"_live", func(t *testing.T) {
			t.Parallel()
			if got := countTerminal(tc.live()); got != 1 {
				t.Fatalf("terminal=%d want=1", got)
			}
		})
		t.Run(tc.name+"_cancel", func(t *testing.T) {
			t.Parallel()
			if got := countTerminal(tc.cancel()); got != 1 {
				t.Fatalf("terminal=%d want=1", got)
			}
		})
	}

	t.Run("analyze_live", func(t *testing.T) {
		orig := analyzeSourceFunc
		t.Cleanup(func() { analyzeSourceFunc = orig })
		analyzeSourceFunc = func(ctx context.Context, _ *sql.DB, _, _ string, _ []string) (dbanalyze.AnalyzeResult, error) {
			return dbanalyze.AnalyzeResult{NextCloneName: "db_dolly_1"}, nil
		}
		cmd, cancel := startAnalyzeCmd(nil, "db", "", nil)
		defer cancel()
		if got := countTerminal([]tea.Msg{cmd()}); got != 1 {
			t.Fatalf("terminal=%d want=1", got)
		}
	})
	t.Run("analyze_cancel", func(t *testing.T) {
		orig := analyzeSourceFunc
		t.Cleanup(func() { analyzeSourceFunc = orig })
		analyzeSourceFunc = func(ctx context.Context, _ *sql.DB, _, _ string, _ []string) (dbanalyze.AnalyzeResult, error) {
			<-ctx.Done()
			return dbanalyze.AnalyzeResult{}, ctx.Err()
		}
		cmd, cancel := startAnalyzeCmd(nil, "db", "", nil)
		cancel()
		msg := cmd().(analyzeResultMsg)
		if !errors.Is(msg.err, context.Canceled) || countTerminal([]tea.Msg{msg}) != 1 {
			t.Fatalf("msg=%+v", msg)
		}
	})
}

func TestSaturatedWorkerTerminalOutcome(t *testing.T) {
	type workerStart func(context.Context, *saturateBarrier) (tea.Cmd, <-chan tea.Msg, context.CancelFunc)
	workers := map[string]workerStart{
		"dump": func(ctx context.Context, b *saturateBarrier) (tea.Cmd, <-chan tea.Msg, context.CancelFunc) {
			return startDumpCmd(barrierDumpRunner{barrier: b}, ctx, nil, "", DumpDraft{}, nil, "", "")
		},
		"restore": func(ctx context.Context, b *saturateBarrier) (tea.Cmd, <-chan tea.Msg, context.CancelFunc) {
			return startRestoreCmd(barrierRestoreRunner{barrier: b}, ctx, nil, "", nil, false, "")
		},
		"clone": func(ctx context.Context, b *saturateBarrier) (tea.Cmd, <-chan tea.Msg, context.CancelFunc) {
			return startCloneCmd(barrierCloneRunner{barrier: b}, ctx, CloneDraft{}, nil)
		},
	}

	for name, start := range workers {
		name, start := name, start
		t.Run(name+"_live_full", func(t *testing.T) {
			b := newSaturateBarrier()
			ctx, decisionCh := withDeliverDecisionObserver(context.Background())
			cmd, ch, cancel := start(ctx, b)
			defer cancel()
			_ = cmd
			b.waitSaturated(t)
			b.waitBlocked(t)
			assertChannelFull(t, ch)
			b.releaseRunner()
			b.waitRunReturned(t)
			if got := waitDeliverDecision(t, decisionCh); !got {
				t.Fatal("delivered=false want true on live saturated channel")
			}
			msgs, err := drainUntilClose(ch)
			if err != nil {
				t.Fatal(err)
			}
			if got := countTerminal(msgs); got != 1 {
				t.Fatalf("terminal=%d want=1 msgs=%d", got, len(msgs))
			}
		})
		t.Run(name+"_cancel_full", func(t *testing.T) {
			b := newSaturateBarrier()
			ctx, decisionCh := withDeliverDecisionObserver(context.Background())
			parentCtx, parentCancel := context.WithCancel(ctx)
			cmd, ch, workerCancel := start(parentCtx, b)
			defer workerCancel()
			_ = cmd
			b.waitSaturated(t)
			b.waitBlocked(t)
			assertChannelFull(t, ch)
			parentCancel()
			workerCancel()
			b.waitRunReturned(t)
			if got := waitDeliverDecision(t, decisionCh); got {
				t.Fatal("delivered=true want false on cancelled saturated channel")
			}
			msgs, err := drainUntilClose(ch)
			if err != nil {
				t.Fatal(err)
			}
			if got := countTerminal(msgs); got != 0 {
				t.Fatalf("terminal=%d want=0 (safe drop) msgs=%d", got, len(msgs))
			}
		})
	}
}

func TestChannelWorkerNoGoroutineLeak(t *testing.T) {
	type workerStart struct {
		start func(context.Context, *saturateBarrier) (tea.Cmd, <-chan tea.Msg, context.CancelFunc)
	}
	workers := map[string]workerStart{
		"dump_cancel": {
			start: func(ctx context.Context, _ *saturateBarrier) (tea.Cmd, <-chan tea.Msg, context.CancelFunc) {
				return startDumpCmd(mockDumpRunner{blockCtx: true}, ctx, nil, "", DumpDraft{}, nil, "", "")
			},
		},
		"dump_saturated_cancel": {
			start: func(ctx context.Context, b *saturateBarrier) (tea.Cmd, <-chan tea.Msg, context.CancelFunc) {
				return startDumpCmd(barrierDumpRunner{barrier: b}, ctx, nil, "", DumpDraft{}, nil, "", "")
			},
		},
		"restore_cancel": {
			start: func(ctx context.Context, _ *saturateBarrier) (tea.Cmd, <-chan tea.Msg, context.CancelFunc) {
				return startRestoreCmd(mockRestoreRunner{blockCtx: true}, ctx, nil, "", nil, false, "")
			},
		},
		"restore_saturated_cancel": {
			start: func(ctx context.Context, b *saturateBarrier) (tea.Cmd, <-chan tea.Msg, context.CancelFunc) {
				return startRestoreCmd(barrierRestoreRunner{barrier: b}, ctx, nil, "", nil, false, "")
			},
		},
		"clone_cancel": {
			start: func(ctx context.Context, _ *saturateBarrier) (tea.Cmd, <-chan tea.Msg, context.CancelFunc) {
				return startCloneCmd(mockCloneRunner{blockCtx: true}, ctx, CloneDraft{}, nil)
			},
		},
		"clone_saturated_cancel": {
			start: func(ctx context.Context, b *saturateBarrier) (tea.Cmd, <-chan tea.Msg, context.CancelFunc) {
				return startCloneCmd(barrierCloneRunner{barrier: b}, ctx, CloneDraft{}, nil)
			},
		},
	}

	for name, w := range workers {
		name, w := name, w
		t.Run(name, func(t *testing.T) {
			before := runtime.NumGoroutine()
			b := newSaturateBarrier()
			ctx, parentCancel := context.WithCancel(context.Background())
			cmd, msgCh, workerCancel := w.start(ctx, b)
			_ = cmd
			saturated := name != "dump_cancel" && name != "restore_cancel" && name != "clone_cancel"
			if saturated {
				b.waitSaturated(t)
				b.waitBlocked(t)
				assertChannelFull(t, msgCh)
			}
			parentCancel()
			workerCancel()
			if err := waitWorkerExit(msgCh); err != nil {
				t.Fatal(err)
			}
			goschedYield()
			if delta := runtime.NumGoroutine() - before; delta > 4 {
				t.Fatalf("goroutine delta=%d", delta)
			}
		})
	}

	t.Run("analyze_cancel", func(t *testing.T) {
		before := runtime.NumGoroutine()
		orig := analyzeSourceFunc
		t.Cleanup(func() { analyzeSourceFunc = orig })
		analyzeSourceFunc = func(ctx context.Context, _ *sql.DB, _, _ string, _ []string) (dbanalyze.AnalyzeResult, error) {
			<-ctx.Done()
			return dbanalyze.AnalyzeResult{}, ctx.Err()
		}
		cmd, cancel := startAnalyzeCmd(nil, "db", "", nil)
		cancel()
		_ = cmd()
		goschedYield()
		if delta := runtime.NumGoroutine() - before; delta > 4 {
			t.Fatalf("goroutine delta=%d", delta)
		}
	})
}

func TestDeliverResultConcurrentCancelRace(t *testing.T) {
	t.Parallel()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			ch := make(chan tea.Msg, 1)
			go deliverResult(ctx, ch, testMsg{})
			cancel()
		}()
	}
	wg.Wait()
}
