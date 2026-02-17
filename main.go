package main

import (
	"os"

	"github.com/devaloi/ask/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
