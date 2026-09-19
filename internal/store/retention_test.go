package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PaulChen79/whoa/internal/core"
)

func writeLog(t *testing.T, dir, session string, age time.Duration, entries ...core.Entry) string {
	t.Helper()
	for i := range entries {
		if err := Append(dir, &entries[i]); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "sessions", session+".jsonl")
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	return path
}

func failedStepIn(session string) core.Entry {
	return core.Entry{Kind: core.KindStep, Timestamp: time.Now(), Harness: core.ClaudeCode,
		Session: session, Tool: "Bash", Outcome: core.OutcomeError}
}

// Watching an agent must not quietly accumulate a permanent record of
// someone's work.
func TestLogsOlderThanRetentionAreRemoved(t *testing.T) {
	dir := t.TempDir()
	old := writeLog(t, dir, "old", 30*24*time.Hour, failedStepIn("old"))
	fresh := writeLog(t, dir, "fresh", 2*24*time.Hour, failedStepIn("fresh"))

	removed, err := Expire(dir, 14)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("removed %d logs, want 1", removed)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("a log past retention survived")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a log within retention was removed")
	}
}

// The calibration corpus is the reason whoa can ever be shown to be
// well-judged. Losing a marked Misjudgment to a timer would throw away the
// only evidence the user went to the trouble of producing.
func TestAMarkedLogSurvivesExpiry(t *testing.T) {
	dir := t.TempDir()
	marked := writeLog(t, dir, "marked", 90*24*time.Hour,
		failedStepIn("marked"),
		core.Entry{Kind: core.KindNudge, Timestamp: time.Now(), Session: "marked", Counter: "repeated_failures", Count: 3},
		core.Entry{Kind: core.KindWrong, Timestamp: time.Now(), Session: "marked"},
	)

	if _, err := Expire(dir, 14); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marked); err != nil {
		t.Error("a log holding a Misjudgment was expired; the calibration corpus is gone")
	}
}

func TestExpiryIsOffWhenRetentionIsZero(t *testing.T) {
	dir := t.TempDir()
	path := writeLog(t, dir, "ancient", 365*24*time.Hour, failedStepIn("ancient"))
	if removed, err := Expire(dir, 0); err != nil || removed != 0 {
		t.Fatalf("Expire(0) = %d, %v; want it to do nothing", removed, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("retention 0 removed a log")
	}
}

func TestExpiryOnAMissingDirectoryIsNotAnError(t *testing.T) {
	if _, err := Expire(filepath.Join(t.TempDir(), "nothing-here"), 14); err != nil {
		t.Errorf("Expire on a fresh install = %v, want no error", err)
	}
}

// Sweeping on every Step would stat every log thousands of times a day for no
// benefit, so a sweep is due at most once a day.
func TestASweepIsDueOnceADay(t *testing.T) {
	dir := t.TempDir()
	if !SweepDue(dir, time.Now()) {
		t.Error("the first sweep must be due")
	}
	if err := MarkSwept(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if SweepDue(dir, time.Now().Add(time.Hour)) {
		t.Error("swept an hour ago and due again")
	}
	if !SweepDue(dir, time.Now().Add(25*time.Hour)) {
		t.Error("swept 25 hours ago and not due")
	}
}

// A marked line has to carry everything needed to reconstruct what whoa saw,
// or disputing a Verdict proves nothing.
func TestAMarkedVerdictReplaysToTheSameCounters(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		e := failedStepIn("replay")
		if err := Append(dir, &e); err != nil {
			t.Fatal(err)
		}
	}
	nudge := core.Entry{Kind: core.KindNudge, Timestamp: time.Now(), Session: "replay",
		Counter: "repeated_failures", Count: 3, Threshold: 3}
	if err := Append(dir, &nudge); err != nil {
		t.Fatal(err)
	}
	wrong := core.Entry{Kind: core.KindWrong, Timestamp: time.Now(), Session: "replay", Ref: 4}
	if err := Append(dir, &wrong); err != nil {
		t.Fatal(err)
	}

	log, err := Load(dir, "replay")
	if err != nil {
		t.Fatal(err)
	}
	evidence := core.Before(log, wrong.Ref)
	got := core.Count(evidence, core.Defaults().Window)
	if got.RepeatedFailures != nudge.Count {
		t.Errorf("replayed RepeatedFailures = %d, want the %d the Verdict claimed", got.RepeatedFailures, nudge.Count)
	}
}
