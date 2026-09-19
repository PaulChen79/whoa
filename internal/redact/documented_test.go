package redact

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The privacy promise is only worth as much as a reader's ability to check it
// in one sitting. That means the README's list of Signals has to be the whole
// list, and it cannot be kept true by remembering to update it.
func TestEverySignalIsInTheREADME(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(readme)

	fields := reflect.TypeOf(Signals{})
	for i := 0; i < fields.NumField(); i++ {
		f := fields.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		if !strings.Contains(text, "`"+name+"`") {
			t.Errorf("Signal %q is extracted but not documented: a reader cannot see the whole of what leaves their machine", name)
		}
		// A Signal that is not a number or a boolean could carry content.
		switch f.Type.Kind() {
		case reflect.Bool, reflect.Int:
		default:
			t.Errorf("Signal %q is a %s: only counts and flags may leave the machine", name, f.Type)
		}
	}
}

// The redacted placeholder is part of the promise: someone reading a log must
// be able to tell "whoa removed something here" from "there was nothing here".
func TestRedactionIsVisibleInTheLog(t *testing.T) {
	got := Command("deploy --token ghp_0123456789abcdefghij")
	if !strings.Contains(got.Text, placeholder) {
		t.Errorf("a redacted command does not show that anything was removed: %q", got.Text)
	}
	out, _ := json.Marshal(got)
	if strings.Contains(string(out), "ghp_0123456789abcdefghij") {
		t.Fatalf("the secret survived: %s", out)
	}
}
