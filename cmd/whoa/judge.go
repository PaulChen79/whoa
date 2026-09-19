package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/PaulChen79/whoa/internal/config"
	"github.com/PaulChen79/whoa/internal/core"
	"github.com/PaulChen79/whoa/internal/judge"
	"github.com/PaulChen79/whoa/internal/store"
)

// keyEnv are the environment variables a Judge API key may arrive in.
var keyEnv = []string{"WHOA_JEV_API_KEY", "TYPESAFE_API_KEY"}

func apiKey() string {
	for _, name := range keyEnv {
		if key := os.Getenv(name); key != "" {
			return key
		}
	}
	return ""
}

// askJudge starts the Judge call and does not wait for it.
//
// The call runs in a separate, detached process rather than in a goroutine
// because this process is about to exit: the hook's whole job is to answer the
// Harness immediately. A goroutine would be killed on exit, and waiting for
// the call would put the Judge's latency on a Step, which is the one thing the
// ticket forbids. The Verdict lands in the log a moment later, which is where
// anything reads it from anyway.
func askJudge(s system, session string) {
	if s.binary == "" {
		warn(s, fmt.Errorf("cannot locate the whoa binary to ask the Judge"))
		return
	}
	cmd := exec.Command(s.binary, "judge", session)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		warn(s, err)
		return
	}
	// Deliberately no Wait. The child is orphaned and finishes on its own;
	// waiting is exactly what this must not do.
	_ = cmd.Process.Release()
}

// judgeCmd is the detached half: it asks the Judge and records the Verdict.
//
// Every failure here degrades to nothing. A Verdict that does not arrive means
// whoa saw this Step with Counters alone, which is the Mode it ships in; a
// network problem must never be the reason an agent is interrupted, and must
// never be the reason whoa crashes something the user is running.
func judgeCmd(s system, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: whoa judge <session>")
	}
	session := args[0]

	cfg, err := config.Load(s.home)
	if err != nil {
		warn(s, err)
	}
	if cfg.Mode == core.ModeCounters {
		return fmt.Errorf("mode is %q; the Judge is not consulted", cfg.Mode)
	}

	log, err := store.Load(cfg.StateDir, session)
	if err != nil {
		return err
	}
	if len(log) == 0 {
		return fmt.Errorf("session %s has no Steps", session)
	}

	client := judge.Client{APIKey: apiKey(), Model: cfg.Model, BaseURL: os.Getenv("WHOA_JEV_ENDPOINT")}
	ctx, cancel := context.WithTimeout(context.Background(), judge.Timeout)
	defer cancel()

	reply, err := client.Ask(ctx, judge.Assemble(log, cfg))
	if err != nil {
		// Recorded rather than raised. The user asked for the Judge, so a
		// Judge that is not answering is something they need to be able to
		// find out about, and the log is where they look.
		return record(s, cfg, &core.Entry{
			Kind: core.KindNotice, Timestamp: s.now().UTC(), Session: session,
			Fact: "the Judge could not be reached: " + err.Error(),
		})
	}

	probs, scores := answers(reply)
	return record(s, cfg, &core.Entry{
		Kind:          core.KindVerdict,
		Timestamp:     s.now().UTC(),
		Session:       session,
		Ref:           len(log),
		Model:         reply.Model,
		Probabilities: probs,
		Scores:        scores,
	})
}

// probabilities flattens the Judge's answers into the numbers a Verdict keeps.
//
// A Noul's probability and a Score's value live in one map because both are
// read the same way downstream: a number against a threshold. The legend that
// gives a Score its meaning is fixed in code, so it does not need storing
// beside every Verdict.
// answers splits what the Judge said into the two things it is: Noul
// probabilities, and Score values.
//
// They must not share a map. A Score of 1.8 on a five-level legend is not a
// probability of 1.8, and filing it under "probabilities" would quietly
// poison the calibration data that Shadow Mode exists to collect.
func answers(r judge.Response) (probabilities, scores map[string]float64) {
	kinds := make(map[string]judge.Kind, len(judge.Questions()))
	for _, q := range judge.Questions() {
		kinds[q.Key] = q.Kind
	}
	for key, a := range r.Answers {
		if kinds[key] == judge.Score {
			if scores == nil {
				scores = map[string]float64{}
			}
			scores[key] = a.Value
			continue
		}
		if probabilities == nil {
			probabilities = map[string]float64{}
		}
		probabilities[key] = a.Probability
	}
	return probabilities, scores
}

func record(s system, cfg core.Config, e *core.Entry) error {
	return store.Append(cfg.StateDir, e)
}
