package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The point of this command is that a person can read the whole payload before
// anything leaves their machine, so it has to work with no key configured and
// make no request.
func TestDigestPrintsTheWholeRequestAndSendsNothing(t *testing.T) {
	s, out, errOut := testSystem(t, "")
	feed(t, s.home, failedStep)
	feed(t, s.home, failedStep)

	if err := run([]string{"digest"}, s); err != nil {
		t.Fatalf("digest: %v", err)
	}

	var request struct {
		State struct {
			Steps []struct {
				Tool    string `json:"tool"`
				Command string `json:"command"`
			} `json:"steps"`
			Counters map[string]any `json:"counters"`
		} `json:"state"`
		Questions []struct {
			Key string `json:"key"`
		} `json:"questions"`
	}
	if err := json.Unmarshal([]byte(out.String()), &request); err != nil {
		t.Fatalf("digest did not print a request: %v\n%s", err, out.String())
	}
	if len(request.State.Steps) != 2 {
		t.Errorf("printed %d Steps, want 2", len(request.State.Steps))
	}
	if len(request.Questions) != 5 {
		t.Errorf("printed %d questions, want 5", len(request.Questions))
	}
	if !strings.Contains(errOut.String(), "Nothing was sent") {
		t.Errorf("digest did not say it sent nothing:\n%s", errOut.String())
	}
}

func TestDigestOnAFreshInstallSaysSo(t *testing.T) {
	s, _, _ := testSystem(t, "")
	if err := run([]string{"digest"}, s); err == nil {
		t.Error("digest reported success with no Session recorded")
	}
}

func TestDigestTakesANamedSession(t *testing.T) {
	s, out, _ := testSystem(t, "")
	feed(t, s.home, failedStep)
	if err := run([]string{"digest", "abc123"}, s); err != nil {
		t.Fatalf("digest abc123: %v", err)
	}
	if !strings.Contains(out.String(), `"steps"`) {
		t.Errorf("no Digest printed:\n%s", out.String())
	}
	if err := run([]string{"digest", "no-such-session"}, s); err == nil {
		t.Error("digest invented a Session that does not exist")
	}
}
