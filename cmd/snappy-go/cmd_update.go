package main

import (
	"fmt"
	"os"

	"launchpad.net/snappy/snappy"
	"launchpad.net/snappy/helpers"
)

type cmdUpdate struct {
}

func init() {
	var cmdUpdateData cmdUpdate
	cmd, _ := parser.AddCommand("update",
		"Update all installed parts",
		"Ensures system is running with latest parts",
		&cmdUpdateData)

	cmd.Aliases = append(cmd.Aliases, "up")
}

func (x *cmdUpdate) Execute(args []string) (err error) {
	if err := helpers.StartPrivileged(); err != nil {
		return err
	}

	if err = update(); err != nil {
		return err
	}

	return helpers.StopPrivileged()
}

func update() error {
	// FIXME: handle args
	updates, err := snappy.ListUpdates()
	if err != nil {
		return err
	}

	for _, part := range updates {
		pbar := snappy.NewTextProgress(part.Name())

		fmt.Printf("Installing %s (%s)\n", part.Name(), part.Version())
		if err := part.Install(pbar); err != nil {
			return err
		}
	}

	if len(updates) > 0 {
		showVerboseList(updates, os.Stdout)
	}

	return nil
}
