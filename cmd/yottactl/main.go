// Command yottactl is the structured CLI for the Yotta control
// plane — kubectl/awscli-shaped resource operations. Plan #60
// Track B. The conversational `yotta` terminal is a separate binary
// (later phase) that talks to Albus, which shells out to this.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/YottaBot-Platform/yottactl/internal/command"
)

func main() {
	root := command.NewRoot()
	if err := root.Execute(); err != nil {
		// Render the message; translate an ExitError into its code so
		// the background agent reads a meaningful exit status.
		fmt.Fprintln(os.Stderr, "error:", err.Error())
		var ee *command.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.Code)
		}
		os.Exit(1)
	}
}
