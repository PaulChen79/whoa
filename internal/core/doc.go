// Package core is whoa's pure decision core, and the seam every behavioural
// test goes through.
//
// It takes a raw Harness hook payload, the Session log so far, and the
// configuration, and returns what to tell the Harness plus what to append to
// the log. It performs no I/O: no files, no network, no clock. That is what
// lets tests be data in, data out, with no test doubles.
//
// The core is entered at two points because the Judge call is I/O in the
// middle of a decision. The first entry point either decides on its own or
// reports that a Digest needs judging; the caller performs the network call
// and comes back through the second. The HTTP client therefore stays outside
// the core without the core needing an injected interface.
//
// Input is the *raw* payload rather than a normalised Step on purpose. The
// differences between Claude Code and Codex (tool_result versus tool_response,
// whether PostToolUse can block, prompt_id versus turn_id) are then covered by
// tests that go through this seam, instead of needing a seam of their own.
package core
