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
type Entry struct {
	Kind      Kind      `json:"kind"`
	Timestamp time.Time `json:"ts"`
	Harness   Harness   `json:"harness,omitempty"`
	Session   string    `json:"session"`
	Turn      string    `json:"turn,omitempty"`

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
