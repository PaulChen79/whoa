package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PaulChen79/whoa/internal/config"
	"github.com/PaulChen79/whoa/internal/judge"
	"github.com/PaulChen79/whoa/internal/store"
)

// digestCmd prints exactly what whoa would send to the Judge, and sends
// nothing.
//
// This exists so the privacy claim can be checked rather than believed. A
// person deciding whether to turn the Judge on can read the entire payload for
// their own real Session first, on their own machine, with no key configured
// and no request made.
func digestCmd(s system, args []string) error {
	cfg, err := config.Load(s.home)
	if err != nil {
		warn(s, err)
	}

	session := ""
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			session = arg
		}
	}
	if session == "" {
		session, err = mostRecentSession(cfg.StateDir)
		if err != nil {
			return err
		}
	}
	if session == "" {
		return fmt.Errorf("no Session has been recorded in %s yet", filepath.Join(cfg.StateDir, "sessions"))
	}

	log, err := store.Load(cfg.StateDir, session)
	if err != nil {
		return err
	}
	if len(log) == 0 {
		return fmt.Errorf("session %s has no Steps", session)
	}

	request := judge.Assemble(log, cfg)
	out, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return err
	}

	fmt.Fprintf(s.stderr, "Session %s, %d Steps, ~%d tokens. Nothing was sent.\n",
		session, len(request.State.Steps), judge.EstimatedTokens(request))
	_, err = fmt.Fprintf(s.stdout, "%s\n", out)
	return err
}

// mostRecentSession is the Session most recently written to.
//
// A person running this in a terminal means "the one I was just watching", and
// the most recently touched log is the only thing on disk that knows which
// that was.
func mostRecentSession(stateDir string) (string, error) {
	logs, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.jsonl"))
	if err != nil || len(logs) == 0 {
		return "", err
	}
	sort.Slice(logs, func(i, j int) bool {
		a, errA := os.Stat(logs[i])
		b, errB := os.Stat(logs[j])
		if errA != nil || errB != nil {
			return false
		}
		return a.ModTime().After(b.ModTime())
	})
	return strings.TrimSuffix(filepath.Base(logs[0]), ".jsonl"), nil
}
