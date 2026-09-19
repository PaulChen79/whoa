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

You can read the whole of this promise in one place: `internal/redact`.

Better, check it. `whoa digest` prints the exact payload whoa would send to
the Judge, for one of your own real sessions, and sends nothing. It needs no
API key and makes no network call. Read it before you decide whether to turn
the Judge on.

## Harnesses

| | Claude Code | Codex |
|---|---|---|
| Observes Steps | yes | yes |
| Detects a failed Step | from the event | read from `tool_response` — see below |
| Records the Goal | yes | yes |
| Nudges before the next Step | yes | yes |
| Needs a trust step after install | no | **yes** — run `/hooks` in Codex |

Codex reports successful and failed tool calls on one event and does not
document how a failure is expressed, so whoa infers it from the fields Codex's
tools are observed to send. Where it cannot tell, it records success. That
means whoa can miss a Codex failure and be slower to notice a Loop there. See
[ADR 0005](docs/adr/0005-codex-outcome-from-tool-response.md).

Run `whoa doctor` to check whoa is actually running. It reports, per Harness,
whether the hook is registered, whether an administrator policy
(`allowManagedHooksOnly`, `allow_managed_hooks_only`, `disableAllHooks`) is
silently disabling it, and when it last actually observed a Step. It exits
non-zero when nothing is running, so it can be used as a check.

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

### The Judge

Outside Counter-only Mode, whoa asks [Jev](https://typesafe.ai) five fixed
questions about the Digest and records the answers as a Verdict. Set a key:

```sh
export WHOA_JEV_API_KEY=...
```

Without one, whoa degrades to Counters and says so once per Session. Set
`on_missing_key` to `"error"` if you would rather be told plainly that you are
unprotected. Degrading never makes whoa louder than the Mode you chose: Shadow
Mode without a key watches nothing and stays silent.

The call runs in a detached process. No Step ever waits for it.

**The wire format is unverified.** This project has never held a Jev key, so
the request shape follows the published description of the primitives rather
than a schema anyone has exercised against the live service. `whoa digest`
prints exactly what would be sent so you can check it against your own account
before trusting it. A mismatch degrades that Step to Counters; it does not
break the tool.

## Halt

The exceptional case. whoa denies one step with a reason, using explicit
`permissionDecision` JSON — never an exit code, because on Claude Code exit 2
is irreversible and a stop-loss you cannot talk out of a mistake is worse
than none.

Denying one step is deliberately weaker than ending the turn: the agent
stops, is told why, and decides again, so it can still rescue itself and you
are not dragged in to restart anything.

Halt fires when a Verdict crosses `halt_threshold` (0.88), or after
`ineffective_nudges_before_halt` nudges about the same thing have been
ignored. The second is the one that matters in practice.

Backoff is counted in code and the gap doubles each time: 3 steps, then 6,
then 12. If whoa has halted twice and the agent is still going, whoa is not
what is going to fix this, and it gets quieter rather than louder. A halt is
always shown to you, whatever `notify` says.

`halt_enabled = false` gives a nudge-only posture. **Halt has no release
gate**, because there will never be enough samples to calibrate it; the
compensating decision is that the threshold is conservative and halts stay
rare.

## Is it any good?

Nobody knows yet, including the author. whoa's judgement is unproven, so it
ships with the means to check it rather than a claim:

```sh
whoa wrong       # the last thing whoa said was wrong
whoa calibrate   # how often has it been wrong?
```

`calibrate` reports the false positive rate over every Nudge-level Verdict you
have labelled, against the release gate: **50 samples, at most 20% false
positives**. It exits non-zero until that is met. Fifty is nowhere near
statistically significant; it is enough to catch "this does not work at all",
which is the only question v0 has to answer.

Precision is what matters here and recall is not. A Misjudgment gets whoa
uninstalled. A miss just leaves you where you already were.

Logs holding a Misjudgment are never expired, whatever `retention_days` says.

## Parameters

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
| `model` | `"jev-1.13.0"` | Which Jev model judges. Pinned, not floating: Verdicts are calibration data, and a model that changes underneath a corpus makes it incomparable. |
| `state_dir` | `~/.whoa` | Where the Session logs and this config live. `~` is expanded. |
| `retention_days` | `14` | How long Session logs are kept. `0` keeps them forever. |
| `on_missing_key` | `"degrade"` | With no API key, `degrade` falls back to Counter-only Mode; `error` tells you that you are unprotected. |

Everything above is a **Parameter**: getting it wrong makes whoa noisy or dull
and nothing worse. Behaviours that would break a promise whoa makes — that the
log never carries your code, that a Halt never ends your turn — are
**Invariants** and are deliberately not configurable. See
[ADR 0004](docs/adr/0004-parameters-and-invariants.md).

## When whoa gets it wrong

Run `whoa wrong`. It marks the last thing whoa said as a Misjudgment, against
the exact line of the log that produced it, and tells you what it marked.

That mark is the point. Whether whoa's judgement is any good is not something
its author can assert; it is something a corpus of disputed Verdicts can show.
The corpus only exists if producing it costs one word at the moment of
annoyance, so that is what it costs.

Session logs expire after `retention_days` (default 14). A log holding a
Misjudgment is kept regardless, so marking a Verdict also preserves the
evidence behind it.

## Design

- [`CONTEXT.md`](CONTEXT.md) — the vocabulary. Start here.
- [`docs/adr/`](docs/adr/) — the decisions that were hard to reverse.

## Licence

MIT
