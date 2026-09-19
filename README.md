# whoa

A stop-loss for coding agents.

An agent that has lost the plot rarely says so. It keeps going: retrying the
same failing command, editing further and further from what you asked, or
quietly redefining the job into something it can finish. whoa watches the tool
calls, notices when that is happening, and says something.

It is a hook. It runs in Claude Code and Codex, it is a single static binary
with no runtime to install, and by default it costs nothing and sends nothing
anywhere.

> **Status: v0.** Counter-only Mode is finished and is the default. The Judge
> is implemented and works end to end, but **its judgement is unproven**: the
> release gate is 50 hand-labelled Verdicts at a false positive rate of 20% or
> better, and the corpus currently stands at 0. Run `whoa calibrate` to see
> where it is. Until then, `counters` is the mode to trust.

## Install

```sh
go install github.com/PaulChen79/whoa/cmd/whoa@latest
whoa install
whoa doctor
```

`whoa install` registers its hooks with every Harness it finds: Claude Code in
`~/.claude/settings.json`, Codex in `~/.codex/hooks.json`. It merges into what
is already there and leaves every other setting exactly as it was, key order
included. `whoa uninstall` takes them out again. Both are idempotent, and
neither touches a Harness that is not installed.

**Codex needs one more step.** Codex will not run a hook it has not been told
to trust, and it fails quietly rather than loudly:

```
codex          # then run /hooks, review whoa, and trust it
```

`whoa doctor` is how you find out whether any of this actually worked. It
reports, per Harness, whether the hook is registered, whether an administrator
policy is silently disabling it, and when it last really observed a Step. It
exits non-zero when nothing is running, so you can put it in a check. Run it
once after installing: an unregistered hook and a registered-but-untrusted one
look identical from the outside, and both leave you believing you are
protected when you are not.

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

**Signals** — counts and flags, never text. This is the complete list:

| Signal | Type |
|---|---|
| `test_file` | boolean — was the edited file a test |
| `assertions_added` | count |
| `assertions_removed` | count |
| `skip_markers_added` | count — `it.skip`, `xit`, `@pytest.mark.skip`, `t.Skip`, `#[ignore]`, `.only` and friends |
| `lines_added` | count |
| `lines_removed` | count |

That is all of it. Not the code, not the file path, not the file name. Every
field is a number or a boolean, and a fuzz test asserts that no input of any
shape produces a Signal containing a fragment of that input.

**Commands**, after Redaction. The complete list of rules:

| Rule | Example in | Kept |
|---|---|---|
| Inline environment values | `API_KEY=abc deploy` | `API_KEY=[redacted] deploy` |
| Values after a secret flag | `--token ghp_…` | `--token [redacted]` |
| Credentials in a URL | `https://bob:pw@host/x` | `https://[redacted]@host/x` |
| Authorization, cookie and API-key headers | `Authorization: Bearer …` | `Authorization: [redacted]` |
| Published key shapes | `sk-`, `sk-ant-`, `ghp_`/`gho_`/`ghu_`/`ghs_`/`ghr_`, `github_pat_`, `glpat-`, `AKIA`, `ASIA`, `xoxb-`/`xoxp-`/`xoxa-`/`xoxr-`/`xoxs-`/`xoxe-`, `AIza`, `npm_`, JWTs | `[redacted]` |
| Opaque blobs of 32+ characters | a 48-character hex string | `[redacted]` |

whoa unwraps `sh -c` / `zsh -lc` wrappers first, so a Codex command and a
Claude Code command for the same work read the same.

When whoa cannot tell where a secret *ends*, it keeps nothing but the program
name. That happens for a heredoc, a private key body, an unbalanced quote, any
single token over 256 characters, and anything spanning more than one line —
all the shapes where file contents get embedded in a command line. A worse
Verdict on a few Steps is an acceptable price; a leaked key is not.

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
| Hook config | `~/.claude/settings.json` | `~/.codex/hooks.json` |
| Observes Steps | yes | yes |
| Separate event for a failed Step | yes, `PostToolUseFailure` | no — inferred from `tool_response` |
| Records the Goal | yes | yes |
| Nudges before the next Step | yes | yes |
| Halts a Step | `permissionDecision: "deny"` | `permissionDecision: "deny"` |
| **`PostToolUse` can block** | **no** | **yes** |
| Trust step after install | no | **yes** — run `/hooks` in Codex |
| Administrator kill switch | `allowManagedHooksOnly`, `disableAllHooks` | `allow_managed_hooks_only` |

Codex can block a tool call *after* it has run and Claude Code cannot. whoa
does not use that. It halts only before a Step, on both, so the two behave
identically and nothing whoa does depends on a capability only one Harness
has. The asymmetry is listed because you should read it here rather than
discover it.

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

**whoa's own result so far: 0 samples.** No corpus has been collected, so the
honest answer to "is the Judge any good?" is that nobody knows. That is why
`counters` is the default and `full` is not.

**And the Judge's own numbers are not independent evidence.** Every published
Jev evaluation is vendor-self-reported. There is no paper, no reliability
curve, and no expected calibration error. The claim whoa leans on — that the
returned probability is calibrated — is exactly the claim with no third-party
measurement behind it. If it does not hold, Counter-only Mode is the product,
and Counter-only Mode is finished, free and offline.

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
and nothing worse. A test fails if a Parameter exists in the code and not in
that table, so there is no undocumented knob.

## Invariants

These are not configurable, and each one has a reason. An Invariant without a
reason reads as an oversight.

| Invariant | Why |
|---|---|
| The Digest never carries file contents, source or paths | It is the whole promise. A setting to turn it off is a setting someone will turn off by accident, and the damage is not recoverable. |
| Redaction degrades to the program name when a secret cannot be bounded | The alternative is a knob whose wrong setting leaks a key. The right move is picking the default correctly, not delegating the choice. |
| The agent never sees a probability | It would argue with the number instead of reconsidering the work. The number is not evidence it can act on. |
| Questions to the Judge are always English | The model is weaker in CJK. Asking in your language would quietly degrade the judgement while looking like a courtesy. Your Goal is stored and sent exactly as you wrote it. |
| A Halt never ends your Turn | It denies one Step. The agent has to be able to rescue itself, and you should not have to restart anything because whoa was wrong. |
| `progress` never affects a Verdict | It is a general impression, not a named failure mode. Acting on it would make "why did whoa just interrupt me?" unanswerable, and explicability is the precondition for leaving this installed. It is recorded for calibration. |

See [ADR 0004](docs/adr/0004-parameters-and-invariants.md).

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
