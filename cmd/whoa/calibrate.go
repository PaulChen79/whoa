package main

import (
	"errors"
	"fmt"
	"sort"

	"github.com/PaulChen79/whoa/internal/config"
	"github.com/PaulChen79/whoa/internal/core"
	"github.com/PaulChen79/whoa/internal/store"
)

// errGateUnmet is returned so that a script can gate on the exit code.
var errGateUnmet = errors.New("release gate not met")

// Gate is the release gate on Nudging from a Verdict: fifty Nudge-level
// Verdicts, hand-labelled, with a false positive rate at or below 20%.
//
// Fifty is nowhere near statistically significant. It is enough to catch
// "this does not work at all", which is the only question v0 has to answer.
// Precision is what matters and recall is not: a Misjudgment gets whoa
// uninstalled, while a miss merely returns the person to the status quo.
const (
	GateSamples = 50
	GateMaxFP   = 0.20
)

// calibrateCmd reports how often whoa has been wrong, from the corpus the
// person built with `whoa wrong`.
//
// This exists so the gate is something anyone can check rather than something
// asserted in a ticket. It cannot be satisfied by writing code: it needs real
// Verdicts from real sessions, hand-labelled by the person they happened to.
func calibrateCmd(s system) error {
	cfg, err := config.Load(s.home)
	if err != nil {
		warn(s, err)
	}
	sessions, err := store.Sessions(cfg.StateDir)
	if err != nil {
		return err
	}
	sort.Strings(sessions)

	total, wrong := 0, 0
	for _, id := range sessions {
		log, err := store.Load(cfg.StateDir, id)
		if err != nil {
			warn(s, err)
			continue
		}
		marked := map[int]bool{}
		for _, e := range log {
			if e.Kind == core.KindWrong {
				marked[e.Ref] = true
			}
		}
		for i, e := range log {
			if e.Kind != core.KindVerdict || core.Strongest(e) < cfg.NudgeThreshold {
				continue
			}
			total++
			if marked[i+1] {
				wrong++
			}
		}
	}

	if total == 0 {
		fmt.Fprintf(s.stdout, "No Nudge-level Verdicts recorded yet.\n"+
			"Run in shadow or full mode, then mark the wrong ones with `whoa wrong`.\n"+
			"The gate needs %d.\n", GateSamples)
		return errGateUnmet
	}

	rate := float64(wrong) / float64(total)
	fmt.Fprintf(s.stdout, "Nudge-level Verdicts: %d (gate needs %d)\n", total, GateSamples)
	fmt.Fprintf(s.stdout, "Marked as Misjudgments: %d\n", wrong)
	fmt.Fprintf(s.stdout, "False positive rate: %.0f%% (gate allows %.0f%%)\n", rate*100, GateMaxFP*100)

	switch {
	case total < GateSamples:
		fmt.Fprintf(s.stdout, "\nNot enough evidence yet: %d more to go.\n", GateSamples-total)
		return errGateUnmet
	case rate > GateMaxFP:
		fmt.Fprintf(s.stdout, "\nGate not met. whoa is wrong too often to nudge on its Verdicts.\n")
		return errGateUnmet
	}
	fmt.Fprintf(s.stdout, "\nGate met.\n")
	return nil
}
