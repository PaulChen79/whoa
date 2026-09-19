package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var at = time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)

func payload(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatalf("reading payload: %v", err)
	}
	return b
}

func observe(t *testing.T, name string) Observation {
	t.Helper()
	return Observe(Input{Raw: payload(t, name), Harness: ClaudeCode, Now: at})
}

func TestSuccessfulStepIsRecorded(t *testing.T) {
	ob := observe(t, "posttooluse_write")

	if ob.Entry == nil {
		t.Fatal("expected a Step to record")
	}
	if got, want := ob.Entry.Tool, "Write"; got != want {
		t.Errorf("Tool = %q, want %q", got, want)
	}
	if got, want := ob.Entry.Turn, "550e8400-e29b-41d4-a716-446655440000"; got != want {
		t.Errorf("Turn = %q, want %q", got, want)
	}
	if got, want := ob.Entry.Session, "abc123"; got != want {
		t.Errorf("Session = %q, want %q", got, want)
	}
	if got, want := ob.Entry.Outcome, OutcomeOK; got != want {
		t.Errorf("Outcome = %q, want %q", got, want)
	}
	if got, want := ob.Entry.DurationMS, 12; got != want {
		t.Errorf("DurationMS = %d, want %d", got, want)
	}
}

func TestFailedStepIsRecordedAsAFailure(t *testing.T) {
	ob := observe(t, "posttoolusefailure_bash")

	if ob.Entry == nil {
		t.Fatal("expected a Step to record")
	}
	if got, want := ob.Entry.Outcome, OutcomeError; got != want {
		t.Errorf("Outcome = %q, want %q", got, want)
	}
	if got, want := ob.Entry.Tool, "Bash"; got != want {
		t.Errorf("Tool = %q, want %q", got, want)
	}
}

// Loop detection counts repeated failures, so a Harness that reports failures
// on a separate event must not leave whoa blind to them.
func TestFailuresAndSuccessesAreBothObserved(t *testing.T) {
	for _, name := range []string{"posttooluse_write", "posttoolusefailure_bash"} {
		if ob := observe(t, name); ob.Entry == nil {
			t.Errorf("%s: expected a Step to record", name)
		}
	}
}

func TestSubagentStepRecordsWhichAgentTookIt(t *testing.T) {
	ob := observe(t, "posttooluse_subagent")

	if ob.Entry == nil {
		t.Fatal("expected a Step to record")
	}
	if got, want := ob.Entry.AgentID, "agent_01"; got != want {
		t.Errorf("AgentID = %q, want %q", got, want)
	}
}

func TestMissingTurnKeyIsNotAnError(t *testing.T) {
	ob := observe(t, "posttooluse_no_prompt_id")

	if ob.Entry == nil {
		t.Fatal("a Step before the first user prompt is still a Step")
	}
	if ob.Entry.Turn != "" {
		t.Errorf("Turn = %q, want empty", ob.Entry.Turn)
	}
}

// The agent's behaviour must never change, so whoa emits nothing at all.
func TestObservationEmitsNoOutput(t *testing.T) {
	for _, name := range []string{"posttooluse_write", "posttoolusefailure_bash", "posttooluse_subagent"} {
		if ob := observe(t, name); len(ob.Output) != 0 {
			t.Errorf("%s: emitted %q, want nothing", name, ob.Output)
		}
	}
}

// A broken payload must cost the user nothing: no output, no recorded Step,
// and above all no failed Step for the agent.
func TestMalformedPayloadIsSilentlyIgnored(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":         "",
		"not json":      "this is not json",
		"truncated":     `{"session_id": "abc`,
		"json array":    `[]`,
		"json null":     `null`,
		"no session id": `{"hook_event_name":"PostToolUse","tool_name":"Bash"}`,
		"no tool name":  `{"session_id":"abc","hook_event_name":"PostToolUse"}`,
		"unknown event": `{"session_id":"abc","hook_event_name":"SessionStart"}`,
	} {
		ob := Observe(Input{Raw: []byte(raw), Harness: ClaudeCode, Now: at})
		if ob.Entry != nil {
			t.Errorf("%s: recorded a Step, want none", name)
		}
		if len(ob.Output) != 0 {
			t.Errorf("%s: emitted %q, want nothing", name, ob.Output)
		}
	}
}

