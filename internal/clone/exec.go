package clone

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

// CommandRunner abstracts os/exec for pg_dump/psql invocation.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
	RunWithEnv(ctx context.Context, env map[string]string, name string, args ...string) error
	Pipe(ctx context.Context, srcName string, srcArgs []string, dstName string, dstArgs []string) error
	PipeWithEnv(ctx context.Context, env map[string]string, srcName string, srcArgs []string, dstName string, dstArgs []string) error
}

// OSCommandRunner is the real implementation using os/exec.
type OSCommandRunner struct{}

// SilentCommandRunner delegates command execution while suppressing process output.
type SilentCommandRunner struct {
	Inner CommandRunner
}

var silentCommandOutputMu sync.Mutex

func (r SilentCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	return withSilentCommandOutput(func() error {
		return r.inner().Run(ctx, name, args...)
	})
}

func (r SilentCommandRunner) RunWithEnv(ctx context.Context, env map[string]string, name string, args ...string) error {
	return withSilentCommandOutput(func() error {
		return r.inner().RunWithEnv(ctx, env, name, args...)
	})
}

func (r SilentCommandRunner) Pipe(ctx context.Context, srcName string, srcArgs []string, dstName string, dstArgs []string) error {
	return withSilentCommandOutput(func() error {
		return r.inner().Pipe(ctx, srcName, srcArgs, dstName, dstArgs)
	})
}

func (r SilentCommandRunner) PipeWithEnv(ctx context.Context, env map[string]string, srcName string, srcArgs []string, dstName string, dstArgs []string) error {
	return withSilentCommandOutput(func() error {
		return r.inner().PipeWithEnv(ctx, env, srcName, srcArgs, dstName, dstArgs)
	})
}

func (r SilentCommandRunner) inner() CommandRunner {
	if r.Inner != nil {
		return r.Inner
	}
	return OSCommandRunner{}
}

func commandRunnerForProgress(runner CommandRunner, progressFn func(string)) CommandRunner {
	if runner == nil {
		runner = OSCommandRunner{}
	}
	if progressFn == nil {
		return runner
	}
	return SilentCommandRunner{Inner: runner}
}

func reportProgress(progressFn func(string), message string) {
	if progressFn != nil {
		progressFn(message)
	}
}

// reportProgressEvent calls the typed ProgressEvent callback and, when set,
// the deprecated ProgressFn string callback for backward compatibility.
// Both fire when both are configured so downstream adapters can bridge either path.
func reportProgressEvent(opts Options, ev ProgressEvent) {
	if opts.ProgressEvent != nil {
		opts.ProgressEvent(ev)
	}
	if opts.ProgressFn != nil {
		opts.ProgressFn(ev.Step)
	}
}

func withSilentCommandOutput(fn func() error) error {
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open null output: %w", err)
	}
	defer devNull.Close()

	silentCommandOutputMu.Lock()
	defer silentCommandOutputMu.Unlock()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	os.Stdout = devNull
	os.Stderr = devNull
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	return fn()
}

func (OSCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	return (OSCommandRunner{}).RunWithEnv(ctx, nil, name, args...)
}

func (OSCommandRunner) RunWithEnv(ctx context.Context, env map[string]string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Env = StripSensitiveEnv(os.Environ())
	if len(env) > 0 {
		cmd.Env = append(cmd.Env, envMapToSlice(env)...)
	}
	if err := cmd.Run(); err != nil {
		return commandFailed("run "+name, err, stderr.String())
	}
	return nil
}

// StripSensitiveEnv returns a copy of environ with PostgreSQL secret variables
// removed. Explicit env maps in RunWithEnv take precedence over the stripped
// environment and can re-add variables when intentionally needed.
func StripSensitiveEnv(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		upper := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			upper = kv[:i]
		}
		switch strings.ToUpper(upper) {
		case "PGPASSWORD", "PGSSLKEY", "PGSSLCERT":
			continue
		}
		out = append(out, kv)
	}
	return out
}

func envMapToSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// Pipe pipes stdout of srcCmd into stdin of dstCmd. Both child processes
// inherit a sanitized copy of the parent environment (PGPASSWORD, PGSSLKEY,
// and PGSSLCERT are stripped).
func (OSCommandRunner) Pipe(ctx context.Context, srcName string, srcArgs []string, dstName string, dstArgs []string) error {
	return (OSCommandRunner{}).PipeWithEnv(ctx, nil, srcName, srcArgs, dstName, dstArgs)
}

// PipeWithEnv pipes stdout of srcCmd into stdin of dstCmd with optional
// extra environment variables. Both child processes inherit a sanitized
// copy of the parent environment; explicit env entries take precedence
// over the stripped environment.
func (OSCommandRunner) PipeWithEnv(ctx context.Context, env map[string]string, srcName string, srcArgs []string, dstName string, dstArgs []string) error {
	baseEnv := StripSensitiveEnv(os.Environ())

	srcCmd := exec.CommandContext(ctx, srcName, srcArgs...)
	srcCmd.Env = append([]string(nil), baseEnv...)
	dstCmd := exec.CommandContext(ctx, dstName, dstArgs...)
	dstCmd.Env = append([]string(nil), baseEnv...)

	if len(env) > 0 {
		extra := envMapToSlice(env)
		srcCmd.Env = append(srcCmd.Env, extra...)
		dstCmd.Env = append(dstCmd.Env, extra...)
	}

	srcOut, err := srcCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe stdout from %s: %w", srcName, err)
	}
	var srcStderr, dstStderr strings.Builder
	srcCmd.Stderr = &srcStderr

	dstCmd.Stdin = srcOut
	dstCmd.Stdout = os.Stdout
	dstCmd.Stderr = &dstStderr

	if err := srcCmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", srcName, err)
	}

	if err := dstCmd.Start(); err != nil {
		_ = srcOut.Close()
		_ = srcCmd.Process.Kill()
		_ = srcCmd.Wait()
		return fmt.Errorf("start %s: %w", dstName, err)
	}

	// Wait for the sink first. If the sink exits early (e.g., on error),
	// the source may block writing to a full pipe buffer. Kill the source
	// to unblock it before calling Wait.
	dstErr := dstCmd.Wait()
	if dstErr != nil {
		_ = srcCmd.Process.Kill()
	}
	srcErr := srcCmd.Wait()

	if dstErr != nil {
		return commandFailed(dstName+" failed", dstErr, firstNonEmpty(dstStderr.String(), srcStderr.String()))
	}
	if srcErr != nil {
		return commandFailed(srcName+" failed", srcErr, firstNonEmpty(srcStderr.String(), dstStderr.String()))
	}
	return nil
}

func commandFailed(label string, err error, stderr string) error {
	stderr = strings.TrimSpace(connections.RedactMessage(stderr))
	if stderr == "" {
		return fmt.Errorf("%s: %w", label, err)
	}
	return fmt.Errorf("%s: %w (stderr: %s)", label, err, stderr)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
