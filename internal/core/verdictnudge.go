package core

import "fmt"

// said is what a Nudge from a Verdict tells the agent and the person.
//
// The agent is never shown the probability. It would argue with the number
// instead of reconsidering the work, and the number is not evidence it can
// act on. The person is shown it, because it is how they decide whether the
// Verdict was a Misjudgment worth recording with `whoa wrong`.
type said struct {
	agent  string
	person string
}

// verdictSays maps each Noul to what whoa says when it fires.
//
// The text is a fixed template because the Judge cannot generate text: it
// answers questions and returns numbers. Every line names the thing whoa
// thinks is happening, so the agent can disagree with a claim rather than
// with a scolding.
var verdictSays = map[string]said{
	"same_intent": {
		agent:  "whoa: these look like the same attempt repeated rather than a new one. Say what you expect to be different this time, or try a different approach.",
		person: "the same attempt is being repeated",
	},
	"goal_drift": {
		agent:  "whoa: this no longer looks like work toward the goal you were given. Check what was actually asked before continuing.",
		person: "the work has drifted from the goal",
	},
	"evading": {
		agent:  "whoa: this looks like changing the test rather than fixing the code. If the test is genuinely wrong, say why before changing it.",
		person: "the test is being changed rather than the code",
	},
	"needs_human": {
		agent:  "whoa: this looks like it needs something only the person can tell you. Consider stopping to ask rather than guessing.",
		person: "the agent appears to need information only you have",
	},
}

// nouls is the order questions are considered in, so that a tie between two
// equal probabilities always resolves the same way.
var nouls = []string{"same_intent", "goal_drift", "evading", "needs_human"}

// fromVerdict turns the most recent unacted Verdict into a Nudge.
//
// It reads Probabilities and never Scores. That is not a filter that could be
// forgotten: progress is a Score and lives in a different field, so there is
// no path by which an uncalibrated impression reaches control flow.
func (in Input) fromVerdict() Observation {
	line, v, _ := latestVerdict(in.Log)

	// Halt is considered first, and can fire on ignored Nudges alone even
	// when there is no fresh Verdict to act on.
	if ob, halted := in.halt(line, v); halted {
		return ob
	}
	if line == 0 || line <= lastVerdictActedOn(in.Log) {
		return Observation{}
	}

	key, p := strongestKey(v), Strongest(v)
	if key == "" || p < in.Config.NudgeThreshold {
		return Observation{}
	}

	text := verdictSays[key]
	human := ""
	if in.Config.Notify {
		human = fmt.Sprintf("whoa: %s (%s, %.2f) — nudged the agent to reconsider.", text.person, key, p)
	}
	entry := &Entry{
		Kind:      KindNudge,
		Timestamp: in.Now.UTC(),
		Harness:   in.Harness,
		Session:   v.Session,
		Counter:   key,
		Ref:       line,
		Fact:      text.person,
	}
	return Observation{Entry: entry, Output: contextOutput(text.agent, human), Human: human}
}

// Strongest is the highest Noul probability in a Verdict, which is the one
// that decides whether it is Nudge-level. Scores are not considered.
func Strongest(e Entry) float64 {
	best := 0.0
	for _, n := range nouls {
		if e.Probabilities[n] > best {
			best = e.Probabilities[n]
		}
	}
	return best
}

// latestVerdict returns the last Verdict in the log and its 1-based line.
func latestVerdict(log []Entry) (int, Entry, bool) {
	for i := len(log) - 1; i >= 0; i-- {
		if log[i].Kind == KindVerdict {
			return i + 1, log[i], true
		}
	}
	return 0, Entry{}, false
}

// lastVerdictActedOn returns the line of the newest Verdict whoa has already
// responded to, by Nudge or by Halt.
//
// A Halt counts. Having just denied a Step over a Verdict, following it with
// a Nudge about the same Verdict says the same thing twice and makes whoa
// look like it is not keeping track.
func lastVerdictActedOn(log []Entry) int {
	for i := len(log) - 1; i >= 0; i-- {
		if (log[i].Kind == KindNudge || log[i].Kind == KindHalt) && log[i].Ref > 0 {
			return log[i].Ref
		}
	}
	return 0
}
