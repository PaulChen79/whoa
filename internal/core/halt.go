package core

import "fmt"

// haltOutput denies one Step, with the reason the agent will be shown.
//
// Denying a single Step is deliberately weaker than ending the Turn. The
// agent stops, is told why, and decides again, so it keeps the ability to
// rescue itself; and the person is not dragged in to restart anything. Both
// Harnesses support deny identically, which is the other reason it was
// chosen: the core mechanism should not rest on a field only one of them is
// confirmed to honour.
func haltOutput(reason, human string) []byte {
	return marshalOutput(hookOutput{
		HookSpecificOutput: preToolUseOutput{
			HookEventName:      "PreToolUse",
			PermissionDecision: "deny",
			PermissionReason:   reason,
		},
		// A Halt is always shown to the person, whatever notify says. They
		// are entitled to know their agent was stopped; notify governs
		// commentary, not this.
		SystemMessage: human,
	})
}

// halt decides whether this Step is denied, and why.
//
// Two things earn a Halt: a Verdict whoa is very sure about, and Nudges that
// have plainly not worked. The second matters more in practice. A Verdict at
// 0.95 is rare; an agent that has been told three times and carried on
// regardless is the case a Nudge cannot fix.
func (in Input) halt(line int, v Entry) (Observation, bool) {
	if !in.Config.HaltEnabled {
		return Observation{}, false
	}
	if n := stepsSinceHalt(in.Log); n >= 0 && n < haltGap(in.Log, in.Config.IneffectiveNudgesBeforeHalt) {
		return Observation{}, false
	}

	var reason, human string
	switch ignored := ignoredNudges(in.Log); {
	case ignored >= in.Config.IneffectiveNudgesBeforeHalt:
		reason = fmt.Sprintf(
			"whoa stopped this step. You have been told %d times that %s, and the approach has not changed. "+
				"Do something different, or say what you are stuck on.",
			ignored, verdictSays[lastNudgeCounter(in.Log)].person)
		human = fmt.Sprintf("whoa: halted the agent after %d ignored nudges (%s).",
			ignored, lastNudgeCounter(in.Log))
	case line > 0 && Strongest(v) >= in.Config.HaltThreshold:
		key := strongestKey(v)
		reason = fmt.Sprintf("whoa stopped this step: %s. Reconsider before trying again.",
			verdictSays[key].person)
		human = fmt.Sprintf("whoa: halted the agent (%s, %.2f).", key, Strongest(v))
	default:
		return Observation{}, false
	}

	return Observation{
		Entry: &Entry{
			Kind: KindHalt, Timestamp: in.Now.UTC(), Harness: in.Harness,
			Session: v.Session, Counter: strongestKey(v), Ref: line, Fact: human,
		},
		Output: haltOutput(reason, human),
		Human:  human,
	}, true
}

// ignoredNudges is the trailing run of Nudges about the same thing.
//
// A Nudge followed by another Nudge about the same Counter is a Nudge that
// did not work. Steps in between are expected: the agent kept going, which
// is the point.
func ignoredNudges(log []Entry) int {
	counter, n := "", 0
	for i := len(log) - 1; i >= 0; i-- {
		switch {
		case log[i].Kind == KindHalt:
			return n
		case log[i].Kind != KindNudge:
			continue
		case counter == "":
			counter, n = log[i].Counter, 1
		case log[i].Counter == counter:
			n++
		default:
			return n
		}
	}
	return n
}

// lastNudgeCounter names the thing the agent has been ignoring.
func lastNudgeCounter(log []Entry) string {
	for i := len(log) - 1; i >= 0; i-- {
		if log[i].Kind == KindNudge {
			return log[i].Counter
		}
	}
	return ""
}

// stepsSinceHalt counts Steps recorded since the last Halt, or -1 if whoa has
// never halted this Session.
func stepsSinceHalt(log []Entry) int {
	for i := len(log) - 1; i >= 0; i-- {
		if log[i].Kind == KindHalt {
			n := 0
			for _, e := range log[i+1:] {
				if e.Kind == KindStep {
					n++
				}
			}
			return n
		}
	}
	return -1
}

// haltGap is how many Steps must pass before whoa may halt again, doubling
// each time.
//
// Backoff is counted in code rather than left to judgement because a
// stop-loss that fires repeatedly is the exact failure it exists to prevent.
// If whoa has halted twice already and the agent is still going, whoa is not
// the thing that is going to fix this, and it should get quieter rather than
// louder.
func haltGap(log []Entry, base int) int {
	if base < 1 {
		base = 1
	}
	gap := base
	for _, e := range log {
		if e.Kind == KindHalt {
			gap *= 2
		}
	}
	return gap
}

// strongestKey names the Noul with the highest probability.
func strongestKey(e Entry) string {
	best := Strongest(e)
	for _, n := range nouls {
		if e.Probabilities[n] == best && best > 0 {
			return n
		}
	}
	return ""
}
