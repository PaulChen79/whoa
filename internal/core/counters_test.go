package core

import "testing"

// step is a compact way to write a Session log in a test table.
func step(tool string, outcome Outcome) Entry {
	return Entry{Kind: KindStep, Tool: tool, Outcome: outcome}
}

func log(entries ...Entry) []Entry { return entries }

func TestCountersOverTheWindow(t *testing.T) {
	tests := []struct {
		name   string
		log    []Entry
		window int
		want   Counters
	}{
		{
			name:   "an empty log counts nothing",
			log:    nil,
			window: 50,
			want:   Counters{},
		},
		{
			name:   "a single successful Step",
			log:    log(step("Bash", OutcomeOK)),
			window: 50,
			want:   Counters{Steps: 1, Tool: "Bash", RepeatedTool: 1},
		},
		{
			name: "consecutive failures of the same tool are a Loop",
			log: log(
				step("Bash", OutcomeError),
				step("Bash", OutcomeError),
				step("Bash", OutcomeError),
			),
			window: 50,
			want: Counters{
				Steps: 3, Tool: "Bash",
				RepeatedFailures: 3, RepeatedTool: 3, FailedSteps: 3,
			},
		},
		{
			name: "a success breaks the failure run but not the tool run",
			log: log(
				step("Bash", OutcomeError),
				step("Bash", OutcomeError),
				step("Bash", OutcomeOK),
			),
			window: 50,
			want: Counters{
				Steps: 3, Tool: "Bash",
				RepeatedFailures: 0, RepeatedTool: 3, FailedSteps: 2,
			},
		},
		{
			name: "a different tool breaks both runs",
			log: log(
				step("Bash", OutcomeError),
				step("Bash", OutcomeError),
				step("Edit", OutcomeError),
			),
			window: 50,
			want: Counters{
				Steps: 3, Tool: "Edit",
				RepeatedFailures: 1, RepeatedTool: 1, FailedSteps: 3,
			},
		},
		{
			name: "the Window drops Steps that fall off the end",
			log: log(
				step("Bash", OutcomeError),
				step("Bash", OutcomeError),
				step("Edit", OutcomeOK),
				step("Edit", OutcomeOK),
			),
			window: 2,
			want: Counters{
				Steps: 2, Tool: "Edit",
				RepeatedFailures: 0, RepeatedTool: 2, FailedSteps: 0,
			},
		},
		{
			name: "Counters survive a Turn boundary",
			log: log(
				Entry{Kind: KindStep, Turn: "t1", Tool: "Bash", Outcome: OutcomeError},
				Entry{Kind: KindStep, Turn: "t1", Tool: "Bash", Outcome: OutcomeError},
				// The user said "continue"; the Turn changed, the Loop did not.
				Entry{Kind: KindStep, Turn: "t2", Tool: "Bash", Outcome: OutcomeError},
			),
			window: 50,
			want: Counters{
				Steps: 3, Tool: "Bash",
				RepeatedFailures: 3, RepeatedTool: 3, FailedSteps: 3,
			},
		},
		{
			name: "whoa's own Nudges are not evidence of a Loop",
			log: log(
				step("Bash", OutcomeError),
				Entry{Kind: KindNudge, Counter: "repeated_failures", Count: 1},
				step("Bash", OutcomeError),
			),
			window: 50,
			want: Counters{
				Steps: 2, Tool: "Bash",
				RepeatedFailures: 2, RepeatedTool: 2, FailedSteps: 2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Count(tt.log, tt.window)
			if got != tt.want {
				t.Errorf("Count() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCountingIsReproducible(t *testing.T) {
	entries := log(
		step("Bash", OutcomeError),
		step("Bash", OutcomeError),
		step("Edit", OutcomeOK),
		step("Bash", OutcomeError),
	)
	first := Count(entries, 50)
	for i := 0; i < 100; i++ {
		if got := Count(entries, 50); got != first {
			t.Fatalf("replay %d produced %+v, want %+v", i, got, first)
		}
	}
}

// Saying "continue" must not wipe the evidence. Impatience is itself a loop
// signal, so a Turn boundary is invisible to the Counters.
func TestCountersSurviveATurnBoundary(t *testing.T) {
	inTurn := func(turn string, e Entry) Entry {
		e.Turn = turn
		return e
	}
	entries := []Entry{
		inTurn("turn-1", step("Bash", OutcomeError)),
		inTurn("turn-1", step("Bash", OutcomeError)),
		// the user says "continue", and the agent goes right back to it
		inTurn("turn-2", step("Bash", OutcomeError)),
		inTurn("turn-2", step("Bash", OutcomeError)),
	}

	got := Count(entries, 50)
	if got.RepeatedFailures != 4 {
		t.Errorf("RepeatedFailures across two Turns = %d, want 4", got.RepeatedFailures)
	}
	if got.Steps != 4 {
		t.Errorf("Steps across two Turns = %d, want 4", got.Steps)
	}
}

// whoa's own entries are not the agent's Steps. If they counted, every Nudge
// would push the Counters further towards the next one, and whoa would talk
// itself into a loop about a loop.
func TestWhoaNeverCountsItsOwnActivity(t *testing.T) {
	entries := []Entry{
		step("Bash", OutcomeError),
		{Kind: KindNudge, Counter: "repeated_failures", Count: 1},
		step("Bash", OutcomeError),
		{Kind: KindNudge, Counter: "repeated_failures", Count: 2},
	}

	got := Count(entries, 50)
	if got.Steps != 2 {
		t.Errorf("Steps = %d, want 2: only the agent's tool calls are Steps", got.Steps)
	}
	if got.RepeatedFailures != 2 {
		t.Errorf("RepeatedFailures = %d, want 2: a Nudge must not break or pad the run", got.RepeatedFailures)
	}
}
