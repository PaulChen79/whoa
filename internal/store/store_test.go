package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PaulChen79/whoa/internal/core"
)

func step(session string) *core.Entry {
	return &core.Entry{
		Kind:      core.KindStep,
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
		var s core.Entry
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

func TestLoadReturnsWhatAppendWrote(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := Append(dir, step("s1")); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := Load(dir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("Load() returned %d entries, want 3", len(entries))
	}
	if !entries[0].IsStep() || entries[0].Tool != step("s1").Tool {
		t.Errorf("Load() = %+v, want the Step that was appended", entries[0])
	}
}

// The first Step of a Session is observed before any log exists.
func TestLoadingAMissingLogIsAnEmptySession(t *testing.T) {
	entries, err := Load(t.TempDir(), "never-seen")
	if err != nil {
		t.Fatalf("Load() on a missing log: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Load() = %v, want nothing", entries)
	}
}

// A log truncated by a crash mid-write must cost one Step's evidence, not the
// rest of the Session.
func TestLoadSkipsALineItCannotParse(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, step("s1")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sessions", "s1.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"kind\":\"step\",\"tool\":\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := Append(dir, step("s1")); err != nil {
		t.Fatal(err)
	}

	entries, err := Load(dir, "s1")
	if err != nil {
		t.Fatalf("Load() over a truncated line: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("Load() returned %d entries, want the two intact ones", len(entries))
	}
}

func TestLoadRejectsASessionIDThatCouldEscapeTheStateDir(t *testing.T) {
	if _, err := Load(t.TempDir(), "../../etc/passwd"); err == nil {
		t.Error("Load() accepted a session id containing a path traversal")
	}
}

func TestAppendRefusesAnEntryWithNoKind(t *testing.T) {
	e := step("s1")
	e.Kind = ""
	if err := Append(t.TempDir(), e); err == nil {
		t.Error("Append() wrote an entry with no kind")
	}
}

// A Misjudgment report is worth something only if the Verdict it disputes can
// be reproduced from the log on disk. Replaying in memory proves nothing about
// a pure function; this writes the Session out, reads it back, and checks that
// the Counters and the Nudge decision come out identical.
func TestAPersistedLogReplaysToTheIdenticalVerdict(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	var live []core.Entry
	for i := 0; i < 4; i++ {
		e := core.Entry{
			Kind: core.KindStep, Timestamp: now, Harness: core.ClaudeCode,
			Session: "replay", Turn: "t1", Tool: "Bash", Outcome: core.OutcomeError,
			Goal: "修好 flaky test",
		}
		if err := Append(dir, &e); err != nil {
			t.Fatal(err)
		}
		live = append(live, e)
	}

	reloaded, err := Load(dir, "replay")
	if err != nil {
		t.Fatal(err)
	}

	cfg := core.Defaults()
	cfg.Trigger = core.Trigger{RepeatedFailures: 3}

	if got, want := core.Count(reloaded, cfg.Window), core.Count(live, cfg.Window); got != want {
		t.Errorf("Counters after a round trip = %+v, want %+v", got, want)
	}

	raw := []byte(`{"session_id":"replay","prompt_id":"t1","hook_event_name":"PreToolUse","tool_name":"Bash"}`)
	verdict := func(log []core.Entry) core.Observation {
		return core.Observe(core.Input{Raw: raw, Harness: core.ClaudeCode, Log: log, Config: cfg, Now: now})
	}

	fromDisk, inMemory := verdict(reloaded), verdict(live)
	if len(inMemory.Output) == 0 {
		t.Fatal("expected a Nudge to replay")
	}
	if string(fromDisk.Output) != string(inMemory.Output) {
		t.Errorf("Verdict from disk = %s, want %s", fromDisk.Output, inMemory.Output)
	}
	if fromDisk.Human != inMemory.Human {
		t.Errorf("human line from disk = %q, want %q", fromDisk.Human, inMemory.Human)
	}
	if !reflect.DeepEqual(fromDisk.Entry, inMemory.Entry) {
		t.Errorf("recorded Entry from disk = %+v, want %+v", *fromDisk.Entry, *inMemory.Entry)
	}
}
