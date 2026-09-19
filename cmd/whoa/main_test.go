package main

import (
	"os"
	"testing"
)

func TestUnknownCommandIsAnError(t *testing.T) {
	if err := run([]string{"nope"}, os.Stdin, os.Stdout, os.Stderr); err == nil {
		t.Fatal("expected an error for an unknown command")
	}
}
