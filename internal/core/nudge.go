package core

import (
	"encoding/json"
	"fmt"
)

// fired is the one Counter that crossed its threshold, and the sentence a
// person would use to describe it.
type fired struct {
	Counter   string
	Count     int
	Threshold int
	Fact      string
}

// fires reports the Counter that has crossed its threshold, if any.
//
// The order is most specific first: retrying one failing thing says more than
// a general pile of failures, and whichever fires is the one the agent is told
// about. Only one Counter is ever reported, because a Nudge listing three
// facts is a Nudge the agent skims.
func (t Trigger) fires(c Counters) (fired, bool) {
	switch {
	case reached(c.RepeatedFailures, t.RepeatedFailures):
		return fired{
			Counter: "repeated_failures", Count: c.RepeatedFailures, Threshold: t.RepeatedFailures,
			Fact: fmt.Sprintf("%s has failed %d times in a row", c.Tool, c.RepeatedFailures),
		}, true
	case reached(c.RepeatedTool, t.RepeatedTool):
		return fired{
			Counter: "repeated_tool", Count: c.RepeatedTool, Threshold: t.RepeatedTool,
			Fact: fmt.Sprintf("the last %d steps have all used %s", c.RepeatedTool, c.Tool),
		}, true
	case reached(c.FailedSteps, t.FailedSteps):
		return fired{
			Counter: "failed_steps", Count: c.FailedSteps, Threshold: t.FailedSteps,
			Fact: fmt.Sprintf("%d of the last %d steps have failed", c.FailedSteps, c.Steps),
		}, true
	}
	return fired{}, false
}

// reached is the threshold test. A threshold of zero switches that Counter off
// rather than firing on everything.
func reached(count, threshold int) bool { return threshold > 0 && count >= threshold }

// intervene is the PreToolUse path: the last moment before the agent's next
// Step where whoa can still say something.
//
// The Counters are computed here rather than on the event that finished the
// previous Step, so that the Nudge lands in the agent's context before it
// acts again instead of after the Step that earned it.
func (in Input) intervene(p hookPayload) Observation {
	if in.Config.Mode != ModeCounters {
		// Shadow Mode never intervenes, and full mode's Verdict comes from the
		// Judge rather than from Counters alone.
		return Observation{}
	}

	counters := Count(in.Log, in.Config.Window)
	f, ok := in.Config.Trigger.fires(counters)
	if !ok || !worthSaying(in.Log, f) {
		return Observation{}
	}

	entry := &Entry{
		Kind:      KindNudge,
		Timestamp: in.Now.UTC(),
		Harness:   in.Harness,
		Session:   p.SessionID,
		Turn:      p.turnKey(),
		Counter:   f.Counter,
		Count:     f.Count,
		Threshold: f.Threshold,
		Fact:      f.Fact,
	}

	human := ""
	if in.Config.Notify {
		human = fmt.Sprintf("whoa: %s — nudged the agent to reconsider.", f.Fact)
	}
	return Observation{Entry: entry, Output: nudgeOutput(f, human), Human: human}
}

// worthSaying decides whether this Nudge tells the agent anything it has not
// already been told.
//
// Repeating a Nudge on every Step is how a stop-loss becomes noise, so a
// Counter that has already been Nudged about stays quiet until the situation
// changes: either it got another full trigger's worth worse, or it dropped,
// which means the agent broke out and has fallen back in.
func worthSaying(entries []Entry, f fired) bool {
	at, last, ok := lastNudge(entries, f.Counter)
	if !ok {
		return true
	}
	// A run that began after the last Nudge is a new situation even when it
	// has reached exactly the same length: the agent broke out of the Loop and
	// fell back into it, which is worth saying again.
	if f.Count <= stepsAfter(entries, at) {
		return true
	}
	// The same run continuing earns another Nudge only once it has got a full
	// trigger's worth worse.
	return f.Count >= last.Count+f.Threshold
}

func lastNudge(entries []Entry, counter string) (int, Entry, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == KindNudge && entries[i].Counter == counter {
			return i, entries[i], true
		}
	}
	return 0, Entry{}, false
}

// stepsAfter counts the Steps recorded after a given position in the log.
func stepsAfter(entries []Entry, at int) int {
	n := 0
	for _, e := range entries[at+1:] {
		if e.IsStep() {
			n++
		}
	}
	return n
}

// nudgeOutput builds what the Harness reads on stdout.
//
// The wording is a code-side template. The Judge cannot generate text, so this
// is the only way a Nudge can be phrased at all, and it means the agent sees
// the same sentence for the same fact every time.
//
// It states the fact and never a probability: a number invites the agent to
// argue with the number rather than reconsider what it is doing.
func nudgeOutput(f fired, human string) []byte {
	context := fmt.Sprintf(
		"whoa: %s. This looks like the same approach being retried rather than a new one. "+
			"Stop and consider whether the obstacle is what you think it is, "+
			"or say what you are stuck on instead of trying again.",
		f.Fact,
	)
	out, err := json.Marshal(hookOutput{
		HookSpecificOutput: preToolUseOutput{
			HookEventName:     "PreToolUse",
			AdditionalContext: context,
		},
		// systemMessage is the documented way to reach the person watching.
		// They get the same fact, but it is a separate field so that what the
		// agent reads and what the human reads can differ: from a Judge, the
		// human's copy carries the probability and the agent's never does.
		SystemMessage: human,
	})
	if err != nil {
		// The fields are strings whoa built itself; this cannot fail. If it
		// somehow does, saying nothing is the documented no-op.
		return nil
	}
	return out
}

type hookOutput struct {
	HookSpecificOutput preToolUseOutput `json:"hookSpecificOutput"`
	SystemMessage      string           `json:"systemMessage,omitempty"`
}

type preToolUseOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}
