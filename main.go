package main

import (
	"errors"
	"os"

	"github.com/crvgilbertson/intentra/cmd"
	"github.com/crvgilbertson/intentra/engine"
)

func main() {
	if err := cmd.Execute(); err != nil {
		var ve *engine.ValidationError
		var ge *engine.GitError
		var re *engine.ReasoningError
		switch {
		case errors.As(err, &ve):
			os.Exit(2)
		case errors.As(err, &ge):
			os.Exit(3)
		case errors.As(err, &re):
			os.Exit(4)
		default:
			os.Exit(1)
		}
	}
}
