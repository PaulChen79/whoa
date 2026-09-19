package core

import (
	"strings"
	"testing"
	"time"
)

// verdict is a Judge answer sitting in the log, waiting to be acted on.
func verdict(probs map[string]float64) Entry {
	return Entry{Kind: KindVerdict, Session: "s", Model: "jev-1.13.0", Probabilities: probs}
}

func fullConfig() Config {
	c := Defaults()
	c.Mode = ModeFull
	c.Trigger = Trigger{RepeatedFailures: 3}
	return c
}

func observeFull(t *testing.T, cfg Config, log []Entry) Observation {
	t.Helper()
	return Observe(Input{
		Raw: preToolUse("s"), Harness: ClaudeCode, Log: log,
		Config: cfg, Now: time.Unix(0, 0), JudgeAvailable: true,
	})
}

func TestAVerdictAboveTheThresholdNudges(t *testing.T) {
	log := append(failures(3), verdict(map[string]float64{"evading": 0.81}))
	ob := observeFull(t, fullConfig(), log)

	if ob.Entry == nil || ob.Entry.Kind != KindNudge {
		t.Fatal("a Verdict of 0.81 against a threshold of 0.60 produced no Nudge")
	}
	if ob.Entry.Counter != "evading" {
		t.Errorf("the Nudge does not name the question that fired: %q", ob.Entry.Counter)
	}
}

// The agent must never see the number. It will argue with a number.
func TestTheAgentIsToldTheFactAndNeverTheProbability(t *testing.T) {
	log := append(failures(3), verdict(map[string]float64{"evading": 0.81}))
	ob := observeFull(t, fullConfig(), log)

	agent := agentContext(t, ob.Output)
	if agent == "" {
		t.Fatal("the agent was told nothing")
	}
	for _, n := range []string{"0.81", "81", "probability", "0.6"} {
		if strings.Contains(agent, n) {
			t.Errorf("the agent can see the number %q: %s", n, agent)
		}
	}
	if !strings.Contains(strings.ToLower(agent), "test") {
		t.Errorf("the Nudge does not say what it is about: %s", agent)
	}
}

// The human does need the number: it is how they judge `whoa wrong`.
func TestTheHumanIsToldTheProbability(t *testing.T) {
	log := append(failures(3), verdict(map[string]float64{"evading": 0.81}))
	ob := observeFull(t, fullConfig(), log)

	if !strings.Contains(ob.Human, "0.81") {
		t.Errorf("the human cannot see the probability, so cannot judge it: %q", ob.Human)
	}

	quiet := fullConfig()
	quiet.Notify = false
	if got := observeFull(t, quiet, log); got.Human != "" {
		t.Errorf("notify=false did not silence the human line: %q", got.Human)
	} else if len(got.Output) == 0 {
		t.Error("notify=false also silenced the agent, which is not what it means")
	}
}

func TestTheThresholdIsAParameter(t *testing.T) {
	log := append(failures(3), verdict(map[string]float64{"evading": 0.55}))

	if ob := observeFull(t, fullConfig(), log); ob.Entry != nil {
		t.Error("0.55 crossed a threshold of 0.60")
	}

	lower := fullConfig()
	lower.NudgeThreshold = 0.50
	if ob := observeFull(t, lower, log); ob.Entry == nil {
		t.Error("lowering the threshold to 0.50 did not change behaviour")
	}
}

func TestEachQuestionCanFireAndNamesItself(t *testing.T) {
	for _, q := range []struct{ key, expect string }{
		{"same_intent", "same"},
		{"goal_drift", "goal"},
		{"evading", "test"},
		{"needs_human", "ask"},
	} {
		t.Run(q.key, func(t *testing.T) {
			// Above nudge_threshold and below halt_threshold, so this is a
			// Nudge and not a Halt.
			log := append(failures(3), verdict(map[string]float64{q.key: 0.75}))
			ob := observeFull(t, fullConfig(), log)
			if ob.Entry == nil {
				t.Fatalf("%s could not fire a Nudge at 0.9", q.key)
			}
			if ob.Entry.Counter != q.key {
				t.Errorf("Nudge names %q, want %q", ob.Entry.Counter, q.key)
			}
			if !strings.Contains(strings.ToLower(agentContext(t, ob.Output)), q.expect) {
				t.Errorf("%s does not say what it means: %s", q.key, agentContext(t, ob.Output))
			}
		})
	}
}

// progress is an impression, not a named failure. Wiring it into control flow
// would make "why did whoa interrupt me?" unanswerable, and explicability is
// the precondition for anyone leaving this installed.
func TestProgressIsRecordedAndNeverActs(t *testing.T) {
	e := verdict(nil)
	e.Scores = map[string]float64{"progress": 1.0}
	log := append(failures(3), e)

	if ob := observeFull(t, fullConfig(), log); ob.Entry != nil {
		t.Errorf("a progress score alone caused an intervention: %+v", ob.Entry)
	}
}

func TestTheSameVerdictIsNotNudgedTwice(t *testing.T) {
	log := append(failures(3), verdict(map[string]float64{"evading": 0.81}))

	first := observeFull(t, fullConfig(), log)
	if first.Entry == nil {
		t.Fatal("no first Nudge")
	}
	log = append(log, *first.Entry)

	if again := observeFull(t, fullConfig(), log); again.Entry != nil {
		t.Errorf("Nudged twice about one unchanged Verdict: %+v", again.Entry)
	}

	// A fresh Verdict is a new situation and earns a new Nudge.
	log = append(log, failures(1)[0], verdict(map[string]float64{"evading": 0.93}))
	if fresh := observeFull(t, fullConfig(), log); fresh.Entry == nil {
		t.Error("a new Verdict was ignored because an older one had been acted on")
	}
}

// The highest-probability question speaks, so one Step produces one Nudge.
func TestTheStrongestQuestionSpeaks(t *testing.T) {
	log := append(failures(3), verdict(map[string]float64{
		"same_intent": 0.72, "evading": 0.95, "goal_drift": 0.66,
	}))
	ob := observeFull(t, fullConfig(), log)
	if ob.Entry == nil || ob.Entry.Counter != "evading" {
		t.Errorf("the strongest signal did not speak: %+v", ob.Entry)
	}
}
