package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func preToolUse(session string) []byte {
	return []byte(`{"session_id":"` + session + `","hook_event_name":"PreToolUse","tool_name":"Bash","prompt_id":"t1"}`)
}

// nudgeConfig is the default configuration with a small, obvious trigger, so
// the tests read as "three failures" rather than "the default happens to be 3".
func nudgeConfig() Config {
	c := Defaults()
	c.Trigger = Trigger{RepeatedFailures: 3}
	return c
}

func failures(n int) []Entry {
	var entries []Entry
	for i := 0; i < n; i++ {
		entries = append(entries, Entry{Kind: KindStep, Session: "s", Tool: "Bash", Outcome: OutcomeError})
	}
	return entries
}

// agentContext digs the string whoa put in the agent's context out of the hook
// output, so the tests assert on what the agent actually receives.
func agentContext(t *testing.T, out []byte) string {
	t.Helper()
	if len(out) == 0 {
		return ""
	}
	var parsed struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
		SystemMessage string `json:"systemMessage"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("hook output is not valid JSON: %v\n%s", err, out)
	}
	if parsed.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q, want PreToolUse", parsed.HookSpecificOutput.HookEventName)
	}
	return parsed.HookSpecificOutput.AdditionalContext
}

func TestNudgeFiresWhenTheTriggerHolds(t *testing.T) {
	ob := Observe(Input{
		Raw:     preToolUse("s"),
		Harness: ClaudeCode,
		Log:     failures(3),
		Config:  nudgeConfig(),
		Now:     time.Unix(0, 0),
	})

	got := agentContext(t, ob.Output)
	if got == "" {
		t.Fatal("no Nudge reached the agent after three repeated failures")
	}
	// The Nudge must state the fact that fired it, or the agent has nothing to
	// act on but an accusation.
	if !strings.Contains(got, "3") || !strings.Contains(got, "Bash") {
		t.Errorf("Nudge does not state the Counter fact that fired it: %q", got)
	}
	if ob.Entry == nil || ob.Entry.Kind != KindNudge {
		t.Fatalf("the Nudge was not recorded in the log: %+v", ob.Entry)
	}
	if ob.Entry.Counter != "repeated_failures" || ob.Entry.Count != 3 {
		t.Errorf("recorded Nudge = %+v, want counter repeated_failures at 3", ob.Entry)
	}
}

func TestNudgeStaysSilentBelowTheTrigger(t *testing.T) {
	ob := Observe(Input{
		Raw: preToolUse("s"), Harness: ClaudeCode,
		Log: failures(2), Config: nudgeConfig(), Now: time.Unix(0, 0),
	})
	if len(ob.Output) != 0 {
		t.Errorf("Nudged at two failures with a trigger of three: %s", ob.Output)
	}
	if ob.Entry != nil {
		t.Errorf("recorded something at two failures: %+v", ob.Entry)
	}
}

// The agent is never shown a probability. It argues with the number instead of
// changing course, and in Counter-only Mode there is no probability anyway.
func TestTheAgentIsNeverShownAProbability(t *testing.T) {
	ob := Observe(Input{
		Raw: preToolUse("s"), Harness: ClaudeCode,
		Log: failures(4), Config: nudgeConfig(), Now: time.Unix(0, 0),
	})
	got := agentContext(t, ob.Output)
	for _, forbidden := range []string{"0.", "%", "probability", "confidence", "likelihood"} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Errorf("the agent's Nudge contains %q: %q", forbidden, got)
		}
	}
}

func TestTheHumanGetsOneLineAndNotifyMutesIt(t *testing.T) {
	in := Input{
		Raw: preToolUse("s"), Harness: ClaudeCode,
		Log: failures(3), Config: nudgeConfig(), Now: time.Unix(0, 0),
	}

	loud := Observe(in)
	if loud.Human == "" {
		t.Fatal("the human was told nothing")
	}
	if strings.Contains(strings.TrimSpace(loud.Human), "\n") {
		t.Errorf("the human's message is more than one line: %q", loud.Human)
	}

	in.Config.Notify = false
	quiet := Observe(in)
	if quiet.Human != "" {
		t.Errorf("notify = false still spoke to the human: %q", quiet.Human)
	}
	if agentContext(t, quiet.Output) == "" {
		t.Error("notify = false also silenced the agent's Nudge, which it must not")
	}
}

func TestTheTriggerIsTunableWithoutCodeChanges(t *testing.T) {
	in := Input{
		Raw: preToolUse("s"), Harness: ClaudeCode,
		Log: failures(5), Config: nudgeConfig(), Now: time.Unix(0, 0),
	}
	in.Config.Trigger = Trigger{RepeatedFailures: 9}
	if len(Observe(in).Output) != 0 {
		t.Error("raising the trigger to nine did not stop a Nudge at five failures")
	}
	in.Config.Trigger = Trigger{RepeatedFailures: 2}
	if len(Observe(in).Output) == 0 {
		t.Error("lowering the trigger to two did not produce a Nudge at five failures")
	}
}

func TestTheWindowIsTunableWithoutCodeChanges(t *testing.T) {
	entries := append(failures(3), Entry{Kind: KindStep, Tool: "Edit", Outcome: OutcomeOK})
	in := Input{
		Raw: preToolUse("s"), Harness: ClaudeCode,
		Log: entries, Config: nudgeConfig(), Now: time.Unix(0, 0),
	}
	in.Config.Window = 1
	if len(Observe(in).Output) != 0 {
		t.Error("a Window of one still saw failures that had fallen off the end")
	}
}

// A Nudge the agent ignored must not be repeated on every following Step, or
// whoa becomes the noise it exists to prevent.
func TestWhoaDoesNotRepeatItselfAboutAnUnchangedSituation(t *testing.T) {
	entries := append(failures(3), Entry{
		Kind: KindNudge, Counter: "repeated_failures", Count: 3, Threshold: 3,
	})
	in := Input{
		Raw: preToolUse("s"), Harness: ClaudeCode,
		Log: entries, Config: nudgeConfig(), Now: time.Unix(0, 0),
	}
	if len(Observe(in).Output) != 0 {
		t.Error("Nudged twice about the same three failures")
	}

	// Another full trigger's worth of failures is a new situation.
	in.Log = append(entries, failures(3)...)
	if len(Observe(in).Output) == 0 {
		t.Error("stayed silent after three further failures")
	}
}

// The agent breaking out of the Loop and falling back into one later is a new
// situation, not a continuation of the old one.
func TestANudgeCanFireAgainAfterTheCounterResets(t *testing.T) {
	entries := append(failures(3), Entry{
		Kind: KindNudge, Counter: "repeated_failures", Count: 3, Threshold: 3,
	})
	entries = append(entries, Entry{Kind: KindStep, Tool: "Bash", Outcome: OutcomeOK})
	entries = append(entries, failures(3)...)

	ob := Observe(Input{
		Raw: preToolUse("s"), Harness: ClaudeCode,
		Log: entries, Config: nudgeConfig(), Now: time.Unix(0, 0),
	})
	if len(ob.Output) == 0 {
		t.Error("a Loop that restarted after a success produced no Nudge")
	}
}

func TestCounterOnlyModeNudgesAndOtherModesDoNotYet(t *testing.T) {
	in := Input{
		Raw: preToolUse("s"), Harness: ClaudeCode,
		Log: failures(3), Config: nudgeConfig(), Now: time.Unix(0, 0),
	}
	in.Config.Mode = ModeShadow
	if len(Observe(in).Output) != 0 {
		t.Error("Shadow Mode intervened, and it must never intervene")
	}
}

func TestDefaultsMatchTheDocumentedParameters(t *testing.T) {
	d := Defaults()
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"mode", d.Mode, ModeCounters},
		{"window", d.Window, 50},
		{"nudge_threshold", d.NudgeThreshold, 0.60},
		{"halt_threshold", d.HaltThreshold, 0.88},
		{"halt_enabled", d.HaltEnabled, true},
		{"ineffective_nudges_before_halt", d.IneffectiveNudgesBeforeHalt, 3},
		{"notify", d.Notify, true},
		{"model", d.Model, "jev-latest"},
		{"retention_days", d.RetentionDays, 14},
		{"on_missing_key", d.OnMissingKey, OnMissingKeyDegrade},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("default %s = %v, want %v (docs/adr/0004)", c.name, c.got, c.want)
		}
	}
	if d.Trigger == (Trigger{}) {
		t.Error("the default trigger fires on nothing, so whoa ships inert")
	}
}

// Each Counter must be able to fire on its own and name itself, or a Nudge
// tells the agent something it cannot act on.
func TestEachCounterCanFireAndNamesItself(t *testing.T) {
	tests := []struct {
		name    string
		trigger Trigger
		log     []Entry
		counter string
		says    string
	}{
		{
			name:    "repeated_failures",
			trigger: Trigger{RepeatedFailures: 3},
			log:     failures(3),
			counter: "repeated_failures",
			says:    "failed 3 times in a row",
		},
		{
			name:    "repeated_tool",
			trigger: Trigger{RepeatedTool: 3},
			log: []Entry{
				{Kind: KindStep, Tool: "Read", Outcome: OutcomeOK},
				{Kind: KindStep, Tool: "Read", Outcome: OutcomeOK},
				{Kind: KindStep, Tool: "Read", Outcome: OutcomeOK},
			},
			counter: "repeated_tool",
			says:    "last 3 steps have all used Read",
		},
		{
			name:    "failed_steps",
			trigger: Trigger{FailedSteps: 3},
			log: []Entry{
				{Kind: KindStep, Tool: "Bash", Outcome: OutcomeError},
				{Kind: KindStep, Tool: "Edit", Outcome: OutcomeError},
				{Kind: KindStep, Tool: "Read", Outcome: OutcomeError},
			},
			counter: "failed_steps",
			says:    "3 of the last 3 steps have failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Defaults()
			cfg.Trigger = tt.trigger
			ob := Observe(Input{
				Raw: preToolUse("s"), Harness: ClaudeCode,
				Log: tt.log, Config: cfg, Now: time.Unix(0, 0),
			})
			if ob.Entry == nil {
				t.Fatal("no Nudge fired")
			}
			if ob.Entry.Counter != tt.counter {
				t.Errorf("counter = %q, want %q", ob.Entry.Counter, tt.counter)
			}
			if !strings.Contains(agentContext(t, ob.Output), tt.says) {
				t.Errorf("the Nudge does not state its fact: %q", ob.Entry.Fact)
			}
		})
	}
}

// A Trigger of all zeroes is an inert whoa rather than one that fires on
// everything.
func TestAZeroTriggerFiresOnNothing(t *testing.T) {
	cfg := Defaults()
	cfg.Trigger = Trigger{}
	ob := Observe(Input{
		Raw: preToolUse("s"), Harness: ClaudeCode,
		Log: failures(99), Config: cfg, Now: time.Unix(0, 0),
	})
	if ob.Entry != nil || len(ob.Output) != 0 {
		t.Errorf("a zero trigger fired: %+v", ob)
	}
}

func TestSessionIDReadsThePayloadAndSurvivesRubbish(t *testing.T) {
	if got := SessionID(preToolUse("abc123")); got != "abc123" {
		t.Errorf("SessionID() = %q, want abc123", got)
	}
	if got := SessionID([]byte("not json")); got != "" {
		t.Errorf("SessionID() on rubbish = %q, want empty", got)
	}
}

// A Misjudgment report is only worth anything if the Verdict it disputes can be
// reproduced exactly. Replaying the same log must yield byte-identical output,
// not merely a Nudge that happens to fire again.
func TestReplayingALogReproducesTheIdenticalVerdict(t *testing.T) {
	in := Input{
		Raw:     preToolUse("s"),
		Harness: ClaudeCode,
		Log:     failures(4),
		Config:  nudgeConfig(),
		Now:     time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
	}

	first := Observe(in)
	if len(first.Output) == 0 {
		t.Fatal("expected a Nudge to replay")
	}
	for i := 0; i < 100; i++ {
		got := Observe(in)
		if string(got.Output) != string(first.Output) {
			t.Fatalf("replay %d output = %s, want %s", i, got.Output, first.Output)
		}
		if (got.Entry == nil) != (first.Entry == nil) {
			t.Fatalf("replay %d disagreed about whether to record an Entry", i)
		}
		if got.Entry != nil && *got.Entry != *first.Entry {
			t.Fatalf("replay %d entry = %+v, want %+v", i, *got.Entry, *first.Entry)
		}
	}
}
