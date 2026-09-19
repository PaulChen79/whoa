package core

import (
	"encoding/json"
	"time"
)

// Harness is the host program running the coding agent.
type Harness string

const (
	ClaudeCode Harness = "claude-code"
	Codex      Harness = "codex"
)

// Outcome is how a Step ended.
type Outcome string

const (
	OutcomeOK    Outcome = "ok"
	OutcomeError Outcome = "error"
)

// hookEvent is one event a Harness fires, and the Outcome it reports.
type hookEvent struct {
	Name    string
	Outcome Outcome
}

// hookEvents is the single place that knows which events whoa registers for
// and what each one means. Install reads it through Events rather than keeping
// a second copy, so adding a Harness is one edit here.
//
// Claude Code reports a successful tool call and a failed one on two different
// events. Loop detection counts repeated failures, so registering only the
// success event would leave whoa blind to exactly the Steps that matter most.
var hookEvents = map[Harness][]hookEvent{
	ClaudeCode: {
		{Name: "PostToolUse", Outcome: OutcomeOK},
		{Name: "PostToolUseFailure", Outcome: OutcomeError},
	},
}

// Events lists the hook events whoa must register for on a Harness, in the
// order it registers them.
func Events(h Harness) []string {
	names := make([]string, 0, len(hookEvents[h]))
	for _, e := range hookEvents[h] {
		names = append(names, e.Name)
	}
	return names
}

// Step is one tool call, as whoa records it.
//
// Every field is structured. No command text, no error text, no file contents
// and no paths: those can carry secrets, and Redaction does not exist yet.
// When it does, what it produces will be Signals, not free text.
type Step struct {
	Timestamp  time.Time `json:"ts"`
	Harness    Harness   `json:"harness"`
	Session    string    `json:"session"`
	Turn       string    `json:"turn,omitempty"`
	Tool       string    `json:"tool"`
	ToolUseID  string    `json:"tool_use_id,omitempty"`
	AgentID    string    `json:"agent_id,omitempty"`
	Outcome    Outcome   `json:"outcome"`
	DurationMS int       `json:"duration_ms,omitempty"`
}

// Config is the subset of whoa's Parameters the core reads.
//
// It is empty while observation is unconditional. The Window, the trigger and
// the thresholds land here when Counters arrive; the core's signature already
// carries it so that they can, without reworking every test.
type Config struct{}

// Input is everything the core needs in order to decide. The shell gathers it;
// the core touches nothing else.
//
// Raw is the payload rather than a normalised Step on purpose, so that the
// differences between Harnesses are covered by tests through this seam instead
// of needing a seam of their own.
//
// Log is the Session so far. Nothing reads it yet: observation depends only on
// the Step in hand, while Counters depend on the ones before it.
type Input struct {
	Raw     []byte
	Harness Harness
	Log     []Step
	Config  Config
	Now     time.Time
}

// Observation is what the core tells the caller to do: what to append to the
// Session log, and what to write to stdout for the Harness.
//
// A zero Observation is the no-op, and it is what every path that cannot make
// sense of its input returns.
type Observation struct {
	Step   *Step
	Output []byte
}

// hookPayload is the subset of a Harness hook payload whoa reads. Fields it
// does not name are ignored, so a Harness adding fields cannot break parsing.
type hookPayload struct {
	SessionID  string `json:"session_id"`
	PromptID   string `json:"prompt_id"`
	TurnID     string `json:"turn_id"`
	Event      string `json:"hook_event_name"`
	ToolName   string `json:"tool_name"`
	ToolUseID  string `json:"tool_use_id"`
	AgentID    string `json:"agent_id"`
	DurationMS int    `json:"duration_ms"`
}

// outcome reports how the Step ended, and whether this is an event whoa
// observes at all.
func (p hookPayload) outcome(h Harness) (Outcome, bool) {
	for _, e := range hookEvents[h] {
		if e.Name == p.Event {
			return e.Outcome, true
		}
	}
	return "", false
}

// turnKey reads whichever turn identifier the Harness supplies. It is absent
// before the first user prompt, which is not an error.
func (p hookPayload) turnKey() string {
	if p.PromptID != "" {
		return p.PromptID
	}
	return p.TurnID
}

// Observe turns a raw Harness hook payload into an Observation.
//
// It is pure: no files, no network, no clock. That is what lets its tests be
// data in, data out, with no test doubles, and it is the seam every
// behavioural test goes through.
func Observe(in Input) Observation {
	var p hookPayload
	if err := json.Unmarshal(in.Raw, &p); err != nil {
		return Observation{}
	}

	outcome, ok := p.outcome(in.Harness)
	if !ok {
		return Observation{}
	}
	// A Step whoa cannot attribute to a Session and a tool is not a Step it
	// can reason about later, so there is no point recording it.
	if p.SessionID == "" || p.ToolName == "" {
		return Observation{}
	}

	return Observation{Step: &Step{
		Timestamp:  in.Now.UTC(),
		Harness:    in.Harness,
		Session:    p.SessionID,
		Turn:       p.turnKey(),
		Tool:       p.ToolName,
		ToolUseID:  p.ToolUseID,
		AgentID:    p.AgentID,
		Outcome:    outcome,
		DurationMS: p.DurationMS,
	}}
}
