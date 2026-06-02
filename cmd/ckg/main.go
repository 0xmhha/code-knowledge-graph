package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// audit subcommand surfaces structured exit codes (1=diff, 2=error)
		// via auditExitCode. Its RunE prints any human-readable diagnostic
		// to stderr before returning, so we don't repeat it here.
		var ae auditExitCode
		if errors.As(err, &ae) {
			os.Exit(int(ae))
		}
		var ve validateExitCode
		if errors.As(err, &ve) {
			os.Exit(int(ve))
		}
		_, _ = fmt.Fprintln(os.Stderr, "ckg:", err)
		os.Exit(1)
	}
}
