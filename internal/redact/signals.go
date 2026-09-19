package redact

import (
	"regexp"
	"strings"
)

// Signals are everything whoa may say about an edit it has seen.
//
// Every field is a count or a flag. That is not a stylistic preference: it is
// the privacy promise from ADR 0001, and it is enforced by a test that walks
// this struct's own JSON and rejects any field that is not a number or a
// boolean. Adding a string here would fail that test, which is the point.
type Signals struct {
	TestFile          bool `json:"test_file,omitempty"`
	AssertionsAdded   int  `json:"assertions_added,omitempty"`
	AssertionsRemoved int  `json:"assertions_removed,omitempty"`
	SkipMarkersAdded  int  `json:"skip_markers_added,omitempty"`
	LinesAdded        int  `json:"lines_added,omitempty"`
	LinesRemoved      int  `json:"lines_removed,omitempty"`
}

// NetAssertionDelta is how many assertions the edit added, less how many it
// took away. Negative is the interesting direction: it is what "deleted the
// test instead of fixing the code" looks like as a number.
func (s Signals) NetAssertionDelta() int { return s.AssertionsAdded - s.AssertionsRemoved }

// Empty reports whether anything was found worth saying.
func (s Signals) Empty() bool { return s == Signals{} }

// merge adds another edit's Signals to these, for tools that carry several.
func (s Signals) merge(o Signals) Signals {
	s.TestFile = s.TestFile || o.TestFile
	s.AssertionsAdded += o.AssertionsAdded
	s.AssertionsRemoved += o.AssertionsRemoved
	s.SkipMarkersAdded += o.SkipMarkersAdded
	s.LinesAdded += o.LinesAdded
	s.LinesRemoved += o.LinesRemoved
	return s
}

// assertions are the ways the major test frameworks say "this must hold".
var assertions = regexp.MustCompile(strings.Join([]string{
	`\bassert\w*\s*\(`,              // assert(, assertEqual(, assert_eq!(
	`\bassert\w*\.\w+\s*\(`,         // assert.equal(, assert_that.is(
	`\.assert\w*\s*\(`,              // self.assertEqual(
	`\brequire\.\w+\s*\(`,           // require.NoError(
	`\bexpect\s*\(`,                 // expect(
	`\.should\b|\bshould\s*\(`,      // x.should.eq, should(
	`\bt\.(Error|Fatal)f?\s*\(`,     // t.Fatalf(
	`\b(EXPECT|ASSERT)_[A-Z]+\s*\(`, // EXPECT_EQ(
	`\bXCTAssert\w*\s*\(`,           // XCTAssertEqual(
}, "|"))

// skipMarkers are the ways to stop a test running without deleting it.
var skipMarkers = regexp.MustCompile(strings.Join([]string{
	`\b(it|test|describe|context|suite)\.(skip|only|todo|failing)\b`,
	`\bx(it|describe|test|context)\s*\(`,
	`@pytest\.mark\.(skip|xfail)`,
	`\b(pytest|unittest)\.skip`,
	`\bt\.Skip(Now)?\s*\(`,
	`#\[ignore\]`,
	`@(Ignore|Disabled)\b`,
	`\.only\s*\(`,
	`\bfit\s*\(|\bfdescribe\s*\(`,
}, "|"))

// testPath is how a file says it holds tests. Every entry is a convention a
// test runner itself keys off, not a guess about naming taste.
var testPath = regexp.MustCompile(strings.Join([]string{
	`_test\.[A-Za-z0-9]+$`,
	`\.(test|spec)\.[A-Za-z0-9]+$`,
	`(^|/)test_[^/]*$`,
	`_spec\.[A-Za-z0-9]+$`,
	`(^|/)(tests?|specs?|__tests__)/`,
	`(^|/)[A-Z][A-Za-z0-9]*Test\.[A-Za-z0-9]+$`,
}, "|"))

// count is how many matches a pattern has across some text. Only the number
// leaves this function; the matches themselves are discarded here, which is
// the single place the promise is kept.
func count(re *regexp.Regexp, text string) int {
	if text == "" {
		return 0
	}
	return len(re.FindAllString(text, -1))
}

func lines(text string) int {
	if text == "" {
		return 0
	}
	return len(strings.Split(strings.TrimRight(text, "\n"), "\n"))
}

// fromEdit reads one before/after pair.
func fromEdit(path, removed, added string) Signals {
	return Signals{
		TestFile:          path != "" && testPath.MatchString(path),
		AssertionsAdded:   count(assertions, added),
		AssertionsRemoved: count(assertions, removed),
		SkipMarkersAdded:  count(skipMarkers, added),
		LinesAdded:        lines(added),
		LinesRemoved:      lines(removed),
	}
}

// FromPatch reads a unified diff, which is how Codex and every `apply_patch`
// style tool present an edit.
func FromPatch(path, patch string) Signals {
	var added, removed strings.Builder
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			// File headers are not content.
		case strings.HasPrefix(line, "+"):
			added.WriteString(line[1:] + "\n")
		case strings.HasPrefix(line, "-"):
			removed.WriteString(line[1:] + "\n")
		}
	}
	return fromEdit(path, removed.String(), added.String())
}
