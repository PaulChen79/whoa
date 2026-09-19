package core

import (
	"testing"
	"time"
)

func observeAs(t *testing.T, h Harness, name string) Observation {
	t.Helper()
	return Observe(Input{
		Raw:     payload(t, name),
		Harness: h,
		Now:     time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC),
	})
}

// Codex reports a successful and a failed tool call on the same event, so the
// Outcome has to come from the tool's own response.
func TestACodexOutcomeComesFromTheToolResponse(t *testing.T) {
	tests := map[string]Outcome{
		"codex_posttooluse_ok":         OutcomeOK,
		"codex_posttooluse_failed":     OutcomeError,
		"codex_posttooluse_applypatch": OutcomeOK,
	}
	for name, want := range tests {
		ob := observeAs(t, Codex, name)
		if ob.Entry == nil {
			t.Fatalf("%s: no Step recorded", name)
		}
		if ob.Entry.Outcome != want {
			t.Errorf("%s: Outcome = %q, want %q", name, ob.Entry.Outcome, want)
		}
	}
}

// The whole point of absorbing the Harness differences inside Seam 1 is that
// nothing downstream can tell which Harness it is looking at. Counters, the
// Window and the Trigger all read Entries, so if the two shapes diverged, a
// Loop would be detectable on one Harness and not the other.
func TestBothHarnessesProduceTheSameShapeForTheSameWork(t *testing.T) {
	claude := observeAs(t, ClaudeCode, "posttoolusefailure_bash").Entry
	codex := observeAs(t, Codex, "codex_posttooluse_failed").Entry
	if claude == nil || codex == nil {
		t.Fatal("both Harnesses must record a Step")
	}

	if claude.Kind != codex.Kind {
		t.Errorf("Kind: claude %q, codex %q", claude.Kind, codex.Kind)
	}
	if claude.Outcome != codex.Outcome {
		t.Errorf("Outcome: claude %q, codex %q", claude.Outcome, codex.Outcome)
	}
	for _, f := range []struct {
		name          string
		claude, codex string
	}{
		{"session", claude.Session, codex.Session},
		{"turn", claude.Turn, codex.Turn},
		{"tool_use_id", claude.ToolUseID, codex.ToolUseID},
	} {
		if (f.claude == "") != (f.codex == "") {
			t.Errorf("%s: claude %q, codex %q — one Harness fills it and the other does not", f.name, f.claude, f.codex)
		}
	}
	if claude.Command == nil || codex.Command == nil {
		t.Fatalf("both must carry a redacted command: claude %v, codex %v", claude.Command, codex.Command)
	}
	if claude.Command.Text != codex.Command.Text {
		t.Errorf("command: claude %q, codex %q — the same work must read the same", claude.Command.Text, codex.Command.Text)
	}
	if claude.DurationMS == 0 || codex.DurationMS == 0 {
		t.Errorf("duration: claude %d, codex %d — both Harnesses report one, in different units", claude.DurationMS, codex.DurationMS)
	}
}

// Codex has no PostToolUseFailure. Registering for it would be a hook that
// never fires, which is the failure mode `whoa doctor` exists to catch.
func TestEventsMatchWhatEachHarnessActuallyFires(t *testing.T) {
	has := func(h Harness, name string) bool {
		for _, e := range Events(h) {
			if e == name {
				return true
			}
		}
		return false
	}
	if !has(ClaudeCode, "PostToolUseFailure") {
		t.Error("Claude Code splits failures onto their own event; whoa must register for it")
	}
	if has(Codex, "PostToolUseFailure") {
		t.Error("Codex has no PostToolUseFailure; registering for it installs a hook that never fires")
	}
	for _, name := range []string{userPromptSubmit, "PreToolUse", "PostToolUse"} {
		if !has(Codex, name) {
			t.Errorf("Codex must register for %q", name)
		}
	}
}

// Seam 2 does not care which Harness it is, and this is the proof: a Codex
// apply_patch yields the same Signals a Claude Code Edit would.
func TestCodexPatchesYieldTheSameSignals(t *testing.T) {
	ob := observeAs(t, Codex, "codex_posttooluse_applypatch")
	if ob.Entry == nil || ob.Entry.Signals == nil {
		t.Fatal("expected Signals from an apply_patch")
	}
	s := ob.Entry.Signals
	if !s.TestFile || s.AssertionsRemoved != 1 || s.SkipMarkersAdded != 1 {
		t.Errorf("Signals = %+v, want a test file losing an assertion and gaining a skip", s)
	}
}

func TestACodexPromptSetsTheGoal(t *testing.T) {
	ob := observeAs(t, Codex, "codex_userpromptsubmit")
	if ob.Entry == nil || ob.Entry.Kind != KindGoal {
		t.Fatalf("Entry = %+v, want a Goal", ob.Entry)
	}
	if ob.Entry.Goal != "fix the failing auth tests" {
		t.Errorf("Goal = %q", ob.Entry.Goal)
	}
	if ob.Entry.Turn != "01a0661c-9999-7e43-a838-3c0b110065c5" {
		t.Errorf("Turn = %q, want the Codex turn_id", ob.Entry.Turn)
	}
}
