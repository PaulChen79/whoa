package core

import "time"

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

	// Kind == KindStep.
	Tool       string  `json:"tool,omitempty"`
	ToolUseID  string  `json:"tool_use_id,omitempty"`
	AgentID    string  `json:"agent_id,omitempty"`
	Outcome    Outcome `json:"outcome,omitempty"`
	DurationMS int     `json:"duration_ms,omitempty"`

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
