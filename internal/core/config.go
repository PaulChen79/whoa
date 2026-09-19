package core

// Mode is how much of whoa is switched on.
type Mode string

const (
	// ModeCounters decides from Counters alone and never calls the Judge.
	// It needs no API key and costs nothing, which is why it is the default:
	// shadow never gets switched on, and full can misjudge on day one.
	ModeCounters Mode = "counters"
	// ModeShadow records Verdicts and never intervenes.
	ModeShadow Mode = "shadow"
	// ModeFull acts on the Judge's Verdicts.
	ModeFull Mode = "full"
)

// What to do when there is no API key. Both are defensible: on a plane some
// people want a working tool, and some want to be told they are unprotected.
const (
	OnMissingKeyDegrade = "degrade"
	OnMissingKeyError   = "error"
)

// Trigger is the Counter condition that escalates: to a Nudge in Counter-only
// Mode, to a Judge call in full mode.
//
// It is one Parameter rather than one per mode because the condition worth
// escalating on does not change with what it escalates to. A user who tuned
// Counter-only Mode to their taste should not have to start again on upgrading.
//
// Each field is the count at or above which that Counter fires. Zero disables
// that Counter, so a Trigger of all zeroes is an inert whoa.
type Trigger struct {
	RepeatedFailures int `json:"repeated_failures"`
	RepeatedTool     int `json:"repeated_tool"`
	FailedSteps      int `json:"failed_steps"`
}

// Config is the full set of Parameters, as read from the config file.
//
// Everything here is a Parameter in the sense of docs/adr/0004: getting it
// wrong makes whoa noisy or dull and nothing worse. Behaviours that would
// break a promise whoa makes are Invariants and deliberately absent.
//
// All of them live in one struct, including the ones no code reads yet, so
// that the documented Parameter set and the implemented one can be compared
// mechanically rather than by eye.
type Config struct {
	Mode                        Mode    `json:"mode"`
	Window                      int     `json:"window"`
	Trigger                     Trigger `json:"trigger"`
	NudgeThreshold              float64 `json:"nudge_threshold"`
	HaltThreshold               float64 `json:"halt_threshold"`
	HaltEnabled                 bool    `json:"halt_enabled"`
	IneffectiveNudgesBeforeHalt int     `json:"ineffective_nudges_before_halt"`
	Notify                      bool    `json:"notify"`
	Model                       string  `json:"model"`
	StateDir                    string  `json:"state_dir"`
	RetentionDays               int     `json:"retention_days"`
	OnMissingKey                string  `json:"on_missing_key"`
}

// Defaults is whoa as it behaves before anyone configures it.
//
// The trigger defaults are deliberately unadventurous. A Misjudgment gets
// whoa uninstalled; a miss merely returns the user to the status quo they
// already live with, so the defaults are tuned for precision over recall.
func Defaults() Config {
	return Config{
		Mode:   ModeCounters,
		Window: 50,
		Trigger: Trigger{
			RepeatedFailures: 4,
			RepeatedTool:     12,
			FailedSteps:      20,
		},
		NudgeThreshold:              0.60,
		HaltThreshold:               0.88,
		HaltEnabled:                 true,
		IneffectiveNudgesBeforeHalt: 3,
		Notify:                      true,
		Model:                       "jev-latest",
		RetentionDays:               14,
		OnMissingKey:                OnMissingKeyDegrade,
	}
}
