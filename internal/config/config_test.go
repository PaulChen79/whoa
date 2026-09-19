package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PaulChen79/whoa/internal/core"
)

// homeWith builds a home directory holding the given config file contents, or
// none at all when body is empty.
func homeWith(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	if body == "" {
		return home
	}
	dir := filepath.Join(home, ".whoa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, Name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestNoConfigFileIsNotAnError(t *testing.T) {
	home := homeWith(t, "")
	cfg, err := Load(home)
	if err != nil {
		t.Fatalf("Load() with no config file: %v", err)
	}
	if cfg.Mode != core.ModeCounters || cfg.Window != 50 {
		t.Errorf("Load() = %+v, want the defaults", cfg)
	}
	if cfg.StateDir != filepath.Join(home, ".whoa") {
		t.Errorf("state_dir = %q, want the default under home", cfg.StateDir)
	}
}

// Setting one Parameter must not silently reset the other eleven.
func TestOneSettingLeavesTheRestAlone(t *testing.T) {
	cfg, err := Load(homeWith(t, `{"window": 7}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Window != 7 {
		t.Errorf("window = %d, want 7", cfg.Window)
	}
	d := core.Defaults()
	if cfg.Mode != d.Mode || cfg.Notify != d.Notify || cfg.RetentionDays != d.RetentionDays {
		t.Errorf("setting window disturbed other Parameters: %+v", cfg)
	}
	if cfg.Trigger != d.Trigger {
		t.Errorf("trigger = %+v, want the default %+v", cfg.Trigger, d.Trigger)
	}
}

func TestOneTriggerFieldLeavesTheOthersAlone(t *testing.T) {
	cfg, err := Load(homeWith(t, `{"trigger": {"repeated_failures": 2}}`))
	if err != nil {
		t.Fatal(err)
	}
	d := core.Defaults()
	if cfg.Trigger.RepeatedFailures != 2 {
		t.Errorf("repeated_failures = %d, want 2", cfg.Trigger.RepeatedFailures)
	}
	if cfg.Trigger.RepeatedTool != d.Trigger.RepeatedTool {
		t.Errorf("repeated_tool = %d, want the default %d", cfg.Trigger.RepeatedTool, d.Trigger.RepeatedTool)
	}
}

// notify = false must take effect, which is the case a merge over defaults is
// easiest to get wrong.
func TestAFalseValueOverridesATrueDefault(t *testing.T) {
	cfg, err := Load(homeWith(t, `{"notify": false}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notify {
		t.Error("notify = false was ignored")
	}
}

func TestStateDirRelocatesLogsAndExpandsHome(t *testing.T) {
	home := homeWith(t, `{"state_dir": "~/elsewhere/logs"}`)
	cfg, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "elsewhere", "logs"); cfg.StateDir != want {
		t.Errorf("state_dir = %q, want %q", cfg.StateDir, want)
	}
}

// A config file that exists but cannot be understood must be loud. Falling
// back to defaults would leave someone believing a setting had taken effect.
func TestABrokenConfigFileIsAnError(t *testing.T) {
	if _, err := Load(homeWith(t, `{"window":`)); err == nil {
		t.Error("a truncated config file loaded without complaint")
	}
}

func TestInvalidValuesAreRejectedByName(t *testing.T) {
	tests := map[string]string{
		"mode":           `{"mode": "loud"}`,
		"on_missing_key": `{"on_missing_key": "maybe"}`,
		"window":         `{"window": 0}`,
		"retention_days": `{"retention_days": -1}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Load(homeWith(t, body))
			if err == nil {
				t.Fatalf("accepted %s", body)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error %q does not name the Parameter %q", err, name)
			}
		})
	}
}

// A config whoa could not parse must still say where to write, or a reported
// error turns into Session logs scattered through the user's working
// directories.
func TestEveryReturnCarriesAUsableStateDir(t *testing.T) {
	for name, body := range map[string]string{
		"broken":  `{"window":`,
		"invalid": `{"mode": "loud"}`,
	} {
		t.Run(name, func(t *testing.T) {
			home := homeWith(t, body)
			cfg, err := Load(home)
			if err == nil {
				t.Fatal("expected an error")
			}
			if cfg.StateDir != filepath.Join(home, ".whoa") {
				t.Errorf("state_dir = %q, want the default under home", cfg.StateDir)
			}
		})
	}
}

// A caller that reports the error and keeps watching must keep watching with
// values that behave. Left in place, `window: 0` does not narrow the Window,
// it removes the bound entirely.
func TestARejectedValueDoesNotSurviveTheRejection(t *testing.T) {
	tests := []struct {
		name string
		body string
		want func(core.Config) error
	}{
		{
			name: "a zero window falls back to the default rather than going unbounded",
			body: `{"window":0}`,
			want: func(c core.Config) error {
				if c.Window != core.Defaults().Window {
					return fmt.Errorf("window = %d, want the default %d", c.Window, core.Defaults().Window)
				}
				return nil
			},
		},
		{
			name: "a negative window likewise",
			body: `{"window":-5}`,
			want: func(c core.Config) error {
				if c.Window <= 0 {
					return fmt.Errorf("window = %d, want a usable one", c.Window)
				}
				return nil
			},
		},
		{
			name: "an unknown mode falls back to counters",
			body: `{"mode":"aggressive"}`,
			want: func(c core.Config) error {
				if c.Mode != core.ModeCounters {
					return fmt.Errorf("mode = %q, want %q", c.Mode, core.ModeCounters)
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := homeWith(t, tt.body)

			cfg, err := Load(home)

			if err == nil {
				t.Fatal("expected the value to be rejected out loud")
			}
			if e := tt.want(cfg); e != nil {
				t.Error(e)
			}
			if cfg.StateDir == "" {
				t.Error("state_dir must survive a rejection")
			}
		})
	}
}
