package judge

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/PaulChen79/whoa/internal/core"
	"github.com/PaulChen79/whoa/internal/redact"
)

func at(min int) time.Time {
	return time.Date(2026, 9, 19, 10, min, 0, 0, time.UTC)
}

// recordedSession is a Loop that turns into an evasion: a test fails, the
// agent retries, then deletes the assertions instead.
func recordedSession() []core.Entry {
	return []core.Entry{
		{Kind: core.KindGoal, Timestamp: at(0), Session: "s", Turn: "t1", Goal: "修好 auth 的 flaky test"},
		{Kind: core.KindStep, Timestamp: at(1), Session: "s", Turn: "t1", Tool: "Bash",
			Outcome: core.OutcomeError, Goal: "修好 auth 的 flaky test",
			Command: &redact.Redacted{Text: "npm test"}},
		{Kind: core.KindStep, Timestamp: at(2), Session: "s", Turn: "t1", Tool: "Bash",
			Outcome: core.OutcomeError, Goal: "修好 auth 的 flaky test",
			Command: &redact.Redacted{Text: "npm test"}},
		{Kind: core.KindStep, Timestamp: at(3), Session: "s", Turn: "t1", Tool: "Edit",
			Outcome: core.OutcomeOK, Goal: "修好 auth 的 flaky test",
			Signals: &redact.Signals{TestFile: true, AssertionsRemoved: 2, SkipMarkersAdded: 1,
				LinesAdded: 1, LinesRemoved: 2}},
		{Kind: core.KindStep, Timestamp: at(4), Session: "s", Turn: "t1", Tool: "Bash",
			Outcome: core.OutcomeOK, Goal: "修好 auth 的 flaky test",
			Command: &redact.Redacted{Text: "cat", Degraded: true}},
	}
}

