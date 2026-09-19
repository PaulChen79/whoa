package redact

import (
	"strings"
	"testing"
)

// Every secret in this table is fake. They are shaped like the real thing
// because the rules key off shape, and a test that used a placeholder would
// prove only that the placeholder was handled.
func TestSecretsNeverSurviveRedaction(t *testing.T) {
	secrets := []struct {
		name    string
		command string
		secret  string
	}{
		{"an inline environment value", `API_TOKEN=abc123def456 npm run deploy`, "abc123def456"},
		{"any inline value, secret-shaped or not", `DATABASE_URL=postgres://bob:hunter2@db/app psql`, "hunter2"},
		{"a bearer token in a header", `curl -H "Authorization: Bearer abcdefghijklmnop" https://api.example.com`, "abcdefghijklmnop"},
		{"a whole authorization header", `curl -H "Authorization: Basic Ym9iOmh1bnRlcjI=" https://api.example.com`, "Ym9iOmh1bnRlcjI="},
		{"an api key header", `curl -H "X-Api-Key: sk-proj-0123456789abcdefghij" https://api.example.com`, "sk-proj-0123456789abcdefghij"},
		{"a value after a secret flag", `deploy --token ghp_0123456789abcdefghijABCDEFGHIJ0123 --dry-run`, "ghp_0123456789abcdefghijABCDEFGHIJ0123"},
		{"a value joined to a secret flag", `deploy --password=hunter2correct --dry-run`, "hunter2correct"},
		{"credentials in a url", `git clone https://bob:hunter2secret@github.com/acme/app.git`, "hunter2secret"},
		{"an openai key anywhere", `echo sk-abcdefghijklmnopqrstuvwxyz0123 > /dev/null`, "sk-abcdefghijklmnopqrstuvwxyz0123"},
		{"an anthropic key", `run --key sk-ant-api03-abcdefghijklmnopqrstuvwxyz`, "sk-ant-api03-abcdefghijklmnopqrstuvwxyz"},
		{"an aws access key id", `aws configure set x AKIAIOSFODNN7EXAMPLE`, "AKIAIOSFODNN7EXAMPLE"},
		{"a slack token", `slack-cli --auth xoxb-1234567890-abcdefghijkl`, "xoxb-1234567890-abcdefghijkl"},
		{"a google api key", `curl "https://maps.googleapis.com/x?key=AIzaSyA0123456789abcdefghijklmnopqrstuv"`, "AIzaSyA0123456789abcdefghijklmnopqrstuv"},
		{"a gitlab token", `glab auth login --token glpat-0123456789abcdefghij`, "glpat-0123456789abcdefghij"},
		{"an npm token", `npm config set //registry.npmjs.org/:_authToken npm_0123456789abcdefghijklmnopqrstuvwxyz`, "npm_0123456789abcdefghijklmnopqrstuvwxyz"},
		{"a json web token", `curl -H "Cookie: s=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"`, "dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"},
		{"an opaque blob nobody can name", `./upload 9f8e7d6c5b4a39281706f5e4d3c2b1a09f8e7d6c5b4a3928`, "9f8e7d6c5b4a39281706f5e4d3c2b1a09f8e7d6c5b4a3928"},
	}

	for _, s := range secrets {
		t.Run(s.name, func(t *testing.T) {
			got := Command(s.command)
			if strings.Contains(got.Text, s.secret) {
				t.Errorf("secret survived Redaction\n command: %s\n      in: %s", s.command, got.Text)
			}
		})
	}
}

