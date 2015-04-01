/*
 * Copyright (C) 2014-2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package systemctl

import (
	"fmt"
	"os/exec"
	"regexp"
	"time"

	"launchpad.net/snappy/helpers"
)

var (
	// RootDir is the path to the root directory, used for systemctl's Enable/Disable commands
	RootDir = "/"
	// the output of "show" must match this for Stop to be done:
	stopDoneRx = regexp.MustCompile(`(?m)\AActiveState=(?:failed|inactive)$`)
	// how many times should Stop check show's output
	stopSteps = 4 * 30
	// how much time should Stop wait between calls to show
	stopDelay = 250 * time.Millisecond
)

// run calls systemctl with the given args, returning its standard output (and wrapped error)
func run(args ...string) ([]byte, error) {
	bs, err := exec.Command("systemctl", args...).Output()
	if err != nil {
		exitCode, _ := helpers.ExitCode(err)
		return nil, &Error{cmd: args, exitCode: exitCode}
	}

	return bs, nil
}

// Systemctl is called from the commands to actually call out to
// systemctl. It's exported so it can be overridden by testing.
var Systemctl = run

// DaemonReload reloads systemd's configuration.
func DaemonReload() error {
	_, err := Systemctl("daemon-reload")
	return err
}

// Enable the given service
func Enable(serviceName string) error {
	_, err := Systemctl("--root", RootDir, "enable", serviceName)
	return err
}

// Disable the given service
func Disable(serviceName string) error {
	_, err := Systemctl("--root", RootDir, "disable", serviceName)
	return err
}

// Start the given service
func Start(serviceName string) error {
	_, err := Systemctl("start", serviceName)
	return err
}

// Stop the given service, and wait until it has stopped.
func Stop(serviceName string) error {
	if _, err := Systemctl("stop", serviceName); err != nil {
		return err
	}

	// and now wait for it to actually stop
	stopped := false
	for i := 0; i < stopSteps; i++ {
		bs, err := Systemctl("show", "--property=ActiveState", serviceName)
		if err != nil {
			return err
		}
		if stopDoneRx.Match(bs) {
			stopped = true
			break
		}
		time.Sleep(stopDelay)
	}

	if !stopped {
		return &Timeout{action: "stop", service: serviceName}
	}

	return nil
}

// Error is returned if the systemctl command failed
type Error struct {
	cmd      []string
	exitCode int
}

func (e *Error) Error() string {
	return fmt.Sprintf("%v failed with exit status %d", e.cmd, e.exitCode)
}

// Timeout is returned if the systemctl action failed to reach the
// expected state in a reasonable amount of time
type Timeout struct {
	action  string
	service string
}

func (e *Timeout) Error() string {
	return fmt.Sprintf("%v failed to %v: timeout", e.service, e.action)
}