// Nothing free-form reaches disk in this ticket: command text and error text
// can carry secrets, and Redaction does not exist yet.
func TestRecordedStepCarriesNoFreeText(t *testing.T) {
	ob := observe(t, "posttoolusefailure_bash")
	if ob.Entry == nil {
		t.Fatal("expected a Step to record")
	}
	line, err := json.Marshal(ob.Entry)
	if err != nil {
		t.Fatalf("marshalling Step: %v", err)
	}
	// The command survives, redacted, because ADR 0003 says it earns its
	// place. Nothing else the payload carried does: not the error, not the
	// description, not the working directory, not a path.
	for _, text := range []string{"Cannot find module", "Run test suite", "/Users/u/proj", "auth.ts"} {
		if strings.Contains(string(line), text) {
			t.Errorf("Step contains free text %q:\n%s", text, line)
		}
	}
	if ob.Entry.Command == nil || ob.Entry.Command.Text != "npm test" {
		t.Errorf("Command = %+v, want the redacted command text", ob.Entry.Command)
	}
}

// The test above can only catch text someone thought to look for. This one
// catches a field nobody thought about at all: every key a Step writes must be
// one this list names, so adding a field that carries text fails here first.
func TestAStepWritesOnlyTheFieldsItIsAllowedTo(t *testing.T) {
	allowed := map[string]bool{
		"kind": true, "ts": true, "harness": true, "session": true, "turn": true,
		"tool": true, "tool_use_id": true, "agent_id": true, "outcome": true,
		"duration_ms": true, "goal": true,
		// Seam 2 output, and nothing else from the tool's arguments.
		"command": true, "signals": true,
	}

	for _, name := range []string{"posttooluse_write", "posttooluse_subagent", "posttoolusefailure_bash"} {
		ob := observe(t, name)
		if ob.Entry == nil {
			continue
		}
		line, err := json.Marshal(ob.Entry)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(line, &fields); err != nil {
			t.Fatal(err)
		}
		for key := range fields {
			if !allowed[key] {
				t.Errorf("%s: Step writes unlisted field %q; every field is part of the privacy promise", name, key)
			}
		}
	}
}

func TestStepIsOneJSONLine(t *testing.T) {
	ob := observe(t, "posttooluse_write")
	line, err := json.Marshal(ob.Entry)
	if err != nil {
		t.Fatalf("marshalling Step: %v", err)
	}
	if strings.ContainsAny(string(line), "\n\r") {
		t.Errorf("Step marshalled across lines:\n%s", line)
	}
}

// Install registers whatever the core says it handles, so the two can never
// drift apart into a Harness whose Steps are never seen, or a Nudge with no
// event to be delivered on. This runs for every Harness whoa claims to
// support, because a gap on the second one is exactly as silent as on the
// first.
func TestEveryHarnessCanObserveAndCanSpeak(t *testing.T) {
	for harness := range hookEvents {
		t.Run(string(harness), func(t *testing.T) {
			var records, speaks bool
			for _, name := range Events(harness) {
				event, ok := hookPayload{Event: name}.event(harness)
				if !ok {
					t.Errorf("registered %q but do not handle it", name)
				}
				records = records || event.Records
				speaks = speaks || (!event.Records && name != userPromptSubmit)
			}
			if !records {
				t.Error("no event records a Step, so whoa sees nothing")
			}
			if !speaks {
				t.Error("no event fires before a Step, so a Nudge has no way to reach the agent")
			}
		})
	}
}

// Claude Code is the Harness that settles the Outcome by which event fired, so
// both of those events have to be registered or its failures go unseen.
func TestClaudeCodeRegistersBothOutcomeEvents(t *testing.T) {
	seen := map[Outcome]bool{}
	for _, name := range Events(ClaudeCode) {
		event, _ := hookPayload{Event: name}.event(ClaudeCode)
		seen[event.Outcome] = true
	}
	if !seen[OutcomeOK] || !seen[OutcomeError] {
		t.Errorf("events cover %v, want both ok and error", seen)
	}
}

// An unknown Harness must fall through to the no-op, not panic on a nil map
// entry.
func TestUnknownHarnessObservesNothing(t *testing.T) {
	if got := Events("nope"); len(got) != 0 {
		t.Errorf("Events(nope) = %v, want none", got)
	}
	ob := Observe(Input{Raw: payload(t, "posttooluse_write"), Harness: "nope", Now: at})
	if ob.Entry != nil {
		t.Errorf("recorded a Step for an unknown Harness: %+v", ob.Entry)
	}
}
