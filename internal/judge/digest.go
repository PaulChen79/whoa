package judge

import (
	"encoding/json"

	"github.com/PaulChen79/whoa/internal/core"
	"github.com/PaulChen79/whoa/internal/redact"
)

// Request is the complete payload that would be sent to the Judge. There is
// nothing else: no headers of interest, no second call, no side channel.
type Request struct {
	State     Digest     `json:"state"`
	Questions []Question `json:"questions"`
}

// Digest is everything the Judge is told about the Session.
//
// Its fields are the whole of what leaves the machine, which is why the type
// is small and why the tests assert it in full. A field added here is a
// promise broken, so adding one should fail a test.
type Digest struct {
	// Goal is the user's own words, the current Substantive Instruction. It
	// passes through untranslated; see Questions.
	Goal string `json:"goal"`
	// GoalTruncated says the Goal was too long to send whole.
	GoalTruncated bool `json:"goal_truncated,omitempty"`
	// Steps is the Window, oldest first.
	Steps    []StepSummary `json:"steps"`
	Counters core.Counters `json:"counters"`
	Totals   Totals        `json:"totals"`
}

// StepSummary is one Step as the Judge sees it.
type StepSummary struct {
	Tool     string       `json:"tool"`
	Outcome  core.Outcome `json:"outcome"`
	Command  string       `json:"command,omitempty"`
	Degraded bool         `json:"command_degraded,omitempty"`
	// Signals are counts and flags only; see package redact.
	Signals *redact.Signals `json:"signals,omitempty"`
}

// Totals are the Signals of the whole Window added up, so that the Judge can
// see a pattern the individual Steps only hint at.
type Totals struct {
	EditedSteps       int `json:"edited_steps,omitempty"`
	TestFileEdits     int `json:"test_file_edits,omitempty"`
	AssertionsAdded   int `json:"assertions_added,omitempty"`
	AssertionsRemoved int `json:"assertions_removed,omitempty"`
	SkipMarkersAdded  int `json:"skip_markers_added,omitempty"`
}

// NetAssertionDelta is the direction the tests moved over the whole Window.
func (t Totals) NetAssertionDelta() int { return t.AssertionsAdded - t.AssertionsRemoved }

// maxGoalChars bounds the one free-text field so that an enormous instruction
// cannot push the request past the Judge's context limit and turn every
// Verdict into an error.
const maxGoalChars = 4000

// Assemble builds the Request that would be sent for a Session log.
//
// It reads only the log, so what it produces can be reproduced from the log
// later, which is what makes a disputed Verdict arguable.
func Assemble(log []core.Entry, cfg core.Config) Request {
	window := core.Window(log, cfg.Window)

	digest := Digest{
		Goal:     goalOf(log),
		Counters: core.Count(log, cfg.Window),
		Steps:    make([]StepSummary, 0, len(window)),
	}
	if len(digest.Goal) > maxGoalChars {
		digest.Goal = digest.Goal[:maxGoalChars]
		digest.GoalTruncated = true
	}

	for _, e := range window {
		s := StepSummary{Tool: e.Tool, Outcome: e.Outcome, Signals: e.Signals}
		if e.Command != nil {
			s.Command, s.Degraded = e.Command.Text, e.Command.Degraded
		}
		digest.Steps = append(digest.Steps, s)

		if e.Signals != nil {
			digest.Totals.EditedSteps++
			if e.Signals.TestFile {
				digest.Totals.TestFileEdits++
			}
			digest.Totals.AssertionsAdded += e.Signals.AssertionsAdded
			digest.Totals.AssertionsRemoved += e.Signals.AssertionsRemoved
			digest.Totals.SkipMarkersAdded += e.Signals.SkipMarkersAdded
		}
	}

	return Request{State: digest, Questions: Questions()}
}

// goalOf is the Goal in force at the end of the log.
func goalOf(log []core.Entry) string {
	for i := len(log) - 1; i >= 0; i-- {
		if log[i].Kind == core.KindGoal {
			return log[i].Goal
		}
		if log[i].Goal != "" {
			return log[i].Goal
		}
	}
	return ""
}

// EstimatedTokens is a deliberately rough size, for checking the request fits.
//
// Four characters to a token is the usual English approximation and it is
// enough: the limits being checked are 32k and 64k, and whoa's requests are
// two orders of magnitude below them. Anything close enough for the estimate's
// error to matter is already a bug.
func EstimatedTokens(v any) int {
	raw, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(raw)/4 + 1
}
