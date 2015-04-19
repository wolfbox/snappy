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
	"path/filepath"
	"strings"
)

// A SnapDataDir represents a single data directory for a version of a package
type SnapDataDir struct {
	base      string
	name      string
	namespace string
	version   string
}

func data1(spec, basedir string) []SnapDataDir {
	var snaps []SnapDataDir
	var specns, specver string
	var filterns bool

	dirs, _ := filepath.Glob(filepath.Join(basedir, "*", "*"))

	idx := strings.IndexRune(spec, '=')
	if idx > -1 {
		specver = spec[idx+1:]
		spec = spec[:idx]
	}
	idx = strings.IndexRune(spec, '.')
	if idx > -1 {
		filterns = true
		specns = spec[idx+1:]
		spec = spec[:idx]
	}

	for _, dir := range dirs {
		version := filepath.Base(dir)
		if specver != "" && version != specver {
			continue
		}
		name := filepath.Base(filepath.Dir(dir))
		namespace := ""
		idx := strings.IndexRune(name, '.')
		if idx > -1 {
			namespace = name[idx+1:]
			name = name[:idx]
		}
		if filterns && specns != namespace {
			continue
		}
		if spec != "" && spec != name {
			continue
		}

		snaps = append(snaps, SnapDataDir{
			base:      basedir,
			name:      name,
			namespace: namespace,
			version:   version,
		})
	}

	return snaps
}

// DataDirs returns the list of all SnapDataDirs in the system.
func DataDirs(spec string) []SnapDataDir {
	return append(data1(spec, snapDataHomeGlob), data1(spec, snapDataDir)...)
}
