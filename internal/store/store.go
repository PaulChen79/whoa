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
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

// maxLineBytes bounds how long a single log line may be when reading back. No
// line whoa writes comes close; the limit is here so that a corrupted log
// cannot make whoa allocate without bound on the path of every Step.
const maxLineBytes = 1 << 20

// Append writes one Step as one line to its Session's log, creating the log
// and its directory if needed.
func Append(stateDir string, s *core.Entry) error {
	if s == nil {
		return fmt.Errorf("no entry to append")
	}
	// An Entry with no Kind reads back as nothing in particular, which would
	// make it invisible to the Counters and unreplayable. Better to refuse to
	// write it and say so.
	if s.Kind == "" {
		return fmt.Errorf("entry has no kind")
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

// Load reads a Session's log back, oldest first.
//
// A missing log is an empty Session, not an error: the first Step of a Session
// is observed before any log exists.
//
// A line that cannot be parsed is skipped rather than failing the read. The
// log is appended to by concurrent hook processes and may be truncated by a
// crash mid-write, and losing one Step's worth of evidence is much better than
// whoa going blind for the rest of the Session.
func Load(stateDir, session string) ([]core.Entry, error) {
	path, err := logPath(stateDir, session)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening session log: %w", err)
	}
	defer f.Close()

	var entries []core.Entry
	scanner := bufio.NewScanner(f)
	// Steps are small, but a long log line must not stop the scan dead.
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e core.Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return entries, fmt.Errorf("reading session log: %w", err)
	}
	return entries, nil
}

// Sessions lists the Session ids that have logs, in no particular order.
//
// It is the one place that knows how logs are laid out on disk. Retention,
// `whoa wrong` and `whoa calibrate` all need to walk them, and three copies
// of the same glob is three places to forget when the layout changes.
func Sessions(stateDir string) ([]string, error) {
	logs, err := sessionLogs(stateDir)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(logs))
	for _, path := range logs {
		ids = append(ids, strings.TrimSuffix(filepath.Base(path), ".jsonl"))
	}
	return ids, nil
}

// sessionLogs lists the log files themselves, for the one caller that works
// on paths rather than ids: expiry, which deletes them.
func sessionLogs(stateDir string) ([]string, error) {
	return filepath.Glob(filepath.Join(stateDir, "sessions", "*.jsonl"))
}
