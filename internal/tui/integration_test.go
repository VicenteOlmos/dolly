package tui

import (
	"bytes"
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

func TestAppStartsOnConnectionIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	var in bytes.Buffer
	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	app := NewApp()
	p := tea.NewProgram(
		app,
		tea.WithContext(ctx),
		tea.WithInput(&in),
		tea.WithOutput(&out),
		tea.WithWindowSize(80, 24),
		tea.WithoutSignals(),
	)

	done := make(chan tea.Model, 1)
	go func() {
		m, err := p.Run()
		if err != nil {
			t.Error(err)
			return
		}
		done <- m
	}()

	time.Sleep(100 * time.Millisecond)
	p.Quit()

	select {
	case m := <-done:
		final := m.(*App)
		if final.screen != ScreenConnection {
			t.Fatalf("screen = %v, want connection", final.screen)
		}
	case <-ctx.Done():
		t.Fatal("program timed out")
	}
}

func TestQuitRequiresConfirmationIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	var in bytes.Buffer
	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	app := NewApp()
	p := tea.NewProgram(
		app,
		tea.WithContext(ctx),
		tea.WithInput(&in),
		tea.WithOutput(&out),
		tea.WithWindowSize(80, 24),
		tea.WithoutSignals(),
	)

	done := make(chan tea.Model, 1)
	go func() {
		m, err := p.Run()
		if err != nil {
			t.Error(err)
			return
		}
		done <- m
	}()

	time.Sleep(100 * time.Millisecond)
	in.WriteString("\x1b") // Esc → quit modal
	time.Sleep(50 * time.Millisecond)
	in.WriteString("y")
	time.Sleep(100 * time.Millisecond)

	select {
	case m := <-done:
		_ = m.(*App)
	case <-time.After(500 * time.Millisecond):
		p.Quit()
		<-done
	case <-ctx.Done():
		t.Fatal("program timed out")
	}
}

func TestDeleteProfileFlowIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "h", Port: "5432", Database: "d", User: "u", Password: "p",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)
	cs.refreshProfiles()

	app = drainUpdate(app, keyPress("d", 'd', 0))
	if !app.modalOpen() {
		t.Fatal("expected delete modal")
	}
	app = drainUpdate(app, keyPress("y", 'y', 0))
	list, err := store.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("list after delete = %v err=%v", list, err)
	}
}
