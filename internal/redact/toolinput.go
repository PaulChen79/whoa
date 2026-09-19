package redact

import (
	"encoding/json"
	"strings"
)

// Extract is everything whoa keeps from one tool's arguments.
type Extract struct {
	Command Redacted `json:"command,omitempty"`
	Signals Signals  `json:"signals,omitempty"`
}

// Empty reports whether the tool's arguments yielded nothing worth carrying.
func (e Extract) Empty() bool { return e.Command == Redacted{} && e.Signals.Empty() }

// The keys whoa understands. Everything else in a payload is walked and
// discarded, which is why this list can be short without being a gap.
var (
	commandKeys = map[string]bool{"command": true, "cmd": true, "script": true}
	removedKeys = map[string]bool{"old_string": true, "old_str": true, "oldtext": true, "old": true}
	addedKeys   = map[string]bool{"new_string": true, "new_str": true, "newtext": true, "new": true, "content": true, "contents": true, "file_text": true}
	patchKeys   = map[string]bool{"patch": true, "diff": true, "unified_diff": true}
	pathKeys    = map[string]bool{"file_path": true, "filepath": true, "path": true, "notebook_path": true, "target_file": true}
)

// FromToolInput reads a tool's arguments and returns the only things whoa is
// allowed to keep from them.
//
// It walks the whole structure rather than reading the fields of any
// particular tool. That is deliberate. Claude Code carries arguments under
// `tool_input`, subagent calls nest another payload inside, Codex passes a
// `command` array and an `apply_patch` body, and both grow fields between
// releases. Enumerating the known shapes would make the privacy promise true
// only of the shapes someone thought to enumerate; walking every string and
// keeping nothing by default makes it true of shapes that do not exist yet.
//
// Values under unrecognised keys, including tool output such as stdout and
// stderr, are visited and dropped. Nothing reaches the result except a
// redacted command or a counted Signal.
func FromToolInput(raw json.RawMessage) Extract {
	var node any
	if len(raw) == 0 || json.Unmarshal(raw, &node) != nil {
		return Extract{}
	}
	var out Extract
	walk(node, "", &out)
	return out
}

func walk(node any, path string, out *Extract) {
	switch n := node.(type) {
	case []any:
		for _, child := range n {
			walk(child, path, out)
		}
	case map[string]any:
		walkObject(n, path, out)
	}
}

// walkObject reads one object, pairing any before/after text it holds with the
// nearest enclosing file path so that a Signal knows whether it is looking at
// a test.
func walkObject(obj map[string]any, path string, out *Extract) {
	for key, value := range obj {
		if pathKeys[strings.ToLower(key)] {
			if s, ok := value.(string); ok && s != "" {
				path = s
			}
		}
	}

	var removed, added strings.Builder
	for key, value := range obj {
		k := strings.ToLower(key)
		switch {
		case commandKeys[k]:
			if text := commandText(value); text != "" {
				out.Command = Command(text)
			}
		case removedKeys[k]:
			appendString(&removed, value)
		case addedKeys[k]:
			appendString(&added, value)
		case patchKeys[k]:
			if s, ok := value.(string); ok {
				out.Signals = out.Signals.merge(FromPatch(path, s))
			}
		default:
			walk(value, path, out)
		}
	}

	if removed.Len() > 0 || added.Len() > 0 {
		out.Signals = out.Signals.merge(fromEdit(path, removed.String(), added.String()))
	} else if path != "" && out.Signals.Empty() {
		out.Signals.TestFile = testPath.MatchString(path)
	}
}

// commandText handles both shapes a command arrives in: Claude Code sends one
// string, Codex sends the argv array.
func commandText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func appendString(b *strings.Builder, value any) {
	if s, ok := value.(string); ok && s != "" {
		b.WriteString(s)
		b.WriteString("\n")
	}
}
