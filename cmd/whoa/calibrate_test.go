package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCalibrateSaysTheGateIsUnmetWithNoEvidence(t *testing.T) {
	s, out, _ := testSystem(t, "")
	if err := run([]string{"calibrate"}, s); err == nil {
		t.Error("an unmet gate exited zero, so a release script would pass")
	}
	if !strings.Contains(out.String(), "50") {
		t.Errorf("did not say how much evidence is needed: %s", out)
	}
}

func TestCalibrateCountsMisjudgmentsAgainstNudgeLevelVerdicts(t *testing.T) {
	s, out, _ := testSystem(t, "")
	writeConfig(t, s.home, `{"mode":"full","state_dir":"`+s.home+`/state"}`)

	// Three Nudge-level Verdicts, one below the threshold, one disputed.
	lines := []string{
		`{"kind":"step","session":"c1","tool":"Bash","outcome":"error"}`,
		`{"kind":"verdict","session":"c1","probabilities":{"evading":0.81}}`,
		`{"kind":"verdict","session":"c1","probabilities":{"evading":0.12}}`,
		`{"kind":"verdict","session":"c1","probabilities":{"same_intent":0.77}}`,
		`{"kind":"verdict","session":"c1","probabilities":{"goal_drift":0.90}}`,
		`{"kind":"wrong","session":"c1","ref":2}`,
	}
	dir := filepath.Join(s.home, "state", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c1.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"calibrate"}, s); err == nil {
		t.Error("three samples met a gate that needs fifty")
	}
	got := out.String()
	for _, want := range []string{"Nudge-level Verdicts: 3", "Misjudgments: 1", "33%"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// The 0.12 Verdict is below the threshold and is not a sample.
	if strings.Contains(got, "Verdicts: 4") {
		t.Error("counted a Verdict that would never have Nudged")
	}
}
