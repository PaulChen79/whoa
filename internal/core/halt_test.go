package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// denial digs out the permission decision whoa sent, so the tests assert on
// what the Harness actually acts on rather than on whoa's internals.
func denial(t *testing.T, out []byte) (decision, reason string) {
	t.Helper()
	if len(out) == 0 {
		return "", ""
	}
	var parsed struct {
		HookSpecificOutput struct {
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %s", out)
	}
	return parsed.HookSpecificOutput.PermissionDecision, parsed.HookSpecificOutput.PermissionDecisionReason
}

func haltConfig() Config {
	c := fullConfig()
	c.HaltEnabled = true
	return c
}

// A Nudge that was ignored three times running is not working.
func nudged(counter string, n int) []Entry {
	var out []Entry
	for i := 0; i < n; i++ {
		out = append(out, Entry{Kind: KindStep, Session: "s", Tool: "Bash", Outcome: OutcomeError})
		out = append(out, Entry{Kind: KindNudge, Session: "s", Counter: counter, Ref: 1})
	}
	return out
}

func TestAVerdictAboveTheHaltThresholdDeniesTheStep(t *testing.T) {
	log := append(failures(3), verdict(map[string]float64{"evading": 0.95}))
	ob := observeFull(t, haltConfig(), log)

	decision, reason := denial(t, ob.Output)
	if decision != "deny" {
		t.Fatalf("permissionDecision = %q, want deny", decision)
	}
	if !strings.Contains(strings.ToLower(reason), "test") {
		t.Errorf("the denial does not say what caused it: %q", reason)
	}
	if ob.Entry == nil || ob.Entry.Kind != KindHalt {
		t.Error("the Halt was not recorded")
	}
}

// Halt denies one Step. It does not end the Turn: the agent must be able to
// rescue itself, which is what keeps this consistent with Nudge-first.
func TestHaltDeniesExactlyOneStep(t *testing.T) {
	log := append(failures(3), verdict(map[string]float64{"evading": 0.95}))
	first := observeFull(t, haltConfig(), log)
	if d, _ := denial(t, first.Output); d != "deny" {
		t.Fatal("no first Halt")
	}
	log = append(log, *first.Entry)

	again := observeFull(t, haltConfig(), log)
	if d, _ := denial(t, again.Output); d == "deny" {
		t.Error("denied a second Step in a row: the agent can never rescue itself")
	}
}

func TestIgnoredNudgesEscalateToAHalt(t *testing.T) {
	cfg := haltConfig()
	cfg.IneffectiveNudgesBeforeHalt = 3

	if ob := observeFull(t, cfg, nudged("evading", 2)); ob.Entry != nil && ob.Entry.Kind == KindHalt {
		t.Error("halted after two ignored Nudges, one short of the parameter")
	}
	ob := observeFull(t, cfg, nudged("evading", 3))
	if d, _ := denial(t, ob.Output); d != "deny" {
		t.Fatalf("three ignored Nudges did not escalate: %s", ob.Output)
	}
	if !strings.Contains(ob.Human, "3") {
		t.Errorf("the person is not told how many Nudges were ignored: %q", ob.Human)
	}
}

func TestHaltCanBeTurnedOffWithoutLosingNudge(t *testing.T) {
	cfg := haltConfig()
	cfg.HaltEnabled = false
	log := append(failures(3), verdict(map[string]float64{"evading": 0.95}))

	ob := observeFull(t, cfg, log)
	if d, _ := denial(t, ob.Output); d == "deny" {
		t.Error("halt_enabled=false still denied a Step")
	}
	if ob.Entry == nil || ob.Entry.Kind != KindNudge {
		t.Fatalf("turning Halt off also lost the Nudge: %+v", ob.Entry)
	}
}

// A stop-loss that loops is the thing it exists to prevent.
func TestHaltBacksOffAndTheGapGrows(t *testing.T) {
	cfg := haltConfig()
	cfg.IneffectiveNudgesBeforeHalt = 3
	log := nudged("evading", 3)

	halts := 0
	for step := 0; step < 40; step++ {
		ob := observeFull(t, cfg, log)
		if d, _ := denial(t, ob.Output); d == "deny" {
			halts++
			log = append(log, *ob.Entry)
		}
		log = append(log, Entry{Kind: KindStep, Session: "s", Tool: "Bash", Outcome: OutcomeError})
	}
	if halts == 0 {
		t.Fatal("never halted at all")
	}
	if halts > 4 {
		t.Errorf("halted %d times in 40 Steps: the stop-loss is itself a loop", halts)
	}
}

// The person must always know their agent was stopped, even with notify off.
func TestAHaltIsAlwaysVisibleToThePerson(t *testing.T) {
	cfg := haltConfig()
	cfg.Notify = false
	log := append(failures(3), verdict(map[string]float64{"evading": 0.95}))

	ob := observeFull(t, cfg, log)
	if ob.Human == "" {
		t.Error("notify=false hid the fact that the agent was stopped")
	}
}

func TestHaltIsIdenticalOnCodex(t *testing.T) {
	log := append(failures(3), verdict(map[string]float64{"evading": 0.95}))
	claude := observeFull(t, haltConfig(), log)
	codex := Observe(Input{
		Raw:     []byte(`{"session_id":"s","hook_event_name":"PreToolUse","tool_name":"shell","turn_id":"t1"}`),
		Harness: Codex, Log: log, Config: haltConfig(),
		Now: time.Unix(0, 0), JudgeAvailable: true,
	})

	cd, cr := denial(t, claude.Output)
	xd, xr := denial(t, codex.Output)
	if cd != xd || cr != xr {
		t.Errorf("the two Harnesses are denied differently:\n  claude: %s / %s\n  codex:  %s / %s", cd, cr, xd, xr)
	}
}

// Having denied a Step over a Verdict, whoa must not then Nudge about the
// same Verdict: that is saying the same thing twice.
func TestAHaltSettlesTheVerdictItActedOn(t *testing.T) {
	log := append(failures(3), verdict(map[string]float64{"evading": 0.95}))
	halt := observeFull(t, haltConfig(), log)
	if halt.Entry == nil || halt.Entry.Kind != KindHalt {
		t.Fatal("no Halt")
	}
	log = append(log, *halt.Entry, Entry{Kind: KindStep, Session: "s", Tool: "Bash", Outcome: OutcomeError})

	if next := observeFull(t, haltConfig(), log); next.Entry != nil {
		t.Errorf("repeated itself about a Verdict it had already halted on: %+v", next.Entry)
	}
}
