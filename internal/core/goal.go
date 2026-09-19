package core

import "strings"

// continuationSignals are the phrases that mean "carry on" and nothing else.
//
// The list is exact-match only, and deliberately short. Both ways of getting
// this wrong are bad: treat a Substantive Instruction as a Continuation Signal
// and whoa reports Drift against a Goal the user has abandoned; treat a
// Continuation Signal as Substantive and the Goal becomes the word "continue",
// which no Judge can measure anything against. Matching whole prompts only, and
// only obvious ones, keeps both rare.
//
// Languages are here because users do not all write English, and a Goal is the
// one thing whoa stores in the user's own words. Missing a language costs a
// Goal; guessing at one costs the truth.
var continuationSignals = map[string]bool{
	// English
	"continue": true, "continue please": true, "please continue": true,
	"go on": true, "go ahead": true, "keep going": true, "carry on": true,
	"proceed": true, "next": true, "more": true, "again": true, "resume": true,
	"yes": true, "y": true, "yeah": true, "yep": true, "ok": true, "okay": true,
	"sure": true, "do it": true, "go": true,
	// Chinese
	"繼續": true, "继续": true, "請繼續": true, "请继续": true, "繼續做": true,
	"好": true, "好的": true, "可以": true, "對": true, "对": true, "是": true,
	"嗯": true, "沒問題": true, "没问题": true,
	// Japanese
	"続けて": true, "続き": true, "はい": true, "お願いします": true,
	// Korean
	"계속": true, "네": true,
	// Spanish / Portuguese
	"continúa": true, "continua": true, "continuar": true, "sigue": true,
	"adelante": true, "vale": true, "sí": true, "si": true, "continue por favor": true,
	// French
	"continuez": true, "vas-y": true, "allez-y": true, "oui": true,
	// German
	"weiter": true, "mach weiter": true, "weitermachen": true, "ja": true,
}

// isContinuationSignal reports whether a prompt only tells the agent to carry
// on with what it was already doing.
func isContinuationSignal(prompt string) bool {
	return continuationSignals[normalisePrompt(prompt)]
}

// normalisePrompt reduces a prompt to the form the signal list is written in:
// lower case, no surrounding space, no trailing punctuation.
//
// It deliberately does not strip interior words. "continue with the refactor"
// normalises to itself and so is Substantive, which is the point.
func normalisePrompt(prompt string) string {
	trimmed := strings.ToLower(strings.TrimSpace(prompt))
	return strings.TrimRight(trimmed, " \t\n.!?,;:~。！？，、…")
}

// goalInForce is the most recent Substantive Instruction in the log, which is
// what the agent is currently meant to be doing.
//
// It is the most recent rather than the first because a user who says "actually,
// go and do CI instead" has retargeted the work. Measuring Drift against the
// original would report the user's own change of mind as the agent's failure.
func goalInForce(log []Entry) string {
	for i := len(log) - 1; i >= 0; i-- {
		if log[i].Kind == KindGoal {
			return log[i].Goal
		}
	}
	return ""
}

// observeGoal handles a user prompt: it moves the Goal, or leaves it alone.
//
// Nothing is written for a Continuation Signal. The Turn has moved on — the
// Harness supplies the new turn key either way — but the Goal has not, and the
// log records what changed, not what happened.
func (in Input) observeGoal(p hookPayload) Observation {
	text := strings.TrimSpace(p.Prompt)
	if text == "" || isContinuationSignal(p.Prompt) {
		return Observation{}
	}
	return Observation{Entry: &Entry{
		Kind:      KindGoal,
		Timestamp: in.Now.UTC(),
		Harness:   in.Harness,
		Session:   p.SessionID,
		Turn:      p.turnKey(),
		Goal:      text,
	}}
}
