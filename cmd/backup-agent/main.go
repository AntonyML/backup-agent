package main

import (
	"os"

	"femucaribe-backup-agent/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
