package main

import (
	"fmt"
	"os"

	"bubblewrap-runner/internal/runner"
)

func main() {
	code, err := runner.Main(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "bwrun:", err)
	}
	os.Exit(code)
}
