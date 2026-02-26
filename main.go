package main

import (
	"os"

	"github.com/crvgilbertson/intentra/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
