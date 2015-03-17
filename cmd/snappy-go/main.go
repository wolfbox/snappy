package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"launchpad.net/snappy/logger"

	"github.com/jessevdk/go-flags"
)

// fixed errors the command can return
var ErrRequiresRoot = errors.New("command requires sudo (root)")

type options struct {
	// No global options yet
}

var optionsData options

var parser = flags.NewParser(&optionsData, flags.Default)

func init() {
	err := logger.ActivateLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to activate logging: %s\n", err)
	}
}

func main() {
	if _, err := parser.Parse(); err != nil {
		os.Exit(1)
	}
}

func isRoot() bool {
	return syscall.Getuid() == 0
}
