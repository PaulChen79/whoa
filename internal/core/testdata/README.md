# Recorded payloads

Each file is a hook payload as a Harness actually sends it.

The Claude Code files come from the hook reference's own worked examples. The
Codex files are built from two sources together: the generated input schema
(`post-tool-use.command.input.schema.json`, which fixes the field names and
which are required) and the field shapes observed in real Codex session
rollouts on this machine, which is where `exit_code`, `status`, `stdout`,
`stderr` and the `{secs, nanos}` duration come from.

`tool_response` is schema-typed as any JSON at all, so the failure shape is the
one thing here not fixed by a contract. See ADR 0005.
