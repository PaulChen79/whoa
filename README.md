# whoa

A stop-loss for coding agents.

An agent that has lost the plot rarely says so. It keeps going: retrying the
same failing command, editing further and further from what you asked, or
quietly redefining the job into something it can finish. whoa watches the tool
calls, notices when that is happening, and says something.

It is a hook. It runs in Claude Code and Codex, it is a single static binary
with no runtime to install, and by default it costs nothing and sends nothing
anywhere.

> **Status: v0, under construction.** Counter-only Mode works today. The Judge
> is not wired up yet. See the [issue tracker](https://github.com/PaulChen79/whoa/issues).

## Install

```sh
go install github.com/PaulChen79/whoa/cmd/whoa@latest
whoa install
```

`whoa install` merges its hooks into your existing `~/.claude/settings.json`,
leaving every other setting exactly as it was. `whoa uninstall` takes them out
again. Both are idempotent.

## What it does

whoa records one line per tool call in a Session log under `~/.whoa/sessions/`.
It counts things that are true without needing an opinion — how many times the
same tool has failed in a row, how long the current run of one tool is — and
when a count crosses a threshold, it says so once, in the agent's context,
before the next tool call.

The log never contains your source code, your file contents, your file paths,
or the text of the commands that ran. It holds tool names, outcomes and
timings. You can read it yourself: it is one JSON object per line.

## Privacy

In the default Mode, whoa makes no network calls at all.

Whatever the Mode, your source code never leaves the machine. whoa reads a
tool's arguments and keeps two things from them, and nothing else:

**Signals** — counts and flags, never text. From an edit whoa keeps how many
assertions it added and removed, how many skip markers it added, how many
lines changed, and whether the file was a test. It does not keep the code, the
file path, or the file name. Every Signal field is machine-checked to be a
number or a boolean, so a field that carried text would fail the build.

**Commands**, after Redaction. Inline environment values, credentials in URLs,
authorization and cookie headers, values after flags like `--token`, published
key shapes (`sk-`, `ghp_`, `AKIA`, `xoxb-`, JWTs and others) and unstructured
blobs of 32 characters or more are replaced with `[redacted]`. When whoa cannot
find where a secret ends — a heredoc, a key body, an unbalanced quote, anything
spanning more than one line — it keeps the program name and discards the entire
rest of the command.

Tool output is read and dropped. So is every field whoa does not recognise,
including ones added by a future release of your agent: the rule is that
nothing is kept unless it became a Signal or a redacted command.

You can read the whole of this promise in one place: `internal/redact`. Run
`whoa digest` to print exactly what would be sent, and send nothing.

## Configuration

whoa reads `~/.whoa/config.json` if it exists. Every key is optional; anything
you leave out keeps its default. A missing file is not an error.

```json
{
  "mode": "counters",
  "window": 50,
  "trigger": { "repeated_failures": 4 }
}
```

### Parameters

| Key | Default | What it does |
| --- | --- | --- |
| `mode` | `"counters"` | `counters` decides from Counters alone and never calls the Judge. `shadow` records what the Judge would have said and never intervenes. `full` acts on the Judge's Verdicts. |
| `window` | `50` | How many recent Steps the Counters look at. Smaller forgets sooner; larger keeps a long-dead loop alive. |
| `trigger.repeated_failures` | `4` | Nudge when the same tool has failed this many times in a row. `0` disables it. |
| `trigger.repeated_tool` | `12` | Nudge when the same tool has run this many times in a row, whatever the outcome. `0` disables it. |
| `trigger.failed_steps` | `20` | Nudge when this many Steps in the Window failed. `0` disables it. |
| `nudge_threshold` | `0.60` | The probability at or above which a Judge Verdict earns a Nudge. |
| `halt_threshold` | `0.88` | The probability at or above which a Judge Verdict earns a Halt. |
| `halt_enabled` | `true` | Whether whoa may block a Step at all. `false` leaves it able only to Nudge. |
| `ineffective_nudges_before_halt` | `3` | How many Nudges may be ignored before whoa escalates to a Halt. |
| `notify` | `true` | Whether whoa prints its one-line explanation to you. `false` leaves the agent nudged and you unbothered. |
| `model` | `"jev-latest"` | Which Jev model judges. |
| `state_dir` | `~/.whoa` | Where the Session logs and this config live. `~` is expanded. |
| `retention_days` | `14` | How long Session logs are kept. `0` keeps them forever. |
| `on_missing_key` | `"degrade"` | With no API key, `degrade` falls back to Counter-only Mode; `error` tells you that you are unprotected. |

Everything above is a **Parameter**: getting it wrong makes whoa noisy or dull
and nothing worse. Behaviours that would break a promise whoa makes — that the
log never carries your code, that a Halt never ends your turn — are
**Invariants** and are deliberately not configurable. See
[ADR 0004](docs/adr/0004-parameters-and-invariants.md).

## Design

- [`CONTEXT.md`](CONTEXT.md) — the vocabulary. Start here.
- [`docs/adr/`](docs/adr/) — the decisions that were hard to reverse.

## Licence

MIT