// ADR 0003: command text is worth carrying only if enough of it survives to
// tell two Steps apart. A rule that redacted everything would pass the test
// above and be useless.
func TestRedactionKeepsWhatIsNotSecret(t *testing.T) {
	kept := []struct {
		command string
		want    []string
	}{
		{`API_TOKEN=abc123def456 npm run deploy`, []string{"npm", "run", "deploy"}},
		{`curl -H "Authorization: Bearer abcdefghijklmnop" https://api.example.com/v1/users`, []string{"curl", "https://api.example.com/v1/users"}},
		{`go test ./internal/core/ -run TestNudge -race`, []string{"go", "test", "./internal/core/", "-run", "TestNudge", "-race"}},
		{`deploy --token ghp_0123456789abcdefghijABCDEFGHIJ0123 --dry-run`, []string{"deploy", "--token", "--dry-run"}},
		{`git clone https://bob:hunter2secret@github.com/acme/app.git`, []string{"git", "clone", "github.com/acme/app.git"}},
	}

	for _, k := range kept {
		got := Command(k.command)
		if got.Degraded {
			t.Errorf("%q degraded, but its secrets are all boundable", k.command)
			continue
		}
		for _, want := range k.want {
			if !strings.Contains(got.Text, want) {
				t.Errorf("Redaction dropped %q\n from: %s\n   to: %s", want, k.command, got.Text)
			}
		}
	}
}

// ADR 0003: "when the boundaries of the secret cannot be determined, the
// command degrades to argv[0]". These are the shapes where they cannot.
func TestUnboundableCommandsDegradeToArgv0(t *testing.T) {
	unboundable := []struct {
		name    string
		command string
		argv0   string
	}{
		{"a heredoc carries a whole file", "cat > .env <<'EOF'\nAPI_KEY=abc123\nEOF", "cat"},
		{"a multi-line command is writing something", "echo one\necho two", "echo"},
		{"an unbalanced quote cannot be parsed", `echo "unterminated`, "echo"},
		{"a private key body", "echo '-----BEGIN RSA PRIVATE KEY-----' > id_rsa", "echo"},
		{"an enormous single token", "./upload " + strings.Repeat("x", 300), "./upload"},
	}

	for _, u := range unboundable {
		t.Run(u.name, func(t *testing.T) {
			got := Command(u.command)
			if !got.Degraded {
				t.Errorf("did not degrade: %s", got.Text)
			}
			if got.Text != u.argv0 {
				t.Errorf("degraded to %q, want %q", got.Text, u.argv0)
			}
		})
	}
}

// Degrading is the safe direction, but argv[0] itself is attacker-adjacent
// text, so it gets the same treatment rather than being trusted.
func TestDegradingNeverLeaksThroughArgv0(t *testing.T) {
	got := Command("sk-abcdefghijklmnopqrstuvwxyz0123\nsecond line")
	if strings.Contains(got.Text, "sk-abcdefghijklmnopqrstuvwxyz0123") {
		t.Errorf("a secret survived as argv[0]: %s", got.Text)
	}
}

func TestAnEmptyCommandIsNotAnError(t *testing.T) {
	if got := Command("   "); got.Text != "" || got.Degraded {
		t.Errorf("Command(blank) = %+v, want an empty, undegraded result", got)
	}
}

// Codex runs every command through a shell; Claude Code does not. If the
// wrapper survived, the same repeated command would count as a Loop on one
// Harness and not on the other.
func TestShellWrappersComeOffSoBothHarnessesAgree(t *testing.T) {
	tests := map[string]string{
		`/bin/zsh -lc "npm test"`:               "npm test",
		`/bin/bash -c "go test ./..."`:          "go test ./...",
		`sh -c "make build && make check"`:      "make build && make check",
		`npm test`:                              "npm test",
		`/bin/zsh`:                              "/bin/zsh",
		`/usr/bin/env node script.js`:           "/usr/bin/env node script.js",
		`zsh -lc "API_KEY=abc123def456 deploy"`: "API_KEY=[redacted] deploy",
	}
	for command, want := range tests {
		if got := Command(command); got.Text != want {
			t.Errorf("Command(%q).Text = %q, want %q", command, got.Text, want)
		}
	}
}

// Degrading a shell invocation must not report every Step as /bin/zsh.
func TestDegradingLooksPastTheShellWrapper(t *testing.T) {
	got := Command("/bin/zsh -lc \"cat > .env <<'EOF'\nsecret\nEOF\"")
	if !got.Degraded {
		t.Fatal("a heredoc must degrade")
	}
	if got.Text != "cat" {
		t.Errorf("degraded to %q, want the command the shell was asked to run", got.Text)
	}
}
