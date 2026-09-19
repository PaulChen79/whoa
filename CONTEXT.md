# whoa

whoa watches what a coding agent does and steps in when it loops, drifts off goal, or works around an obstacle instead of solving it. This document defines vocabulary only. Implementation lives in `docs/SPEC.md`; decisions live in `docs/adr/`.

## Language

### Units of time

**Step**:
One tool call. The smallest unit whoa observes.
_Avoid_: action, call, iteration

**Outcome**:
How a Step ended: it either succeeded or it failed. Carries no error text.
_Avoid_: status, result, exit code

**Turn**:
The period from one Substantive Instruction until the agent stops and waits. The main unit whoa reasons over.
_Avoid_: request, prompt, exchange

**Session**:
One connected period of a Harness, containing many Turns.
_Avoid_: conversation, thread

### Intent

**Substantive Instruction**:
A user message that changes the Goal.
_Avoid_: prompt, user message, instruction

**Continuation Signal**:
A user message that does not change the Goal and only extends the current Turn, such as "continue" or "go on".
_Avoid_: ack, filler

**Goal**:
What the current Turn is for: the most recent Substantive Instruction.
_Avoid_: task, objective, original goal, intent

### Failure modes

**Loop**:
The agent repeatedly retries the same already-failed thing by substantially the same means.
_Avoid_: repetition, stuck, thrash

**Drift**:
The agent's current work no longer serves the Goal.
_Avoid_: scope creep, yak shaving, wander

**Evasion**:
The agent works around an obstacle rather than resolving it, such as deleting an assertion to make a test pass.
_Avoid_: cheating, shortcut, gaming

### Evidence and judgement

**Session log**:
The ordered record of one Session: every Step whoa observed and every intervention it made. The only thing a Verdict can be replayed against.
_Avoid_: history, trace, audit log

**Counter**:
A deterministic fact code computes from the sequence of Steps. Carries no probability.
_Avoid_: metric, stat, heuristic

**Window**:
How many recent Steps the Counters cover. Fixed length; never reset at a Turn boundary.
_Avoid_: buffer, history, lookback

**Signal**:
A structured fact code extracts from a tool's input or output, such as "removed 3 assertions". Never contains file contents.
_Avoid_: feature, extract, fingerprint

**Redaction**:
Removal of suspected secrets from text by code before that text enters a Digest. Command text is the only source that needs Redaction; file contents never enter a Digest, so they need none.
_Avoid_: sanitize, scrub, mask

**Digest**:
The complete input sent to the Judge: the Goal, a summary of recent Steps, the Counters, and the Signals.
_Avoid_: context, payload, snapshot

**Judge**:
The model that turns a Digest into probabilities.
_Avoid_: model, classifier, evaluator

**Verdict**:
The conclusion produced by the Judge's probabilities together with the thresholds.
_Avoid_: decision, result, score

**Misjudgment**:
A Verdict the user has marked wrong after the fact.
_Avoid_: false positive, mistake

### Intervention

**Nudge**:
Injecting a warning back into the agent's context without interrupting it. whoa's primary instrument.
_Avoid_: warning, hint, reminder

**Halt**:
Blocking the agent's current Step and handing control back to the human. Does not end the Turn. The exception, not the rule.
_Avoid_: block, stop, kill, abort

### Environment

**Harness**:
The host program that runs a coding agent. Currently Claude Code and Codex.
_Avoid_: client, host, IDE, platform

### Operating modes

**Shadow Mode**:
Records Verdicts and never intervenes.
_Avoid_: dry run, passive, observe-only

**Counter-only Mode**:
Decides from Counters alone and never calls the Judge. Needs no API key.
_Avoid_: offline mode, local mode, free mode

### Configuration

**Parameter**:
A value the user can change in the config file. Getting it wrong makes whoa noisy or dull, nothing worse.
_Avoid_: option, setting, knob

**Invariant**:
A behaviour that is not open to configuration, because changing it would break a promise whoa makes. That is what makes it not a Parameter.
_Avoid_: constant, hardcoded, policy
