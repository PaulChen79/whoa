package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/PaulChen79/whoa/internal/config"
	"github.com/PaulChen79/whoa/internal/core"
	"github.com/PaulChen79/whoa/internal/store"
)

// wrongCmd marks the most recent Verdict as a Misjudgment.
//
// This is the only thing whoa ever asks a person to do, and it is the whole
// calibration story: a corpus of disputed Verdicts is the only way to find out
// whether whoa's judgement is any good, and it exists only if producing it
// costs one word at the moment of annoyance. Anything that has to be done
// later, in another tool, does not get done.
func wrongCmd(s system) error {
	cfg, err := config.Load(s.home)
	if err != nil {
		warn(s, err)
	}

	found, total, err := lastVerdict(cfg.StateDir)
	if err != nil {
		return err
	}
	if found.line == 0 {
		if total > 0 {
			fmt.Fprintf(s.stdout, "Every Verdict whoa has given is already marked (%d of them).\n", total)
		} else {
			fmt.Fprintln(s.stdout, "whoa has not said anything yet, so there is nothing to mark.")
		}
		fmt.Fprintf(s.stdout, "Verdicts are recorded in %s.\n", filepath.Join(cfg.StateDir, "sessions"))
		return nil
	}

	mark := core.Entry{
		Kind:      core.KindWrong,
		Timestamp: s.now().UTC(),
		Harness:   found.entry.Harness,
		Session:   found.session,
		Turn:      found.entry.Turn,
		Ref:       found.line,
	}
	if err := store.Append(cfg.StateDir, &mark); err != nil {
		return err
	}

	fmt.Fprintf(s.stdout, "Marked as a Misjudgment: %s\n", found.entry.Fact)
	fmt.Fprintf(s.stdout, "  said %s, in session %s\n", humanAge(s.now().Sub(found.entry.Timestamp)), found.session)
	fmt.Fprintf(s.stdout, "  line %d of %s\n", found.line,
		filepath.Join(cfg.StateDir, "sessions", found.session+".jsonl"))
	fmt.Fprintln(s.stdout, "  everything before that line is the evidence it was given")
	fmt.Fprintln(s.stdout, "  this log is now exempt from expiry, so the evidence survives")
	return nil
}

// verdictRef is one Verdict and where it lives.
type verdictRef struct {
	session string
	entry   core.Entry
	line    int
}

// lastVerdict finds the most recent unmarked Verdict across every Session, and
// how many Verdicts there are in total.
//
// It searches every log rather than the current Session because there is no
// current Session: `whoa wrong` is typed by a person in a terminal, not by a
// Harness with a payload. The most recent Verdict is the one they just saw.
//
// The total is what lets the caller tell "whoa has never said anything" apart
// from "you have already marked everything it said". Those reach here as the
// same outcome and deserve very different sentences.
func lastVerdict(stateDir string) (verdictRef, int, error) {
	logs, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.jsonl"))
	if err != nil {
		return verdictRef{}, 0, err
	}

	var (
		best  verdictRef
		total int
	)
	for _, path := range logs {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var (
			verdicts []verdictRef
			marked   = map[int]bool{}
			session  = strings.TrimSuffix(filepath.Base(path), ".jsonl")
		)
		for i, text := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
			if text == "" {
				continue
			}
			var e core.Entry
			if json.Unmarshal([]byte(text), &e) != nil {
				continue
			}
			switch {
			case e.Kind == core.KindWrong && e.Ref > 0:
				marked[e.Ref] = true
			case e.Kind == core.KindNudge:
				verdicts = append(verdicts, verdictRef{session, e, i + 1})
			}
		}
		total += len(verdicts)

		// Walking back from the end, because marking twice should reach the
		// Verdict before rather than skipping the Session entirely.
		for i := len(verdicts) - 1; i >= 0; i-- {
			if marked[verdicts[i].line] {
				continue
			}
			if best.line == 0 || verdicts[i].entry.Timestamp.After(best.entry.Timestamp) {
				best = verdicts[i]
			}
			break
		}
	}
	return best, total, nil
}
