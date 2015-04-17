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

package snappy

import (
	"os"

	"launchpad.net/snappy/helpers"
)

const (
	// the postfix we append to the release that is sent to the store
	// FIXME: find a better way to detect the postfix
	releasePostfix = "-core"
)

var LsbRelease string

func init() {
	// init the global directories at startup
	root := os.Getenv("SNAPPY_GLOBAL_ROOT")
	if root == "" {
		root = "/"
	}

	SetRootDir(root)

	SetRelease(helpers.LsbRelease(globalRootDir))
}

func SetRelease(release string) {
	LsbRelease = release + releasePostfix
}
