package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	command := newCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"version"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != version {
		t.Fatalf("version output=%q", output.String())
	}
}

func TestPlainProcessRequiresSource(t *testing.T) {
	command := newCommand()
	command.SetArgs([]string{"process", "--no-tui"})
	if err := command.Execute(); err == nil {
		t.Fatal("missing source accepted")
	}
}
