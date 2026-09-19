package core

import (
	"encoding/json"
	"fmt"
	"strings"
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
	counters := Count(in.Log, in.Config.Window)
	f, ok := in.Config.Trigger.fires(counters)
	if !ok || !worthSaying(in.Log, f) {
		return Observation{}
	}

	pl := in.plan()
	if pl.problem != "" {
		return Observation{Problem: pl.problem}
	}
	// Outside Counter-only Mode the Trigger asks the Judge rather than
	// speaking for itself. In Shadow Mode that is the whole of what happens:
	// the Judge is asked, the answer is written down, and the agent is left
	// entirely alone. That is what makes the recorded Verdicts worth anything
	// as calibration data — nothing whoa did can have changed them.
	if pl.ask {
		// A Verdict already in hand is acted on before another is asked
		// for: the Judge answers asynchronously, so the answer to the last
		// Trigger arrives during a later Step, and this is where it lands.
		if ob := in.fromVerdict(); ob.Entry != nil {
			return ob
		}
		return Observation{AskJudge: true}
	}
	if !pl.speak {
		return Observation{Notice: pl.notice}
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
	if pl.notice != nil {
		human = strings.TrimSpace(human + " " + noKeyLine)
	}
	return Observation{Entry: entry, Output: nudgeOutput(f, human), Human: human, Notice: pl.notice}
}

// noKeyLine is what the person is told, once, when whoa wanted the Judge and
// had no key.
const noKeyLine = "whoa has no Judge API key, so it is running on Counters alone. " +
	"Set WHOA_JEV_API_KEY, or set mode to \"counters\" to stop seeing this."

// NoKeyNotice is the Fact recorded when whoa degrades for want of a key.
const NoKeyNotice = "no Judge API key configured"

// plan is what whoa will do about a Trigger that has fired: speak from
// Counters alone, ask the Judge, or neither.
type plan struct {
	speak   bool
	ask     bool
	notice  *Entry
	problem string
}

// plan resolves those three, given whether a Judge key exists.
//
// Both answers to a missing key are legitimate and the difference matters to
// real people. On a plane, with an expired key, or out of quota, some want a
// tool that keeps working and some want to be told plainly that they are not
// protected. Degrading is the default because a stop-loss that stops at the
// first inconvenience protects nobody.
//
// Degrading never makes whoa louder than the Mode asked for. Shadow Mode was
// chosen to watch in silence, so without a key it watches nothing and stays
// silent; it does not fall back to Nudging. Only Full Mode, which was already
// willing to speak, falls back to speaking from Counters.
//
// The notice is produced only when something would otherwise have happened, so
// whoa does not nag about a key during a Session where nothing went wrong.
func (in Input) plan() plan {
	switch {
	case in.Config.Mode == ModeCounters:
		return plan{speak: true}
	case in.JudgeAvailable:
		return plan{ask: true}
	case in.Config.OnMissingKey == OnMissingKeyError:
		return plan{problem: NoKeyNotice + `, and on_missing_key is "error": this Step was not judged`}
	}
	p := plan{speak: in.Config.Mode == ModeFull}
	for _, e := range in.Log {
		if e.Kind == KindNotice && e.Fact == NoKeyNotice {
			return p
		}
	}
	p.notice = &Entry{Kind: KindNotice, Timestamp: in.Now.UTC(), Harness: in.Harness, Fact: NoKeyNotice}
	return p
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
	return contextOutput(fmt.Sprintf(
		"whoa: %s. This looks like the same approach being retried rather than a new one. "+
			"Stop and consider whether the obstacle is what you think it is, "+
			"or say what you are stuck on instead of trying again.",
		f.Fact,
	), human)
}

// contextOutput is the one place a Nudge becomes hook output.
//
// The agent's text and the person's are separate fields because they are
// deliberately different: from a Judge, the person's copy carries the
// probability and the agent's never does.
func contextOutput(context, human string) []byte {
	out, err := json.Marshal(hookOutput{
		HookSpecificOutput: preToolUseOutput{
			HookEventName:     "PreToolUse",
			AdditionalContext: context,
		},
		// systemMessage is the documented way to reach the person watching.
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
