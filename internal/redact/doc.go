// Package redact turns text into structured facts, and is whoa's second seam.
//
// It extracts Signals from diffs (assertions removed, skip markers added,
// whether the file is a test, net assertion delta) and removes secrets from
// command text, degrading a command to argv[0] when a secret's boundary cannot
// be determined.
//
// It is reachable through package core and still has its own test suite. The
// Signal list and the Redaction rules together are whoa's entire privacy
// promise: they are the complete set of what leaves the machine. Someone
// deciding whether to install whoa has to be able to read that set on its own,
// which they cannot do if it is only asserted by end-to-end tests.
//
// See docs/adr/0001-signals-not-source.md and
// docs/adr/0003-redacted-commands-in-digest.md.
package redact
