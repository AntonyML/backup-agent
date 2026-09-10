package main

import (
	"os"
	"testing"

	"femucaribe-backup-agent/internal/cli"
)

func TestExecute_Help(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"backup-agent", "--help"}
	code := cli.Execute()
	if code != cli.ExitOK {
		t.Fatalf("esperaba ExitOK (0) para --help, dio %d", code)
	}
}
