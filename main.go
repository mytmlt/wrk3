package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/mytmlt/wrk3/cmd"
	"github.com/mytmlt/wrk3/internal/telemetry"
)

func main() {
	telemetry.Version = cmd.Version
	telemetry.Commit = cmd.Commit
	telemetry.Date = cmd.Date
	telemetry.Init()
	defer telemetry.Flush()

	defer func() {
		if r := recover(); r != nil {
			err, ok := r.(error)
			if !ok {
				err = fmt.Errorf("panic: %v", r)
			}
			telemetry.ReportIfEnabled("panic", err)
			_, _ = fmt.Fprintf(os.Stderr, "panic: %v\n%s\n", r, debug.Stack())
			os.Exit(1)
		}
	}()

	if err := cmd.Execute(); err != nil {
		telemetry.ReportIfEnabled(cmd.NameOfLastCommand(), err)
		telemetry.Flush()
		os.Exit(1)
	}
}