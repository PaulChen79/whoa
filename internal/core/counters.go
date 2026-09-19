package core

// Counters are the deterministic facts code computes from the sequence of
// Steps. They carry no probability and no judgement: a Counter is either true
// of the log or it is not, and the same log always yields the same Counters.
//
// That reproducibility is the point. A Verdict a user disagrees with is only
// worth collecting if whoa can show exactly what it saw, and a time decay or a
// wall-clock term would make a replayed Verdict differ from the original.
type Counters struct {
	// Steps in the Window.
	Steps int `json:"steps"`
	// Tool is the most recent Step's tool: the one the runs below are about.
	Tool string `json:"tool,omitempty"`
	// RepeatedFailures is the trailing run of failed Steps using Tool. This is
	// the Loop counter: the agent retrying the same already-failed thing.
	RepeatedFailures int `json:"repeated_failures"`
	// RepeatedTool is the trailing run of Steps using Tool, however they ended.
	RepeatedTool int `json:"repeated_tool"`
	// FailedSteps is every failed Step in the Window, run or not.
	FailedSteps int `json:"failed_steps"`
}

// Count computes the Counters over the last window Steps of the log.
//
// The Window is a fixed Step count and never resets at a Turn boundary. A user
// saying "continue" must not erase the evidence — impatience is itself part of
// the pattern whoa is looking for.
func Count(entries []Entry, window int) Counters {
	steps := recentSteps(entries, window)
	if len(steps) == 0 {
		return Counters{}
	}

	last := steps[len(steps)-1]
	c := Counters{Steps: len(steps), Tool: last.Tool}

	for _, s := range steps {
		if s.Outcome == OutcomeError {
			c.FailedSteps++
		}
	}

	for i := len(steps) - 1; i >= 0 && steps[i].Tool == last.Tool; i-- {
		c.RepeatedTool++
	}
	for i := len(steps) - 1; i >= 0 && steps[i].Tool == last.Tool && steps[i].Outcome == OutcomeError; i-- {
		c.RepeatedFailures++
	}
	return c
}

// recentSteps is the Window: the last window Steps, ignoring everything whoa
// wrote about itself.
func recentSteps(entries []Entry, window int) []Entry {
	steps := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.IsStep() {
			steps = append(steps, e)
		}
	}
	if window > 0 && len(steps) > window {
		steps = steps[len(steps)-window:]
	}
	return steps
}

// Before returns the log as it stood immediately before the given line,
// counting from 1.
//
// Disputing a Verdict is only meaningful if the evidence behind it can be
// reconstructed exactly. A Misjudgment records which line it disputes, and
// this is what turns that reference back into the Window the Verdict saw.
func Before(log []Entry, line int) []Entry {
	if line <= 0 {
		return nil
	}
	if line > len(log) {
		return log
	}
	return log[:line-1]
}
