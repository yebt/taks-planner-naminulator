// Package memory is the long-term memory port and its adapters. The Engram
// adapter shells out to the autodetected `engram` CLI; when engram isn't
// installed, Detect returns a Noop so the rest of the app degrades gracefully.
package memory

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ErrUnavailable is returned by the Noop backend.
var ErrUnavailable = errors.New("memory backend not available (install engram)")

// defaultTimeout bounds a single engram invocation. The CLI can block on its
// own backend and callers hand us a context with no deadline, so without this a
// hung engram blocks the caller forever — in the TUI that means a frozen prompt
// that not even ctrl+c can reach.
const defaultTimeout = 10 * time.Second

// Memory is the port for saving and recalling long-term notes.
type Memory interface {
	Available() bool
	Name() string
	Save(ctx context.Context, title, content string) error
	Recall(ctx context.Context, query string, limit int) (string, error)
}

// Detect returns an Engram-backed Memory if the `engram` CLI is on PATH,
// otherwise a Noop. project scopes engram operations (empty = autodetect).
func Detect(project string) Memory {
	path, err := exec.LookPath("engram")
	if err != nil {
		return Noop{}
	}
	return &Engram{bin: path, project: project, run: defaultRun, timeout: defaultTimeout}
}

type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Engram shells out to the engram CLI. A zero timeout means defaultTimeout, so
// a directly constructed Engram is bounded too.
type Engram struct {
	bin     string
	project string
	run     runner
	timeout time.Duration
}

func (e *Engram) Available() bool { return true }
func (e *Engram) Name() string    { return "engram" }

func (e *Engram) timeoutOrDefault() time.Duration {
	if e.timeout <= 0 {
		return defaultTimeout
	}
	return e.timeout
}

// exec runs the CLI under a bounded context so a hung engram surfaces as a
// timeout instead of blocking the caller. A cancellation coming from the caller
// is left as-is: it is not our deadline and must not be reported as one.
func (e *Engram) exec(ctx context.Context, args ...string) ([]byte, error) {
	timeout := e.timeoutOrDefault()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := e.run(ctx, e.bin, args...)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return out, fmt.Errorf("timed out after %s", timeout)
	}
	return out, err
}

// fail wraps a failed invocation, appending the CLI output only when there is
// some (a timeout usually has none).
func fail(op string, out []byte, err error) error {
	if s := strings.TrimSpace(string(out)); s != "" {
		return fmt.Errorf("engram %s: %v: %s", op, err, s)
	}
	return fmt.Errorf("engram %s: %v", op, err)
}

func (e *Engram) Save(ctx context.Context, title, content string) error {
	args := []string{"save", title, content, "--type", "note"}
	if e.project != "" {
		args = append(args, "--project", e.project)
	}
	out, err := e.exec(ctx, args...)
	if err != nil {
		return fail("save", out, err)
	}
	return nil
}

func (e *Engram) Recall(ctx context.Context, query string, limit int) (string, error) {
	if limit <= 0 {
		limit = 5
	}
	args := []string{"search", query, "--limit", strconv.Itoa(limit)}
	if e.project != "" {
		args = append(args, "--project", e.project)
	}
	out, err := e.exec(ctx, args...)
	if err != nil {
		return "", fail("search", out, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Noop is used when no memory backend is installed.
type Noop struct{}

func (Noop) Available() bool                                     { return false }
func (Noop) Name() string                                        { return "none" }
func (Noop) Save(context.Context, string, string) error          { return ErrUnavailable }
func (Noop) Recall(context.Context, string, int) (string, error) { return "", ErrUnavailable }
