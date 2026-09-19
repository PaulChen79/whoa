package core

import (
	"encoding/json"
	"github.com/PaulChen79/whoa/internal/redact"
	"time"
)

// Harness is the host program running the coding agent.
type Harness string

const (
	ClaudeCode Harness = "claude-code"
	Codex      Harness = "codex"
)

// hookEvent is one event a Harness fires and what whoa uses it for.
//
// An event either records a Step that has finished, or it is the moment before
// the next Step where whoa can still say something. Nothing else is needed to
// place an event, so nothing else is stored here.
type hookEvent struct {
	Name string
	// Records is the Outcome this event reports, or "" when the event fires
	// before the tool runs and records nothing.
	Records Outcome
}

// hookEvents is the single place that knows which events whoa registers for
// and what each one means. Install reads it through Events rather than keeping
// a second copy, so adding a Harness is one edit here.
//
// Claude Code reports a successful tool call and a failed one on two different
// events. Loop detection counts repeated failures, so registering only the
// success event would leave whoa blind to exactly the Steps that matter most.
//
// PreToolUse carries no Outcome. It is the Nudge channel: whoa observes on the
// post events and speaks on the pre event, so a Nudge lands in the agent's
// context before its next Step rather than after the one that earned it.
var hookEvents = map[Harness][]hookEvent{
	ClaudeCode: {
		{Name: userPromptSubmit},
		{Name: "PreToolUse"},
		{Name: "PostToolUse", Records: OutcomeOK},
		{Name: "PostToolUseFailure", Records: OutcomeError},
	},
}

// userPromptSubmit is handled outside the per-Harness table because it is
// genuinely the same event everywhere: both Harnesses fire it under this name
// and carry the user's text in the same field. Duplicating it per Harness
// would invite them to drift apart in code when they have not in fact.
const userPromptSubmit = "UserPromptSubmit"

// Events lists the hook events whoa must register for on a Harness, in the
// order it registers them.
func Events(h Harness) []string {
	names := make([]string, 0, len(hookEvents[h]))
	for _, e := range hookEvents[h] {
		names = append(names, e.Name)
	}
	return names
}

// Input is everything the core needs in order to decide. The shell gathers it;
// the core touches nothing else.
//
// Raw is the payload rather than a normalised Entry on purpose, so that the
// differences between Harnesses are covered by tests through this seam instead
// of needing a seam of their own.
type Input struct {
	Raw     []byte
	Harness Harness
	// Log is the Session so far, oldest first, as the shell read it back.
	Log    []Entry
	Config Config
	Now    time.Time
}

// Observation is what the core tells the caller to do: what to append to the
// Session log, what to write to stdout for the Harness, and what to show the
// human.
//
// A zero Observation is the no-op, and it is what every path that cannot make
// sense of its input returns.
type Observation struct {
	Entry  *Entry
	Output []byte
	// Human is the one line for the person watching, empty when there is
	// nothing to say or when notify is off.
	Human string
}

// hookPayload is the subset of a Harness hook payload whoa reads. Fields it
// does not name are ignored, so a Harness adding fields cannot break parsing.
type hookPayload struct {
	SessionID  string          `json:"session_id"`
	PromptID   string          `json:"prompt_id"`
	TurnID     string          `json:"turn_id"`
	Event      string          `json:"hook_event_name"`
	ToolName   string          `json:"tool_name"`
	ToolUseID  string          `json:"tool_use_id"`
	ToolInput  json.RawMessage `json:"tool_input"`
	Prompt     string          `json:"prompt"`
	AgentID    string          `json:"agent_id"`
	DurationMS int             `json:"duration_ms"`
}

// event finds the hookEvent this payload belongs to, if whoa registered for it.
func (p hookPayload) event(h Harness) (hookEvent, bool) {
	for _, e := range hookEvents[h] {
		if e.Name == p.Event {
			return e, true
		}
	}
	return hookEvent{}, false
}

// turnKey reads whichever turn identifier the Harness supplies. It is absent
// before the first user prompt, which is not an error.
func (p hookPayload) turnKey() string {
	if p.PromptID != "" {
		return p.PromptID
	}
	return p.TurnID
}

// SessionID reads just the Session a payload belongs to.
//
// The shell needs this before it can load the Session log, which the core then
// needs in order to decide. Knowledge of the payload shape stays in the core
// rather than leaking into the shell for the sake of one field.
func SessionID(raw []byte) string {
	var p hookPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return ""
	}
	return p.SessionID
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

	// The user speaking is the one event that is not about a tool, so it is
	// answered before anything asks which tool this was.
	if p.Event == userPromptSubmit {
		if p.SessionID == "" {
			return Observation{}
		}
		return in.observeGoal(p)
	}

	event, ok := p.event(in.Harness)
	if !ok {
		return Observation{}
	}
	// A Step whoa cannot attribute to a Session and a tool is not a Step it
	// can reason about later, so there is no point recording it.
	if p.SessionID == "" || p.ToolName == "" {
		return Observation{}
	}

	if event.Records == "" {
		return in.intervene(p)
	}

	command, signals := seam2(p.ToolInput)

	return Observation{Entry: &Entry{
		Kind:       KindStep,
		Timestamp:  in.Now.UTC(),
		Harness:    in.Harness,
		Session:    p.SessionID,
		Turn:       p.turnKey(),
		Tool:       p.ToolName,
		ToolUseID:  p.ToolUseID,
		Command:    command,
		Signals:    signals,
		AgentID:    p.AgentID,
		Goal:       goalInForce(in.Log),
		Outcome:    event.Records,
		DurationMS: p.DurationMS,
	}}
}

// seam2 is the only way tool arguments enter a Step. Whatever it does not
// return is discarded: see package redact, and ADRs 0001 and 0003.
//
// Both results are pointers so that a Step which touched nothing whoa
// understands carries no empty objects in the log.
func seam2(input json.RawMessage) (*redact.Redacted, *redact.Signals) {
	got := redact.FromToolInput(input)
	var command *redact.Redacted
	if (got.Command != redact.Redacted{}) {
		command = &got.Command
	}
	var signals *redact.Signals
	if !got.Signals.Empty() {
		signals = &got.Signals
	}
	return command, signals
}
