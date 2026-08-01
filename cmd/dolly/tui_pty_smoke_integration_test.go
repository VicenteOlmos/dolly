//go:build integration && unix

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

const envTUIPTYSmoke = "DOLLY_TUI_PTY_SMOKE"

func TestTUIPTYSmoke(t *testing.T) {
	tui := startTUISmoke(t)

	output := tui.WaitFor("Connection", 10*time.Second)
	if !strings.Contains(output, "dolly") {
		t.Fatalf("output missing brand; got:\n%s", output)
	}

	tui.Quit()
}

func TestTUIPTYHelpSmoke(t *testing.T) {
	tui := startTUISmoke(t)

	tui.WaitFor("Connection", 10*time.Second)
	tui.SendAndWait("\x1bOP", "Keyboard Help", 5*time.Second) // F1 works from inside fields.
	tui.SendAndWait("\x1b", "Host:", 5*time.Second)

	tui.Quit()
}

func TestTUIPTYScreenNavigationSmoke(t *testing.T) {
	tui := startTUISmoke(t)

	tui.WaitFor("Connection", 10*time.Second)
	tui.SendAndWait("\x1b", "Connection fields", 5*time.Second) // leave fields so 1-5 are global screen jumps.
	for _, tc := range []struct {
		key  string
		want string
	}{
		{key: "2", want: "Schema"},
		{key: "3", want: "Dump"},
		{key: "4", want: "Clone"},
		{key: "5", want: "Config"},
		{key: "1", want: "Connection"},
	} {
		tui.SendAndWait(tc.key, tc.want, 5*time.Second)
	}

	tui.Quit()
}

func TestTUIPTYConfigEditSmoke(t *testing.T) {
	tui := startTUISmoke(t)

	tui.WaitFor("Connection", 10*time.Second)
	tui.SendAndWait("\x1b", "Connection fields", 5*time.Second)
	tui.SendAndWait("5", "Config", 5*time.Second)
	tui.Send("\r/tmp/dolly-pty-smoke")
	tui.WaitFor("/tmp/dolly-pt", 5*time.Second)

	tui.Quit()
}

type tuiSmoke struct {
	t    *testing.T
	ptmx *os.File
	done <-chan error
	out  <-chan string
	buf  bytes.Buffer
}

func startTUISmoke(t *testing.T) *tuiSmoke {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping PTY smoke in short mode")
	}
	if os.Getenv(envTUIPTYSmoke) != "1" {
		t.Skipf("set %s=1 to run interactive PTY smoke", envTUIPTYSmoke)
	}

	bin := buildDollyBinary(t)
	workDir := t.TempDir()

	cmd := exec.Command(bin, "tui")
	cmd.Dir = workDir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + workDir,
		"XDG_CONFIG_HOME=" + filepath.Join(workDir, "xdg"),
		"TERM=xterm-256color",
		"NO_COLOR=1",
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty.Start: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = ptmx.Close()
		select {
		case <-done:
			return
		default:
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})

	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		t.Fatalf("pty.Setsize: %v", err)
	}
	out := make(chan string, 16)
	go func() {
		readBuf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(readBuf)
			if n > 0 {
				out <- string(readBuf[:n])
			}
			if err != nil {
				close(out)
				return
			}
		}
	}()

	return &tuiSmoke{t: t, ptmx: ptmx, done: done, out: out}
}

func (s *tuiSmoke) WaitFor(want string, timeout time.Duration) string {
	s.t.Helper()
	if strings.Contains(s.buf.String(), want) {
		return s.buf.String()
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case chunk, ok := <-s.out:
			if !ok {
				s.t.Fatalf("wait for %q: PTY closed before match\npartial output:\n%s", want, s.buf.String())
			}
			s.buf.WriteString(chunk)
			if strings.Contains(s.buf.String(), want) {
				return s.buf.String()
			}
		case <-timer.C:
			s.t.Fatalf("wait for %q: timeout\npartial output:\n%s", want, s.buf.String())
		}
	}
}

func (s *tuiSmoke) Send(keys string) {
	s.t.Helper()
	if _, err := s.ptmx.Write([]byte(keys)); err != nil {
		s.t.Fatalf("write keys %q: %v", keys, err)
	}
}

func (s *tuiSmoke) SendAndWait(keys, want string, timeout time.Duration) string {
	s.t.Helper()
	s.buf.Reset()
	s.Send(keys)
	return s.WaitFor(want, timeout)
}

func (s *tuiSmoke) Quit() {
	s.t.Helper()

	s.SendAndWait("\x03", "Quit dolly?", 5*time.Second) // Ctrl+C opens quit modal regardless of focused screen/field.
	s.Send("y")
	select {
	case err := <-s.done:
		if err != nil {
			s.t.Fatalf("dolly tui exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		s.t.Fatal("timeout waiting for dolly tui to exit after quit")
	}
}

func buildDollyBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "dolly")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func TestTUIPTYCloneCtrlC(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PTY smoke in short mode")
	}
	if os.Getenv(envTUIPTYSmoke) != "1" {
		t.Skipf("set %s=1 to run interactive PTY smoke", envTUIPTYSmoke)
	}

	bin := buildDollyBinary(t)
	workDir := t.TempDir()

	cmd := exec.Command(bin, "clone")
	cmd.Dir = workDir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + workDir,
		"XDG_CONFIG_HOME=" + filepath.Join(workDir, "xdg"),
		"TERM=xterm-256color",
		"NO_COLOR=1",
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty.Start: %v", err)
	}

	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		t.Fatalf("pty.Setsize: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()

	t.Cleanup(func() {
		_ = ptmx.Close()
		select {
		case <-done:
			return
		default:
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})

	// Wait for first prompt output before sending Ctrl-C.
	out := make(chan string, 16)
	go func() {
		readBuf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(readBuf)
			if n > 0 {
				out <- string(readBuf[:n])
			}
			if err != nil {
				close(out)
				return
			}
		}
	}()

	// Collect output until we see a prompt indicator (or timeout).
	var buf bytes.Buffer
	promptTimer := time.NewTimer(10 * time.Second)
	defer promptTimer.Stop()
waitLoop:
	for {
		select {
		case chunk, ok := <-out:
			if !ok {
				break waitLoop
			}
			buf.WriteString(chunk)
			if strings.Contains(buf.String(), "ource") {
				break waitLoop
			}
		case <-promptTimer.C:
			t.Fatalf("timeout waiting for clone prompt\npartial output:\n%s", buf.String())
		}
	}

	// Send Ctrl-C (becomes SIGINT to the child session leader).
	if _, err := ptmx.Write([]byte("\x03")); err != nil {
		t.Fatalf("write Ctrl-C: %v", err)
	}

	// Wait for process exit within 1 second.
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected non-zero exit after SIGINT")
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected ExitError, got %T: %v", err, err)
		}
		ws, ok := exitErr.Sys().(syscall.WaitStatus)
		if !ok {
			t.Fatalf("Sys() is not syscall.WaitStatus on this platform")
		}
		if !ws.Signaled() {
			t.Fatal("expected signaled=true")
		}
		if ws.Signal() != syscall.SIGINT {
			t.Fatalf("signal = %v, want syscall.SIGINT", ws.Signal())
		}
	case <-time.After(time.Second):
		t.Fatal("clone did not terminate within 1 second after Ctrl-C")
	}
}
