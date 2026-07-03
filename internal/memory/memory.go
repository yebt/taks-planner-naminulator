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
)

// ErrUnavailable is returned by the Noop backend.
var ErrUnavailable = errors.New("memory backend not available (install engram)")

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
	return &Engram{bin: path, project: project, run: defaultRun}
}

type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Engram shells out to the engram CLI.
type Engram struct {
	bin     string
	project string
	run     runner
}

func (e *Engram) Available() bool { return true }
func (e *Engram) Name() string    { return "engram" }

func (e *Engram) Save(ctx context.Context, title, content string) error {
	args := []string{"save", title, content, "--type", "note"}
	if e.project != "" {
		args = append(args, "--project", e.project)
	}
	out, err := e.run(ctx, e.bin, args...)
	if err != nil {
		return fmt.Errorf("engram save: %v: %s", err, strings.TrimSpace(string(out)))
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
	out, err := e.run(ctx, e.bin, args...)
	if err != nil {
		return "", fmt.Errorf("engram search: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Noop is used when no memory backend is installed.
type Noop struct{}

func (Noop) Available() bool                                     { return false }
func (Noop) Name() string                                        { return "none" }
func (Noop) Save(context.Context, string, string) error          { return ErrUnavailable }
func (Noop) Recall(context.Context, string, int) (string, error) { return "", ErrUnavailable }
