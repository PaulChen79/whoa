package core

import (
	"time"

	"github.com/PaulChen79/whoa/internal/redact"
)

// Kind says what a line of the Session log records.
//
// The log is one JSONL file per Session holding more than Steps: what whoa
// observed and what whoa did about it live in the same ordered stream, because
// a Verdict is only worth anything if it can be replayed against exactly the
// evidence that produced it.
type Kind string

const (
	KindStep  Kind = "step"
	KindNudge Kind = "nudge"
	// KindGoal is a Substantive Instruction: what the user asked for.
	KindGoal Kind = "goal"
	// KindWrong is a Misjudgment the user reported: the Verdict at Ref was
	// not right. It is the only entry a person writes, and the reason the
	// calibration corpus is a by-product of using whoa rather than a chore.
	KindWrong Kind = "wrong"
	// KindVerdict is what the Judge said. In Shadow Mode it is all whoa does.
	KindVerdict Kind = "verdict"
	// KindHalt is whoa denying one Step. It is the exceptional case, and it
	// is recorded so that backoff can be counted from the log alone.
	KindHalt Kind = "halt"
	// KindNotice is whoa saying something about itself: that it has degraded,
	// and why. It lives in the log so that "tell the user once" can be
	// answered from the same evidence as everything else.
	KindNotice Kind = "notice"
)

// Outcome is how a Step ended.
type Outcome string

const (
	OutcomeOK    Outcome = "ok"
	OutcomeError Outcome = "error"
)

// Entry is one line of the Session log.
//
// It is deliberately flat rather than a discriminated union with nested
// payloads: the log is meant to be read by a person with `jq`, or with their
// eyes, when they are deciding whether to trust this tool. The Kind says which
// fields are meaningful.
//
// No field holds free text from the agent's work. No command text, no error
// text, no file contents and no paths: those can carry secrets. Redaction is
// what makes any of that admissible, and only in the forms Redaction produces.
// The single exception is Goal, which is the user's own prompt: their words,
// which they chose to write, about work they chose to ask for.
type Entry struct {
	Kind      Kind      `json:"kind"`
	Timestamp time.Time `json:"ts"`
	Harness   Harness   `json:"harness,omitempty"`
	Session   string    `json:"session"`
	Turn      string    `json:"turn,omitempty"`

	// Goal is the user's own words. On a KindGoal Entry it is the Substantive
	// Instruction itself; on a Step it is the Goal that was in force when the
	// Step happened, so that one line can be read on its own.
	//
	// This is the one field that holds free text, and it is the user's text,
	// never the agent's work: what they asked for, in the language they asked
	// for it in. Nothing derived from a file, a command or an error goes here.
	Goal string `json:"goal,omitempty"`

	// Kind == KindWrong: which line of this log the Misjudgment disputes,
	// counting from 1.
	//
	// A line number rather than a timestamp, because timestamps are not
	// unique: two Steps inside the same millisecond are ordinary, and a
	// Misjudgment that could refer to either would mark both. The log is
	// append-only, so a line number never moves, and it reconstructs the
	// evidence exactly — everything before it is what whoa saw.
	//
	// Counting from 1 so that the zero value means "no reference", and so
	// that it matches what every tool that reads a file by line says.
	Ref int `json:"ref,omitempty"`

	// Kind == KindStep.
	Tool      string `json:"tool,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`

	// What Seam 2 was willing to keep from the tool's arguments. Command is
	// text only after Redaction (ADR 0003); Signals are counts and flags only
	// (ADR 0001). Together these are the whole of what a Step says about what
	// the agent actually did.
	Command    *redact.Redacted `json:"command,omitempty"`
	Signals    *redact.Signals  `json:"signals,omitempty"`
	Outcome    Outcome          `json:"outcome,omitempty"`
	DurationMS int              `json:"duration_ms,omitempty"`

	// Kind == KindVerdict: what the Judge answered, by question key, and
	// which Judge answered it.
	//
	// The Digest that produced this is not copied here. It is a pure function
	// of the log above this line, so Ref plus the log reconstructs it exactly,
	// and a copy could only introduce a version that disagrees with the
	// evidence it claims to be.
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	// Scores are the Judge's Score answers, kept apart from Probabilities
	// because a score on a legend is not a probability.
	Scores map[string]float64 `json:"scores,omitempty"`
	Model  string             `json:"model,omitempty"`

	// Kind == KindNudge: the Counter fact that fired, kept so that a Nudge can
	// be replayed, argued with, and marked as a Misjudgment later.
	Counter   string `json:"counter,omitempty"`
	Count     int    `json:"count,omitempty"`
	Threshold int    `json:"threshold,omitempty"`
	Fact      string `json:"fact,omitempty"`
}

// IsStep reports whether the Entry records a tool call.
//
// Entries whoa writes about its own interventions are not Steps, which is what
// keeps whoa from reading its own Nudges back as evidence that the agent is
// looping.
func (e Entry) IsStep() bool { return e.Kind == KindStep }
