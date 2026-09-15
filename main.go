package main

import (
	"os"

	"github.com/mytmlt/wrk3/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
