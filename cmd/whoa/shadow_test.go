package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// judgeAt points whoa at a local Judge and gives it a key, for the duration of
// one test. Nothing in this file reaches the network.
func judgeAt(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("WHOA_JEV_ENDPOINT", srv.URL)
	t.Setenv("WHOA_JEV_API_KEY", "test-key")
	return srv
}

const cannedAnswers = `{"model":"jev-1.13.0","answers":{
	"same_intent":{"probability":0.91},"goal_drift":{"probability":0.12},
	"evading":{"probability":0.88},"needs_human":{"probability":0.3},
	"progress":{"value":2.4,"confidence":0.71}}}`

// Shadow Mode is only worth anything if nothing whoa did can have changed what
// it recorded.
func TestShadowModeRecordsAVerdictAndNeverIntervenes(t *testing.T) {
	judgeAt(t, cannedAnswers)
	s, _, _ := testSystem(t, "")
	writeConfig(t, s.home, `{"mode": "shadow", "trigger": {"repeated_failures": 2}}`)

	feed(t, s.home, failedStep)
	feed(t, s.home, failedStep)
	spoken, _ := feed(t, s.home, beforeNextStep)

	if spoken != "" {
		t.Errorf("Shadow Mode spoke to the agent:\n%s", spoken)
	}
	for _, line := range logLines(t, s, "abc123") {
		if strings.Contains(line, `"kind":"nudge"`) {
			t.Errorf("Shadow Mode recorded a Nudge:\n%s", line)
		}
	}

	if err := run([]string{"judge", "abc123"}, s); err != nil {
		t.Fatalf("judge: %v", err)
	}
	var verdict map[string]any
	lines := logLines(t, s, "abc123")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &verdict); err != nil {
		t.Fatal(err)
	}
	if verdict["kind"] != "verdict" {
		t.Fatalf("last entry is %v, want a Verdict", verdict["kind"])
	}
	if verdict["model"] != "jev-1.13.0" {
		t.Errorf("the answering model was not recorded: %v", verdict["model"])
	}
	probabilities, _ := verdict["probabilities"].(map[string]any)
	if probabilities["evading"] != 0.88 {
		t.Errorf("probabilities not recorded faithfully: %v", probabilities)
	}
	// A Score's value is not a probability, and filing it as one would
	// poison the very calibration data Shadow Mode exists to collect.
	if probabilities["progress"] != nil {
		t.Error("a Score was recorded as a probability")
	}
	scores, _ := verdict["scores"].(map[string]any)
	if scores["progress"] != 2.4 {
		t.Errorf("the Score was not recorded: %v", scores)
	}
	if verdict["ref"] == nil {
		t.Error("the Verdict does not say which evidence produced it")
	}
}

// Counter-only Mode must never assemble or send a Digest, whatever else is
// configured. It is the Mode whoa ships in and the one people choose for
// privacy.
func TestCounterOnlyModeNeverReachesTheJudge(t *testing.T) {
	asked := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = true
		io.WriteString(w, cannedAnswers)
	}))
	defer srv.Close()
	t.Setenv("WHOA_JEV_ENDPOINT", srv.URL)
	t.Setenv("WHOA_JEV_API_KEY", "test-key")

	s, _, _ := testSystem(t, "")
	writeConfig(t, s.home, `{"mode": "counters", "trigger": {"repeated_failures": 2}}`)
	feed(t, s.home, failedStep)
	feed(t, s.home, failedStep)
	feed(t, s.home, beforeNextStep)

	if asked {
		t.Error("Counter-only Mode sent a Digest")
	}
	if err := run([]string{"judge", "abc123"}, s); err == nil {
		t.Error("the judge command ran in Counter-only Mode")
	}
}

func TestMissingKeyDegradesAndSaysSoExactlyOnce(t *testing.T) {
	s, _, _ := testSystem(t, "")
	writeConfig(t, s.home, `{"mode": "full", "on_missing_key": "degrade", "trigger": {"repeated_failures": 2}}`)
	for _, name := range keyEnv {
		t.Setenv(name, "")
	}

	feed(t, s.home, failedStep)
	feed(t, s.home, failedStep)
	first, _ := feed(t, s.home, beforeNextStep)
	if !strings.Contains(first, "Counters alone") {
		t.Errorf("did not say it had degraded:\n%s", first)
	}
	if !strings.Contains(first, "additionalContext") {
		t.Errorf("degraded but did not Nudge; the whole point is that it keeps working:\n%s", first)
	}

	feed(t, s.home, failedStep)
	feed(t, s.home, failedStep)
	again, _ := feed(t, s.home, beforeNextStep)
	if strings.Contains(again, "Counters alone") {
		t.Errorf("said it again; once means once:\n%s", again)
	}

	notices := 0
	for _, line := range logLines(t, s, "abc123") {
		if strings.Contains(line, `"kind":"notice"`) {
			notices++
		}
	}
	if notices != 1 {
		t.Errorf("recorded %d notices, want exactly 1", notices)
	}
}

func TestMissingKeyCanFailLoudlyInstead(t *testing.T) {
	s, _, _ := testSystem(t, "")
	writeConfig(t, s.home, `{"mode": "full", "on_missing_key": "error", "trigger": {"repeated_failures": 2}}`)
	for _, name := range keyEnv {
		t.Setenv(name, "")
	}

	feed(t, s.home, failedStep)
	feed(t, s.home, failedStep)
	spoken, complained := feed(t, s.home, beforeNextStep)

	if !strings.Contains(complained, "was not judged") {
		t.Errorf("did not say the Step went unjudged:\n%s", complained)
	}
	if strings.Contains(spoken, "additionalContext") {
		t.Errorf("error mode Nudged anyway; the user asked to be told they are unprotected:\n%s", spoken)
	}
}

// A Judge that is not answering must leave a trace the user can find, and must
// not break anything.
func TestAnUnreachableJudgeIsRecordedNotRaised(t *testing.T) {
	t.Setenv("WHOA_JEV_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("WHOA_JEV_API_KEY", "test-key")
	s, _, _ := testSystem(t, "")
	writeConfig(t, s.home, `{"mode": "shadow", "trigger": {"repeated_failures": 2}}`)
	feed(t, s.home, failedStep)

	if err := run([]string{"judge", "abc123"}, s); err != nil {
		t.Errorf("an unreachable Judge broke the command: %v", err)
	}
	found := false
	for _, line := range logLines(t, s, "abc123") {
		if strings.Contains(line, `"kind":"notice"`) && strings.Contains(line, "could not be reached") {
			found = true
		}
	}
	if !found {
		t.Error("nothing in the log says the Judge was unreachable")
	}
}

// The Judge's latency must never land on a Step. The call runs in a separate
// process precisely so that it cannot.
func TestTheJudgeCallDoesNotDelayAStep(t *testing.T) {
	if _, err := os.Stat("judge.go"); err != nil {
		t.Skip("source not available")
	}
	source, err := os.ReadFile("judge.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), "cmd.Wait()") || strings.Contains(string(source), "cmd.Run()") {
		t.Error("the hook waits for the Judge; the Step would carry its latency")
	}
	if !strings.Contains(string(source), "cmd.Start()") {
		t.Error("the Judge call is not started as a separate process")
	}
}
