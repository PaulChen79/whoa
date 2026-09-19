package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Endpoint is where the Judge lives.
const Endpoint = "https://api.typesafe.ai/v1/systemone"

// Timeout bounds one Judge call. The published end-to-end latency is 70-500ms;
// this is long enough that a slow call still returns and short enough that a
// hung one does not leave a process sitting around for the rest of the day.
//
// Nothing waits on this. The call runs in a detached process precisely so that
// the timeout is never a Step's problem.
const Timeout = 20 * time.Second

// Answer is what the Judge returns for one question.
//
// A Noul returns Probability alone: there is no separate confidence field
// because the probability is the uncertainty. A Score returns a
// probability-weighted Value that can land between the legend's levels, with
// its own Confidence.
type Answer struct {
	Probability float64 `json:"probability,omitempty"`
	Value       float64 `json:"value,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
}

// UnmarshalJSON accepts "score" as well as "value" for a Score's answer.
//
// whoa has never held a key, so the exact spelling is not something this
// project can confirm. Reading both costs nothing and turns one plausible
// mismatch into a non-event; the alternative is a Score that silently reads
// zero, which looks exactly like a real answer of zero.
func (a *Answer) UnmarshalJSON(b []byte) error {
	var raw struct {
		Probability float64  `json:"probability"`
		Value       *float64 `json:"value"`
		Score       *float64 `json:"score"`
		Confidence  float64  `json:"confidence"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	a.Probability, a.Confidence = raw.Probability, raw.Confidence
	switch {
	case raw.Value != nil:
		a.Value = *raw.Value
	case raw.Score != nil:
		a.Value = *raw.Score
	}
	return nil
}

// Response is the Judge's reply.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
}

// Client calls the Judge.
//
// The wire shape below follows the published description of the primitives,
// not a schema whoa has exercised against the live service: this project has
// never held a key. `whoa digest` prints what would be sent so that the shape
// can be checked by anyone who has one, and a mismatch degrades that Step to
// Counter-only rather than breaking the tool. See the README.
type Client struct {
	// BaseURL allows the tests to stand up a server locally. Every test in
	// this package points at one; none of them reaches the network.
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// request is the wire form.
type request struct {
	Model     string     `json:"model"`
	State     Digest     `json:"state"`
	Questions []Question `json:"questions"`
}

// Ask sends one Digest and its questions, and returns what the Judge said.
func (c Client) Ask(ctx context.Context, r Request) (Response, error) {
	if c.APIKey == "" {
		return Response{}, ErrNoKey
	}
	url := c.BaseURL
	if url == "" {
		url = Endpoint
	}

	body, err := json.Marshal(request{Model: c.Model, State: r.State, Questions: r.Questions})
	if err != nil {
		return Response{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: Timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	// Bounded because a Verdict is a handful of numbers, and an endpoint that
	// returns a gigabyte is a problem whoa should not participate in.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Response{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("judge returned %s", resp.Status)
	}

	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return Response{}, fmt.Errorf("judge returned unreadable JSON: %w", err)
	}
	if len(out.Answers) == 0 {
		return Response{}, fmt.Errorf("judge returned no answers")
	}
	return out, nil
}

// ErrNoKey is returned when no API key is configured. It is a distinct error
// because it is the one failure that is a user's ordinary situation rather
// than a fault, and on_missing_key decides what to do about it.
var ErrNoKey = fmt.Errorf("no Judge API key configured")