// The Digest is asserted whole, so that anything added to what whoa sends
// fails here before it reaches anyone's machine.
func TestTheDigestIsExactlyThis(t *testing.T) {
	cfg := core.Defaults()
	got, err := json.MarshalIndent(Assemble(recordedSession(), cfg).State, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	want := `{
  "goal": "修好 auth 的 flaky test",
  "steps": [
    {
      "tool": "Bash",
      "outcome": "error",
      "command": "npm test"
    },
    {
      "tool": "Bash",
      "outcome": "error",
      "command": "npm test"
    },
    {
      "tool": "Edit",
      "outcome": "ok",
      "signals": {
        "test_file": true,
        "assertions_removed": 2,
        "skip_markers_added": 1,
        "lines_added": 1,
        "lines_removed": 2
      }
    },
    {
      "tool": "Bash",
      "outcome": "ok",
      "command": "cat",
      "command_degraded": true
    }
  ],
  "counters": {
    "steps": 4,
    "tool": "Bash",
    "repeated_failures": 0,
    "repeated_tool": 1,
    "failed_steps": 2
  },
  "totals": {
    "edited_steps": 1,
    "test_file_edits": 1,
    "assertions_removed": 2,
    "skip_markers_added": 1
  }
}`
	if string(got) != want {
		t.Errorf("the Digest changed. This is the whole of what leaves the machine.\ngot:\n%s\n\nwant:\n%s", got, want)
	}
}

// The Goal is the user's own words, in their language. Everything whoa says
// about it is in English, because the Judge is weaker in CJK and cannot
// translate.
func TestEveryInstructionIsInEnglish(t *testing.T) {
	for _, q := range Questions() {
		text := q.Instructions.What + q.Instructions.NotFor + strings.Join(q.Instructions.Examples, "")
		for _, level := range q.Legend {
			text += level.Label
		}
		for _, r := range text {
			if r > unicode.MaxASCII {
				t.Errorf("%s: instruction contains %q; instructions are English whatever the Goal is", q.Key, r)
				break
			}
		}
	}
}

func TestTheQuestionSetIsTheFiveAgreed(t *testing.T) {
	want := []string{"same_intent", "goal_drift", "evading", "needs_human", "progress"}
	questions := Questions()
	if len(questions) != len(want) {
		t.Fatalf("asked %d questions, want %d", len(questions), len(want))
	}
	for i, q := range questions {
		if q.Key != want[i] {
			t.Errorf("question %d is %q, want %q", i, q.Key, want[i])
		}
		if q.Instructions.What == "" {
			t.Errorf("%s: no instructions", q.Key)
		}
	}
}

// A level count without a legend does not define a question: the returned
// value can land between levels, and nothing says what the space between two
// numbers means.
func TestProgressShipsAnOrderedLegend(t *testing.T) {
	var progress Question
	for _, q := range Questions() {
		if q.Key == "progress" {
			progress = q
		}
	}
	if progress.Kind != Score {
		t.Fatalf("progress is %q, want a score", progress.Kind)
	}
	if len(progress.Legend) < 2 || len(progress.Legend) > 10 {
		t.Errorf("legend has %d levels; the Judge accepts 2 to 10", len(progress.Legend))
	}
	for i, level := range progress.Legend {
		if level.Value != i+1 {
			t.Errorf("legend level %d has value %d; levels must be ordered and contiguous", i, level.Value)
		}
		if len(level.Label) < 10 {
			t.Errorf("legend level %d has no meaningful label: %q", level.Value, level.Label)
		}
	}
}

// Descriptive, never predictive: a calibrated classifier is reliable about
// what is in front of it and much less so about what happens next.
func TestNeedsHumanIsAnObservationNotAPrediction(t *testing.T) {
	var q Question
	for _, candidate := range Questions() {
		if candidate.Key == "needs_human" {
			q = candidate
		}
	}
	if !strings.Contains(q.Instructions.What, "steps show") {
		t.Errorf("needs_human is not phrased as an observation: %q", q.Instructions.What)
	}
	if !strings.Contains(q.Instructions.NotFor, "Not a prediction") {
		t.Errorf("needs_human does not rule out predicting: %q", q.Instructions.NotFor)
	}
	for _, word := range []string{"will need", "is likely to", "going to fail"} {
		if strings.Contains(q.Instructions.What, word) {
			t.Errorf("needs_human predicts (%q): %q", word, q.Instructions.What)
		}
	}
}

// The Judge takes about 64k tokens for the state plus every question, and
// about 32k for the state plus the longest single question.
func TestTheRequestFitsTheJudgesLimits(t *testing.T) {
	cfg := core.Defaults()
	log := make([]core.Entry, 0, 200)
	log = append(log, core.Entry{Kind: core.KindGoal, Timestamp: at(0), Goal: strings.Repeat("goal ", 2000)})
	for i := 0; i < 200; i++ {
		log = append(log, core.Entry{Kind: core.KindStep, Timestamp: at(i), Tool: "Bash",
			Outcome: core.OutcomeError,
			Command: &redact.Redacted{Text: strings.Repeat("x", 200)}})
	}

	request := Assemble(log, cfg)
	if got := EstimatedTokens(request); got > 64_000 {
		t.Errorf("request is ~%d tokens, over the 64k budget", got)
	}
	if n := len(request.Questions); n > 32 {
		t.Errorf("%d questions, over the limit of 32", n)
	}

	state := EstimatedTokens(request.State)
	for _, q := range request.Questions {
		if got := state + EstimatedTokens(q); got > 32_000 {
			t.Errorf("state plus %s is ~%d tokens, over the 32k single-question budget", q.Key, got)
		}
	}
}

// An enormous instruction must not turn every Verdict into a context error.
func TestAnEnormousGoalIsBounded(t *testing.T) {
	log := []core.Entry{{Kind: core.KindGoal, Timestamp: at(0), Goal: strings.Repeat("a", 10_000)}}
	got := Assemble(log, core.Defaults()).State
	if len(got.Goal) > maxGoalChars {
		t.Errorf("Goal is %d characters, want it bounded at %d", len(got.Goal), maxGoalChars)
	}
	if !got.GoalTruncated {
		t.Error("the Goal was cut without saying so")
	}
}

// The Digest describes the same Steps the Counters were computed over. If they
// could disagree, a Verdict would be arguing with its own evidence.
func TestTheStepsShownAreTheStepsCounted(t *testing.T) {
	cfg := core.Defaults()
	cfg.Window = 3
	log := recordedSession()
	got := Assemble(log, cfg).State
	if len(got.Steps) != got.Counters.Steps {
		t.Errorf("showed %d Steps but counted %d", len(got.Steps), got.Counters.Steps)
	}
	if len(got.Steps) != 3 {
		t.Errorf("Window is 3 but %d Steps were sent", len(got.Steps))
	}
}

// The Digest is built only from what Seam 2 already allowed through, so this
// should be impossible. It is asserted anyway, because "impossible by
// construction" is exactly the kind of claim that stops being true quietly.
func TestTheDigestCarriesNoSourceAndNoRawCommand(t *testing.T) {
	const secret = "sk-abcdefghijklmnopqrstuvwxyz0123"
	const source = "func Authenticate(user string) error { return nil }"

	raw := `{"file_path":"/Users/paul/app/auth_test.go",` +
		`"old_string":"` + source + ` expect(x).toBe(1)",` +
		`"new_string":"it.skip('x')"}`
	edit := redact.FromToolInput([]byte(raw))
	shell := redact.FromToolInput([]byte(`{"command":"deploy --token ` + secret + `"}`))

	log := []core.Entry{
		{Kind: core.KindGoal, Timestamp: at(0), Goal: "fix auth"},
		{Kind: core.KindStep, Timestamp: at(1), Tool: "Edit", Outcome: core.OutcomeOK, Signals: &edit.Signals},
		{Kind: core.KindStep, Timestamp: at(2), Tool: "Bash", Outcome: core.OutcomeError, Command: &shell.Command},
	}

	sent, err := json.Marshal(Assemble(log, core.Defaults()))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{secret, source, "auth_test.go", "/Users/paul", "it.skip"} {
		if strings.Contains(string(sent), leak) {
			t.Errorf("the Digest would send %q:\n%s", leak, sent)
		}
	}
	if !strings.Contains(string(sent), `"skip_markers_added":1`) {
		t.Errorf("the Signal that matters was lost on the way to the Digest:\n%s", sent)
	}
}
