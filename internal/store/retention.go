package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PaulChen79/whoa/internal/core"
)

// sweepMarker records when logs were last expired. Its modification time is
// the whole of its content.
const sweepMarker = ".last-sweep"

// sweepInterval is how often expiry is worth doing. A Session can run
// thousands of Steps in a day and every one of them spawns this binary;
// walking the log directory each time would cost far more than it saves.
const sweepInterval = 24 * time.Hour

// SweepDue reports whether logs are due to be expired.
func SweepDue(stateDir string, now time.Time) bool {
	info, err := os.Stat(filepath.Join(stateDir, sweepMarker))
	if err != nil {
		return true
	}
	return now.Sub(info.ModTime()) >= sweepInterval
}

// MarkSwept records that expiry has just run.
func MarkSwept(stateDir string, now time.Time) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(stateDir, sweepMarker)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chtimes(path, now, now)
}

// Expire removes Session logs last written more than days ago, and reports how
// many it removed. A retention of zero or less turns expiry off.
//
// A log holding a Misjudgment is kept however old it is. Marking a Verdict as
// wrong is the one thing whoa asks of a user that costs them something, and
// the marked logs together are the only evidence that whoa's judgement is any
// good. Deleting them on a timer would throw that away silently.
func Expire(stateDir string, days int) (int, error) {
	if days <= 0 {
		return 0, nil
	}
	logs, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.jsonl"))
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().AddDate(0, 0, -days)

	removed := 0
	for _, path := range logs {
		info, err := os.Stat(path)
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		marked, err := holdsAMisjudgment(path)
		if err != nil || marked {
			continue
		}
		if os.Remove(path) == nil {
			removed++
		}
	}
	return removed, nil
}

// holdsAMisjudgment reports whether a log records a Verdict the user disputed.
//
// This reads the file rather than tracking marks elsewhere, so the log stays
// the single record: nothing can say a log is marked when it is not, or lose
// the knowledge that it was.
func holdsAMisjudgment(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		var e core.Entry
		if json.Unmarshal([]byte(line), &e) == nil && e.Kind == core.KindWrong {
			return true, nil
		}
	}
	return false, nil
}
