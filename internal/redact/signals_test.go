package redact

import (
	"encoding/json"
	"testing"
)

func TestAssertionsRemovedAreCounted(t *testing.T) {
	removed := `
	expect(user.name).toBe("bob");
	assert.equal(total, 42);
	self.assertEqual(total, 42)
	t.Fatalf("want %d", want)
	EXPECT_EQ(total, 42);
	require.NoError(t, err)
`
	got := fromEdit("auth_test.go", removed, "")
	if got.AssertionsRemoved != 6 {
		t.Errorf("AssertionsRemoved = %d, want 6", got.AssertionsRemoved)
	}
	if got.AssertionsAdded != 0 {
		t.Errorf("AssertionsAdded = %d, want 0", got.AssertionsAdded)
	}
	if got.NetAssertionDelta() != -6 {
		t.Errorf("NetAssertionDelta = %d, want -6", got.NetAssertionDelta())
	}
}

func TestSkipMarkersAddedAreCounted(t *testing.T) {
	added := `
	it.skip("logs in", () => {})
	xdescribe("auth", () => {})
	@pytest.mark.skip(reason="flaky")
	t.Skip("flaky")
	#[ignore]
	@Disabled
	describe.only("just this one", () => {})
`
	got := fromEdit("auth.test.ts", "", added)
	if got.SkipMarkersAdded != 7 {
		t.Errorf("SkipMarkersAdded = %d, want 7", got.SkipMarkersAdded)
	}
}

// `.only` is a skip marker wearing a different hat: it does not disable the
// test it marks, it disables every other one in the file.
func TestFocusingOneTestCountsAsSkipping(t *testing.T) {
	if got := fromEdit("a.spec.js", "", `it.only("x", () => {})`); got.SkipMarkersAdded != 1 {
		t.Errorf("SkipMarkersAdded = %d, want 1", got.SkipMarkersAdded)
	}
}

func TestTestFilesAreRecognisedAcrossLanguages(t *testing.T) {
	tests := map[string]bool{
		"internal/core/observe_test.go":   true,
		"src/auth.test.ts":                true,
		"src/auth.spec.tsx":               true,
		"tests/test_login.py":             true,
		"spec/models/user_spec.rb":        true,
		"src/__tests__/auth.js":           true,
		"app/src/test/java/AuthTest.java": true,
		"internal/core/observe.go":        false,
		"src/auth.ts":                     false,
		"src/latest.go":                   false,
		"README.md":                       false,
	}
	for path, want := range tests {
		if got := fromEdit(path, "", "").TestFile; got != want {
			t.Errorf("TestFile(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestADiffSplitsIntoAddedAndRemoved(t *testing.T) {
	patch := `--- a/auth_test.go
+++ b/auth_test.go
@@ -1,5 +1,5 @@
 func TestLogin(t *testing.T) {
-	require.NoError(t, err)
-	assert.Equal(t, 200, res.Code)
+	t.Skip("flaky")
 }
`
	got := FromPatch("auth_test.go", patch)
	if got.AssertionsRemoved != 2 {
		t.Errorf("AssertionsRemoved = %d, want 2", got.AssertionsRemoved)
	}
	if got.SkipMarkersAdded != 1 {
		t.Errorf("SkipMarkersAdded = %d, want 1", got.SkipMarkersAdded)
	}
	if !got.TestFile {
		t.Error("TestFile = false, want true")
	}
	// The `---`/`+++` headers are not content and must not be counted as lines.
	if got.LinesAdded != 1 || got.LinesRemoved != 2 {
		t.Errorf("lines +%d -%d, want +1 -2", got.LinesAdded, got.LinesRemoved)
	}
}

// This is the whole privacy promise, stated as one assertion: whatever goes
// in, what comes out is numbers and flags. Every Signal is machine-checked to
// be a number or a boolean, so no future field can quietly carry text.
func TestSignalsAreOnlyEverNumbersAndFlags(t *testing.T) {
	sensitive := "sk-abcdefghijklmnopqrstuvwxyz0123 /Users/paul/secret/auth.ts password=hunter2"
	got := fromEdit("/Users/paul/secret/auth_test.ts", sensitive, sensitive)

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for name, value := range fields {
		switch value.(type) {
		case float64, bool:
		default:
			t.Errorf("Signal %q is %T; Signals may only be numbers or flags", name, value)
		}
	}
}
