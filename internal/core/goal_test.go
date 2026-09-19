package core

import (
	"testing"
	"time"
)

func prompt(harness Harness, text string) Input {
	raw := `{"session_id":"s","hook_event_name":"UserPromptSubmit","prompt":` + quote(text)
	if harness == Codex {
		raw += `,"turn_id":"t9"}`
	} else {
		raw += `,"prompt_id":"t9"}`
	}
	return Input{
		Raw:     []byte(raw),
		Harness: harness,
		Config:  Defaults(),
		Now:     time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
	}
}

func quote(s string) string {
	out := []byte{'"'}
	for _, r := range s {
		switch r {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		case '\n':
			out = append(out, '\\', 'n')
		default:
			out = append(out, string(r)...)
		}
	}
	return string(append(out, '"'))
}

func TestASubstantiveInstructionBecomesTheGoal(t *testing.T) {
	in := prompt(ClaudeCode, "Add retry logic to the payment client")

	got := Observe(in)

	if got.Entry == nil {
		t.Fatal("a Substantive Instruction must be recorded as the Goal")
	}
	if got.Entry.Kind != KindGoal {
		t.Errorf("kind = %q, want %q", got.Entry.Kind, KindGoal)
	}
	if got.Entry.Goal != "Add retry logic to the payment client" {
		t.Errorf("Goal = %q, want it stored as the user wrote it", got.Entry.Goal)
	}
	if got.Entry.Turn != "t9" {
		t.Errorf("Turn = %q, want the Harness turn key t9", got.Entry.Turn)
	}
	if len(got.Output) != 0 {
		t.Errorf("recording a Goal must not speak to the agent, got %s", got.Output)
	}
}

func TestAContinuationSignalLeavesTheGoalAlone(t *testing.T) {
	signals := []string{
		"continue", "Continue", "continue.", "go on", "keep going", "carry on",
		"proceed", "next", "yes", "ok", "sure", "please continue", "  continue  ",
		"繼續", "請繼續", "继续", "请继续", "好", "好的", "可以",
		"続けて", "はい", "계속", "sigue", "adelante", "weiter", "oui",
	}
	for _, s := range signals {
		got := Observe(prompt(ClaudeCode, s))
		if got.Entry != nil {
			t.Errorf("%q was treated as a Substantive Instruction; it is a Continuation Signal", s)
		}
	}
}

func TestAnInstructionThatMerelyStartsWithContinueIsSubstantive(t *testing.T) {
	substantive := []string{
		"continue with the refactor of the auth module",
		"go on to the next ticket and open a PR",
		"繼續做第二張票",
		"ok now delete the migration",
	}
	for _, s := range substantive {
		got := Observe(prompt(ClaudeCode, s))
		if got.Entry == nil {
			t.Errorf("%q carries new instruction and must replace the Goal", s)
		}
	}
}

func TestTheGoalIsStoredInWhateverLanguageItWasWritten(t *testing.T) {
	const goal = "把測試補齊，然後發 PR"

	got := Observe(prompt(ClaudeCode, goal))

	if got.Entry == nil || got.Entry.Goal != goal {
		t.Fatalf("Goal = %q, want %q byte for byte", got.Entry.Goal, goal)
	}
}

func TestClassificationWorksOnBothHarnesses(t *testing.T) {
	for _, h := range []Harness{ClaudeCode, Codex} {
		substantive := Observe(prompt(h, "Rewrite the README"))
		if substantive.Entry == nil {
			t.Errorf("%s: a Substantive Instruction was not recorded", h)
			continue
		}
		if substantive.Entry.Turn != "t9" {
			t.Errorf("%s: Turn = %q, want t9 from its own turn key", h, substantive.Entry.Turn)
		}
		if substantive.Entry.Harness != h {
			t.Errorf("%s: Harness = %q", h, substantive.Entry.Harness)
		}
		if got := Observe(prompt(h, "continue")); got.Entry != nil {
			t.Errorf("%s: a Continuation Signal was recorded as a Goal", h)
		}
	}
}

func TestAStepCarriesTheGoalInForce(t *testing.T) {
	in := Input{
		Raw:     []byte(`{"session_id":"s","prompt_id":"t2","hook_event_name":"PostToolUse","tool_name":"Edit"}`),
		Harness: ClaudeCode,
		Log: []Entry{
			{Kind: KindGoal, Turn: "t1", Goal: "Fix the flaky test"},
			{Kind: KindStep, Turn: "t1", Tool: "Bash", Outcome: OutcomeError},
		},
		Config: Defaults(),
		Now:    time.Now(),
	}

	got := Observe(in)

	if got.Entry == nil {
		t.Fatal("expected the Step to be recorded")
	}
	if got.Entry.Goal != "Fix the flaky test" {
		t.Errorf("Step.Goal = %q, want the Goal in force when it happened", got.Entry.Goal)
	}
}

func TestAContinuationSignalLeavesLaterStepsOnTheOriginalGoal(t *testing.T) {
	// The user said "continue", so the Turn moved on but the Goal did not.
	in := Input{
		Raw:     []byte(`{"session_id":"s","prompt_id":"t3","hook_event_name":"PostToolUse","tool_name":"Bash"}`),
		Harness: ClaudeCode,
		Log: []Entry{
			{Kind: KindGoal, Turn: "t1", Goal: "Fix the flaky test"},
			{Kind: KindStep, Turn: "t1", Tool: "Bash", Outcome: OutcomeError, Goal: "Fix the flaky test"},
		},
		Config: Defaults(),
		Now:    time.Now(),
	}

	if got := Observe(in); got.Entry.Goal != "Fix the flaky test" {
		t.Errorf("Goal after a Continuation Signal = %q, want it unchanged", got.Entry.Goal)
	}
}

func TestARetargetReplacesTheGoalForLaterSteps(t *testing.T) {
	in := Input{
		Raw:     []byte(`{"session_id":"s","prompt_id":"t5","hook_event_name":"PostToolUse","tool_name":"Bash"}`),
		Harness: ClaudeCode,
		Log: []Entry{
			{Kind: KindGoal, Turn: "t1", Goal: "Fix the flaky test"},
			{Kind: KindStep, Turn: "t1", Tool: "Bash", Outcome: OutcomeError, Goal: "Fix the flaky test"},
			{Kind: KindGoal, Turn: "t5", Goal: "Actually, go and fix CI instead"},
		},
		Config: Defaults(),
		Now:    time.Now(),
	}

	if got := Observe(in); got.Entry.Goal != "Actually, go and fix CI instead" {
		t.Errorf("Goal = %q, want the most recent Substantive Instruction", got.Entry.Goal)
	}
}

func TestAnEmptyPromptIsNotAGoal(t *testing.T) {
	for _, s := range []string{"", "   ", "\n"} {
		if got := Observe(prompt(ClaudeCode, s)); got.Entry != nil {
			t.Errorf("%q was recorded as a Goal", s)
		}
	}
}

func TestAGoalEntryDoesNotBreakACounterRun(t *testing.T) {
	entries := []Entry{
		step("Bash", OutcomeError),
		step("Bash", OutcomeError),
		{Kind: KindGoal, Goal: "continue is not a goal, but this is"},
		step("Bash", OutcomeError),
	}

	got := Count(entries, 50)

	if got.RepeatedFailures != 3 {
		t.Errorf("RepeatedFailures = %d, want 3: a Goal is not a Step and must not break the run", got.RepeatedFailures)
	}
	if got.Steps != 3 {
		t.Errorf("Steps = %d, want 3", got.Steps)
	}
}
