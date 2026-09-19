package redact

import (
	"regexp"
	"strings"
)

// Redacted is command text that is safe to carry in a Digest.
//
// Degraded says the text is argv[0] alone, because somewhere in the original
// there was a secret whose end could not be found. A Digest reports this so a
// Judge is not left to wonder why a Step looks so bare.
type Redacted struct {
	Text     string `json:"text,omitempty"`
	Degraded bool   `json:"degraded,omitempty"`
}

const placeholder = "[redacted]"

// maxToken is the length past which a single argument stops looking like an
// argument. Nothing a person types by hand reaches it; embedded file content
// and long credentials both do.
const maxToken = 256

// secretShapes are credentials recognisable on sight, wherever they appear.
// Each one is a published, documented prefix, which is what makes matching
// them by shape honest rather than guesswork.
var secretShapes = regexp.MustCompile(strings.Join([]string{
	`sk-ant-[A-Za-z0-9_-]{16,}`,
	`sk-[A-Za-z0-9_-]{16,}`,
	`gh[pousr]_[A-Za-z0-9]{16,}`,
	`github_pat_[A-Za-z0-9_]{20,}`,
	`glpat-[A-Za-z0-9_-]{16,}`,
	`AKIA[0-9A-Z]{16}`,
	`ASIA[0-9A-Z]{16}`,
	`xox[baprse]-[A-Za-z0-9-]{10,}`,
	`AIza[0-9A-Za-z_-]{30,}`,
	`npm_[A-Za-z0-9]{30,}`,
	`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`,
}, "|"))

// opaqueBlob is a whole argument with no structure to it: no dots, no slashes,
// nothing that makes it read as a path, a version or a hostname. At this
// length it is a key, a hash or a token, and whoa cannot tell which.
//
// Slashes are excluded so that long paths survive, which costs the ability to
// spot a base64 secret containing one. That is the cheaper mistake: paths are
// on most commands, and a base64 secret is nearly always introduced by a flag
// or an assignment, both of which are redacted by position regardless.
var opaqueBlob = regexp.MustCompile(`^[A-Za-z0-9+_=-]{32,}$`)

// urlCredentials is the user:password@ in a URL.
var urlCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]+@`)

// assignment is NAME=value, the shape of an inline environment variable. The
// whole value goes, secret-looking or not: NODE_ENV=production is not worth
// the judgement call about which names are safe.
var assignment = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)

// secretHeaders carry a credential as their entire value.
var secretHeaders = map[string]bool{
	"authorization": true, "proxy-authorization": true, "cookie": true,
	"set-cookie": true, "x-api-key": true, "api-key": true, "apikey": true,
	"x-auth-token": true, "auth-token": true, "x-access-token": true,
	"private-token": true, "x-amz-security-token": true,
}

// secretFlags take their secret as the next argument, or after an `=`.
//
// Only long flags are listed. A short flag is too ambiguous to key off: `-p`
// is a password to one program and a port to the next, and redacting ports
// would cost more signal than it protects.
var secretFlags = map[string]bool{
	"--token": true, "--password": true, "--passwd": true, "--pass": true,
	"--secret": true, "--api-key": true, "--apikey": true, "--auth": true,
	"--access-token": true, "--auth-token": true, "--private-key": true,
	"--credential": true, "--credentials": true, "--client-secret": true,
	"--session-token": true, "--bearer": true,
}

// unboundable is text whose end whoa cannot find: a heredoc body, a key body,
// or any command spread over more than one line. In every case the argument
// boundaries stop meaning anything, so the whole command is given up.
func unboundable(raw string) bool {
	return strings.ContainsAny(raw, "\n\r") ||
		strings.Contains(raw, "<<") ||
		strings.Contains(raw, "-----BEGIN")
}

// Command removes secrets from command text, degrading to argv[0] when it
// cannot find where a secret ends. See ADR 0003.
func Command(raw string) Redacted {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Redacted{}
	}
	if unboundable(raw) {
		return degrade(raw)
	}
	tokens, ok := tokenise(raw)
	if !ok {
		return degrade(raw)
	}
	for _, t := range tokens {
		if len(t) > maxToken {
			return degrade(raw)
		}
	}

	out := make([]string, 0, len(tokens))
	redactNext := false
	for _, t := range tokens {
		switch {
		case redactNext:
			out = append(out, placeholder)
			redactNext = false
		default:
			if secretFlags[strings.ToLower(t)] {
				redactNext = true
			}
			out = append(out, token(t))
		}
	}
	return Redacted{Text: join(out)}
}

// degrade keeps the program name and nothing else. argv[0] is text whoa did
// not write either, so it goes through the same rules on the way out.
func degrade(raw string) Redacted {
	argv0 := strings.TrimSpace(raw)
	if i := strings.IndexAny(argv0, " \t\n\r"); i >= 0 {
		argv0 = argv0[:i]
	}
	return Redacted{Text: token(argv0), Degraded: true}
}

// token redacts one argument in place.
func token(t string) string {
	if m := assignment.FindStringSubmatch(t); m != nil && m[2] != "" {
		return m[1] + "=" + placeholder
	}
	if name, value, ok := strings.Cut(t, "="); ok && value != "" && secretFlags[strings.ToLower(name)] {
		return name + "=" + placeholder
	}
	if name, _, ok := strings.Cut(t, ":"); ok && secretHeaders[strings.ToLower(strings.TrimSpace(name))] {
		return strings.TrimSpace(name) + ": " + placeholder
	}
	if opaqueBlob.MatchString(t) {
		return placeholder
	}
	t = urlCredentials.ReplaceAllString(t, "${1}"+placeholder+"@")
	return secretShapes.ReplaceAllString(t, placeholder)
}

// join puts the arguments back together, quoting the ones that need it so the
// result still reads as one command.
func join(tokens []string) string {
	quoted := make([]string, len(tokens))
	for i, t := range tokens {
		if strings.ContainsAny(t, " \t") {
			t = `"` + t + `"`
		}
		quoted[i] = t
	}
	return strings.Join(quoted, " ")
}

// tokenise splits a command the way a shell would, enough to find argument
// boundaries. It reports failure rather than guessing: unbalanced quotes mean
// whoa does not know where anything ends, which is the whole trigger for
// degrading.
func tokenise(s string) ([]string, bool) {
	var (
		tokens  []string
		current strings.Builder
		quote   rune
		started bool
	)
	flush := func() {
		if started {
			tokens = append(tokens, current.String())
			current.Reset()
			started = false
		}
	}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case c == '\\' && quote != '\'' && i+1 < len(runes):
			i++
			current.WriteRune(runes[i])
			started = true
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				current.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote = c
			started = true
		case c == ' ' || c == '\t':
			flush()
		default:
			current.WriteRune(c)
			started = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	flush()
	return tokens, true
}
