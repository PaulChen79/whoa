// Package store persists Steps to the Session log.
//
// One append-only JSONL file per Session, one line per Step. Append-only
// because a small write under O_APPEND is atomic enough not to need locking
// between the Harness's concurrent hooks, because there is no schema to
// migrate, and above all because replay comes free: a Verdict has to be
// reproducible from the log for a Misjudgment report to be worth anything.
//
// See docs/adr/0004-parameters-and-invariants.md for state_dir.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/PaulChen79/whoa/internal/core"
)

// dirMode and fileMode keep the log readable only by its owner: it is a record
// of what someone's agent did on their machine.
const (
	dirMode  os.FileMode = 0o700
	fileMode os.FileMode = 0o600
)

// Append writes one Step as one line to its Session's log, creating the log
// and its directory if needed.
func Append(stateDir string, s *core.Step) error {
	if s == nil {
		return fmt.Errorf("no step to append")
	}
	path, err := logPath(stateDir, s.Session)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("creating session directory: %w", err)
	}

	line, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshalling step: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("opening session log: %w", err)
	}
	defer f.Close()

	// One Write of one line under O_APPEND is what keeps concurrent hooks from
	// interleaving mid-line, so the line is assembled before it is written.
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("appending to session log: %w", err)
	}
	return f.Close()
}

// logPath resolves a Session's log file.
//
// The Session id comes from the Harness, which is to say from outside whoa, so
// it is rejected rather than sanitised if it could steer the write anywhere
// but into stateDir. Quietly rewriting it would mean two Sessions could
// collide on one log.
func logPath(stateDir, session string) (string, error) {
	if err := validSessionID(session); err != nil {
		return "", err
	}
	return filepath.Join(stateDir, "sessions", session+".jsonl"), nil
}

func validSessionID(session string) error {
	switch {
	case session == "":
		return fmt.Errorf("empty session id")
	case session == "." || session == "..":
		return fmt.Errorf("invalid session id %q", session)
	case strings.ContainsAny(session, `/\`):
		return fmt.Errorf("session id %q contains a path separator", session)
	case strings.Contains(session, ".."):
		return fmt.Errorf("session id %q contains %q", session, "..")
	case strings.ContainsRune(session, 0):
		return fmt.Errorf("session id contains a null byte")
	case strings.HasPrefix(session, "."):
		return fmt.Errorf("session id %q starts with a dot", session)
	}
	return nil
}
