package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PaulChen79/whoa/internal/core"
)

func step(session string) *core.Step {
	return &core.Step{
		Timestamp: time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC),
		Harness:   core.ClaudeCode,
		Session:   session,
		Tool:      "Bash",
		Outcome:   core.OutcomeOK,
	}
}

func TestAppendWritesOneLinePerStep(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := Append(dir, step("abc123")); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	b, err := os.ReadFile(filepath.Join(dir, "sessions", "abc123.jsonl"))
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3:\n%s", len(lines), b)
	}
	for i, line := range lines {
		var s core.Step
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}

func TestAppendCreatesMissingDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does", "not", "exist")
	if err := Append(dir, step("abc123")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sessions", "abc123.jsonl")); err != nil {
		t.Fatalf("log not created: %v", err)
	}
}

func TestEachSessionGetsItsOwnLog(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, step("one")); err != nil {
		t.Fatal(err)
	}
	if err := Append(dir, step("two")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d logs, want 2", len(entries))
	}
}

// A Session id arrives from outside whoa, so it must never be able to steer a
// write out of the state directory.
func TestSessionIdCannotEscapeTheStateDirectory(t *testing.T) {
	for _, id := range []string{
		"../escape",
		"../../escape",
		"a/b",
		`..\escape`,
		"/etc/passwd",
		".",
		"..",
	} {
		dir := t.TempDir()
		err := Append(dir, step(id))
		if err != nil {
			continue // refusing outright is a fine answer
		}
		written := []string{}
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				written = append(written, p)
			}
			return nil
		})
		if len(written) == 0 {
			t.Errorf("%q: wrote nothing and reported no error", id)
		}
		for _, p := range written {
			rel, err := filepath.Rel(filepath.Join(dir, "sessions"), p)
			if err != nil || strings.HasPrefix(rel, "..") {
				t.Errorf("%q: escaped to %s", id, p)
			}
		}
	}
}

func TestEmptySessionIdIsRefused(t *testing.T) {
	if err := Append(t.TempDir(), step("")); err == nil {
		t.Error("expected an error for an empty Session id")
	}
}

// The Session log is private to the user who is being observed.
func TestLogIsNotWorldReadable(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, step("abc123")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "sessions", "abc123.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("log mode is %o, want no group or other access", perm)
	}
}
