package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

// The promise is not "the paths we tested are safe", it is "every path is
// safe". These are every shape whoa can currently be handed, from both
// Harnesses, each carrying the same secret and the same file content, and
// none of them may let either through.
func TestEveryToolArgumentShapeRoutesThroughRedaction(t *testing.T) {
	const secret = "sk-abcdefghijklmnopqrstuvwxyz0123"
	const content = "the quick brown fox jumps over the lazy dog"

	shapes := map[string]string{
		"claude code bash":              `{"command":"deploy --token ` + secret + `"}`,
		"claude code edit":              `{"file_path":"/tmp/a.ts","old_string":"` + content + `","new_string":"` + secret + `"}`,
		"claude code write":             `{"file_path":"/tmp/a.ts","content":"` + content + ` ` + secret + `"}`,
		"claude code multiedit":         `{"file_path":"/tmp/a.ts","edits":[{"old_string":"` + content + `","new_string":"` + secret + `"}]}`,
		"claude code notebook":          `{"notebook_path":"/tmp/a.ipynb","new_source":"` + secret + `"}`,
		"codex shell argv":              `{"command":["deploy","--token","` + secret + `"]}`,
		"codex apply patch":             `{"path":"/tmp/a.ts","patch":"--- a\n+++ b\n+` + secret + `\n-` + content + `"}`,
		"codex tool output":             `{"stdout":"` + content + `","stderr":"` + secret + `","aggregated_output":"` + content + `"}`,
		"a subagent nesting it":         `{"agent":{"tool_input":{"command":"deploy --token ` + secret + `"}}}`,
		"a field invented next release": `{"someFutureField":"` + secret + ` ` + content + `"}`,
	}

	for name, payload := range shapes {
		t.Run(name, func(t *testing.T) {
			got := FromToolInput(json.RawMessage(payload))
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), secret) {
				t.Errorf("a secret escaped\n  in: %s\n out: %s", payload, encoded)
			}
			if strings.Contains(string(encoded), content) {
				t.Errorf("file content escaped\n  in: %s\n out: %s", payload, encoded)
			}
		})
	}
}

// Tool output is walked and dropped, not merely unmatched: whoa has no reason
// to carry what a command printed, and Codex hands it a lot of it.
func TestToolOutputIsDroppedEntirely(t *testing.T) {
	got := FromToolInput(json.RawMessage(`{"stdout":"expect(x).toBe(1)","stderr":"it.skip('a')","aggregated_output":"assert x"}`))
	if !got.Empty() {
		t.Errorf("tool output produced %+v, want nothing", got)
	}
}

func TestEditsAreReadThroughTheirEnclosingPath(t *testing.T) {
	got := FromToolInput(json.RawMessage(`{"file_path":"src/auth_test.go","edits":[
		{"old_string":"require.NoError(t, err)","new_string":"t.Skip(\"flaky\")"},
		{"old_string":"assert.Equal(t, 1, n)","new_string":""}
	]}`))
	if !got.Signals.TestFile {
		t.Error("TestFile = false; the path is on the enclosing object, not the edit")
	}
	if got.Signals.AssertionsRemoved != 2 {
		t.Errorf("AssertionsRemoved = %d, want 2", got.Signals.AssertionsRemoved)
	}
	if got.Signals.SkipMarkersAdded != 1 {
		t.Errorf("SkipMarkersAdded = %d, want 1", got.Signals.SkipMarkersAdded)
	}
}

func TestMalformedToolArgumentsYieldNothing(t *testing.T) {
	for _, raw := range []string{"", "not json", "null", "[]", "{}", `"a string"`} {
		if got := FromToolInput(json.RawMessage(raw)); !got.Empty() {
			t.Errorf("FromToolInput(%q) = %+v, want nothing", raw, got)
		}
	}
}

// FuzzSignalsNeverEchoTheirInput is the audit that does not rely on anyone
// thinking of the right example. Whatever arbitrary text arrives, under
// whatever key, no run of it may appear in the Signals whoa keeps.
//
// This is stated over Signals alone, because the two promises differ. A
// Signal must retain nothing of what it read. A redacted command deliberately
// retains what is not secret, which is the whole of ADR 0003; what it must
// never retain is the secret, and that is the property below it.
func FuzzSignalsNeverEchoTheirInput(f *testing.F) {
	f.Add("content", "expect(user).toBe(null)")
	f.Add("stdout", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow==\n")
	f.Add("old_string", "assert x == 1")
	f.Add("file_path", "/Users/paul/secret/auth_test.ts")
	f.Add("patch", "--- a\n+++ b\n+it.skip('x')\n-assert(y)")

	const runLength = 8

	f.Fuzz(func(t *testing.T, key, value string) {
		payload, err := json.Marshal(map[string]string{key: value})
		if err != nil {
			t.Skip()
		}
		encoded, err := json.Marshal(FromToolInput(payload).Signals)
		if err != nil {
			t.Fatal(err)
		}
		kept := string(encoded)
		for i := 0; i+runLength <= len(value); i++ {
			run := value[i : i+runLength]
			if strings.TrimSpace(run) == "" {
				continue
			}
			if strings.Contains(kept, run) {
				t.Fatalf("input echoed into a Signal\n key: %q\n run: %q\n out: %s", key, run, kept)
			}
		}
	})
}

// FuzzSecretsNeverSurviveSurroundingText is the command-side promise: a
// credential stays gone however it is packed, however it is quoted, and
// whatever arbitrary text is wrapped around it.
func FuzzSecretsNeverSurviveSurroundingText(f *testing.F) {
	f.Add("deploy ", " --dry-run")
	f.Add("curl -H \"Authorization: Bearer ", "\" https://x")
	f.Add("", "")
	f.Add("FOO=", " bar")

	const secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz"

	f.Fuzz(func(t *testing.T, before, after string) {
		payload, err := json.Marshal(map[string]string{"command": before + secret + after})
		if err != nil {
			t.Skip()
		}
		encoded, err := json.Marshal(FromToolInput(payload))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("a secret survived\n before: %q\n  after: %q\n    out: %s", before, after, encoded)
		}
	})
}
