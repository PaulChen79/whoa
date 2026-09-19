package judge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PaulChen79/whoa/internal/core"
)

// cannedJudge stands in for the Judge. Every test in this package points at
// one; nothing here reaches the network.
func cannedJudge(t *testing.T, status int, body string, seen *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if seen != nil {
			*seen = string(raw)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTheJudgesAnswersAreRead(t *testing.T) {
	srv := cannedJudge(t, http.StatusOK, `{
		"model": "jev-1.13.0",
		"answers": {
			"same_intent": {"probability": 0.91},
			"goal_drift":  {"probability": 0.12},
			"evading":     {"probability": 0.88},
			"needs_human": {"probability": 0.30},
			"progress":    {"value": 2.4, "confidence": 0.71}
		}
	}`, nil)

	client := Client{BaseURL: srv.URL, APIKey: "k", Model: "jev-1.13.0"}
	got, err := client.Ask(context.Background(), Assemble(nil, core.Defaults()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "jev-1.13.0" {
		t.Errorf("model = %q", got.Model)
	}
	if got.Answers["same_intent"].Probability != 0.91 {
		t.Errorf("same_intent = %v, want 0.91", got.Answers["same_intent"].Probability)
	}
	if got.Answers["progress"].Value != 2.4 {
		t.Errorf("progress = %v, want 2.4 — a Score can land between levels", got.Answers["progress"].Value)
	}
}

// Pinning a version is what stops judgements shifting underneath a user.
func TestTheModelIsSentAndTheAnsweringModelIsReturned(t *testing.T) {
	var sent string
	srv := cannedJudge(t, http.StatusOK, `{"model":"jev-1.13.0","answers":{"evading":{"probability":0.5}}}`, &sent)

	client := Client{BaseURL: srv.URL, APIKey: "k", Model: "jev-1.13.0"}
	got, err := client.Ask(context.Background(), Assemble(nil, core.Defaults()))
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]any
	if err := json.Unmarshal([]byte(sent), &request); err != nil {
		t.Fatal(err)
	}
	if request["model"] != "jev-1.13.0" {
		t.Errorf("the pinned model was not sent: %v", request["model"])
	}
	if got.Model != "jev-1.13.0" {
		t.Errorf("the answering model was not returned: %q", got.Model)
	}
}

func TestTheKeyIsSentAndNeverTheUsersWork(t *testing.T) {
	var sent string
	srv := cannedJudge(t, http.StatusOK, `{"model":"m","answers":{"evading":{"probability":0.1}}}`, &sent)

	log := []core.Entry{{Kind: core.KindGoal, Goal: "fix auth"}}
	client := Client{BaseURL: srv.URL, APIKey: "secret-key"}
	if _, err := client.Ask(context.Background(), Assemble(log, core.Defaults())); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sent, "secret-key") {
		t.Error("the API key was put in the request body")
	}
	if !strings.Contains(sent, "fix auth") {
		t.Errorf("the Goal did not reach the Judge: %s", sent)
	}
}

// Every one of these means "no Verdict for this Step", never "stop whoa".
func TestEveryJudgeFailureIsAnError(t *testing.T) {
	tests := map[string]*httptest.Server{
		"a refusal":           cannedJudge(t, http.StatusTooManyRequests, `{"error":"rate limited"}`, nil),
		"a server error":      cannedJudge(t, http.StatusInternalServerError, `nope`, nil),
		"unreadable JSON":     cannedJudge(t, http.StatusOK, `{"answers":`, nil),
		"an empty answer set": cannedJudge(t, http.StatusOK, `{"model":"m","answers":{}}`, nil),
	}
	for name, srv := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := (Client{BaseURL: srv.URL, APIKey: "k"}).
				Ask(context.Background(), Assemble(nil, core.Defaults())); err == nil {
				t.Error("reported success")
			}
		})
	}
}

func TestNoKeyIsItsOwnError(t *testing.T) {
	client := Client{BaseURL: "http://127.0.0.1:1", APIKey: ""}
	_, err := client.Ask(context.Background(), Assemble(nil, core.Defaults()))
	if err != ErrNoKey {
		t.Errorf("err = %v, want ErrNoKey; a missing key is a situation, not a fault", err)
	}
}

// A refused connection is the plane case, and it must look like every other
// failure rather than hanging or panicking.
func TestAnUnreachableJudgeIsAnError(t *testing.T) {
	if _, err := (Client{BaseURL: "http://127.0.0.1:1", APIKey: "k"}).
		Ask(context.Background(), Assemble(nil, core.Defaults())); err == nil {
		t.Error("reported success against a closed port")
	}
}
