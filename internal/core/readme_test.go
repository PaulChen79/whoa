package core

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// docs/adr/0004 says every Parameter must be listed in full in the README.
// Left to eyes alone that promise decays the first time someone adds a field,
// so it is checked mechanically: the struct is the list, the README is the
// documentation, and this test fails when they disagree.
func TestEveryParameterIsDocumentedInTheREADME(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(readme)

	for _, key := range parameterKeys(reflect.TypeOf(Config{}), "") {
		if !strings.Contains(text, "`"+key+"`") {
			t.Errorf("Parameter %q exists in core.Config but is not documented in README.md", key)
		}
	}
}

// parameterKeys is every JSON key a user could write in the config file,
// including the ones nested inside Trigger, in dotted form.
func parameterKeys(t reflect.Type, prefix string) []string {
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		name = prefix + name
		if field.Type.Kind() == reflect.Struct {
			keys = append(keys, parameterKeys(field.Type, name+".")...)
			continue
		}
		keys = append(keys, name)
	}
	return keys
}
