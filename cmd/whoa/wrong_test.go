package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWrongMarksTheLastVerdictAndSaysWhat(t *testing.T) {
	s, out, _ := testSystem(t, "")
	writeConfig(t, s.home, `{"trigger": {"repeated_failures": 2}}`)
	feed(t, s.home, failedStep)
	feed(t, s.home, failedStep)
	feed(t, s.home, beforeNextStep)

	if err := run([]string{"wrong"}, s); err != nil {
		t.Fatalf("wrong: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "Marked as a Misjudgment") {
		t.Errorf("wrong did not confirm the mark:\n%s", text)
	}
	if !strings.Contains(text, "failed 2 times in a row") {
		t.Errorf("wrong did not say what it marked:\n%s", text)
	}

	lines := logLines(t, s, "abc123")
	last := lines[len(lines)-1]
	if !strings.Contains(last, `"kind":"wrong"`) || !strings.Contains(last, `"ref":`) {
		t.Errorf("the mark does not point at what it disputes:\n%s", last)
	}
}

// A user who types this expecting something to happen deserves a sentence, not
// a stack trace or a silent success.
func TestWrongWithNothingToMarkSaysSo(t *testing.T) {
	s, out, _ := testSystem(t, "")
	if err := run([]string{"wrong"}, s); err != nil {
		t.Errorf("wrong failed obscurely instead of explaining: %v", err)
	}
	if !strings.Contains(out.String(), "nothing to mark") {
		t.Errorf("wrong did not explain itself:\n%s", out.String())
	}
}

// Marking twice should reach the Verdict before, not silently re-mark the same
// one and let the user think they logged two complaints.
func TestWrongTwiceReachesTheVerdictBefore(t *testing.T) {
	s, out, _ := testSystem(t, "")
	writeConfig(t, s.home, `{"trigger": {"repeated_failures": 2}}`)
	for i := 0; i < 2; i++ {
		feed(t, s.home, failedStep)
	}
	feed(t, s.home, beforeNextStep)
	for i := 0; i < 2; i++ {
		feed(t, s.home, failedStep)
	}
	feed(t, s.home, beforeNextStep)

	if err := run([]string{"wrong"}, s); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"wrong"}, s); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "nothing to mark") {
		t.Errorf("the second mark found nothing though two Verdicts were given:\n%s", out.String())
	}

	marks := 0
	for _, line := range logLines(t, s, "abc123") {
		if strings.Contains(line, `"kind":"wrong"`) {
			marks++
		}
	}
	if marks != 2 {
		t.Errorf("recorded %d Misjudgments, want 2", marks)
	}
}

// Retention is a promise about a directory, and state_dir moves the directory.
func TestExpiryFollowsStateDir(t *testing.T) {
	s, _, _ := testSystem(t, "")
	elsewhere := filepath.Join(t.TempDir(), "logs")
	writeConfig(t, s.home, `{"state_dir": "`+elsewhere+`", "retention_days": 14}`)

	feed(t, s.home, failedStep)
	path := filepath.Join(elsewhere, "sessions", "abc123.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log not written to state_dir: %v", err)
	}

	// An abandoned Session from a month ago, in the relocated directory. The
	// live Session cannot be used for this: whoa appends to it on every Step,
	// so its own log is never idle long enough to expire, which is the
	// behaviour wanted.
	stale := filepath.Join(elsewhere, "sessions", "last-month.jsonl")
	if err := os.WriteFile(stale, []byte(`{"kind":"step","session":"last-month"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -30)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(elsewhere, ".last-sweep")); err != nil {
		t.Fatal(err)
	}
	feed(t, s.home, failedStep)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a log past retention in a relocated state_dir survived")
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("the live Session log was expired while it was still in use")
	}
}

// "whoa has never said anything" and "you have marked everything it said" are
// the same outcome in code and very different sentences to read.
func TestWrongDistinguishesNothingSaidFromAllMarked(t *testing.T) {
	s, out, _ := testSystem(t, "")
	writeConfig(t, s.home, `{"trigger": {"repeated_failures": 2}}`)
	feed(t, s.home, failedStep)
	feed(t, s.home, failedStep)
	feed(t, s.home, beforeNextStep)

	if err := run([]string{"wrong"}, s); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"wrong"}, s); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already marked") {
		t.Errorf("told the user whoa had said nothing, when they had marked all of it:\n%s", out.String())
	}
}
