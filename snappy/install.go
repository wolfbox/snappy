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
	"io/ioutil"
	"os"
	"sort"
	"strings"

	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/progress"
)

// InstallFlags can be used to pass additional flags to the install of a
// snap
type InstallFlags uint

const (
	// AllowUnauthenticated allows to install a snap even if it can not be authenticated
	AllowUnauthenticated InstallFlags = 1 << iota
	// InhibitHooks will ensure that the hooks are not run
	InhibitHooks
	// DoInstallGC will ensure that garbage collection is done
	DoInstallGC
	// AllowOEM allows the installation of OEM packages, this does not affect updates.
	AllowOEM
)

// check if the image is in developer mode
// FIXME: this is a bit crude right now, but it seems like there is not more
//        meta-data to check right now
// TODO: add feature to ubuntu-device-flash to write better info file when
//       the image is in developer mode
func inDeveloperMode() bool {
	f, err := os.Open(cloudMetaDataFile)
	if err != nil {
		return false
	}
	defer f.Close()
	data, err := ioutil.ReadAll(f)
	if err != nil {
		return false
	}
	needle := "public-keys:\n"
	if strings.Contains(string(data), needle) {
		return true
	}
	return false
}

// Install the givens snap names provided via args. This can be local
// files or snaps that are queried from the store
func Install(name string, flags InstallFlags, meter progress.Meter) (string, error) {
	name, err := doInstall(name, flags, meter)
	if err != nil {
		return "", logger.LogError(err)
	}

	return name, logger.LogError(GarbageCollect(name, flags))
}

func doInstall(name string, flags InstallFlags, meter progress.Meter) (snapName string, err error) {
	defer func() {
		if err != nil {
			err = &ErrInstallFailed{snap: name, origErr: err}
		}
	}()

	// consume local parts
	if fi, err := os.Stat(name); err == nil && fi.Mode().IsRegular() {
		// we allow unauthenticated package when in developer
		// mode
		if inDeveloperMode() {
			flags |= AllowUnauthenticated
		}

		return installClick(name, flags, meter, sideloadedNamespace)
	}

	// check repos next
	mStore := NewMetaStoreRepository()
	installed, err := NewMetaLocalRepository().Installed()
	if err != nil {
		return "", err
	}

	found, err := mStore.Details(name)
	if err != nil {
		return "", err
	}

	for _, part := range found {
		cur := FindSnapsByNameAndVersion(Dirname(part), part.Version(), installed)
		if len(cur) != 0 {
			return "", ErrAlreadyInstalled
		}
		if PackageNameActive(part.Name()) {
			return "", ErrPackageNameAlreadyInstalled
		}

		// TODO block oem snaps here once the store supports package types

		return part.Install(meter, flags)
	}

	return "", ErrPackageNotFound
}

// GarbageCollect removes all versions two older than the current active
// version, as long as NeedsReboot() is false on all the versions found, and
// DoInstallGC is set.
func GarbageCollect(name string, flags InstallFlags) error {
	var parts BySnapVersion

	if (flags & DoInstallGC) == 0 {
		return nil
	}

	m := NewMetaRepository()
	installed, err := m.Installed()
	if err != nil {
		return err
	}

	parts = FindSnapsByName(name, installed)
	if len(parts) < 3 {
		// not enough things installed to do gc
		return nil
	}

	sort.Sort(parts)
	active := -1 // active is the index of the active part in parts (-1 if no active part)

	for i, part := range parts {
		if part.IsActive() {
			if active > -1 {
				return ErrGarbageCollectImpossible("more than one active (should not happen).")
			}
			active = i
		}
		if part.NeedsReboot() {
			return nil // don't do gc on parts that need reboot.
		}
	}

	if active < 1 {
		// how was this an install?
		return nil
	}

	for _, part := range parts[:active-1] {
		if err := part.Uninstall(progress.MakeProgressBar(part.Name())); err != nil {
			return ErrGarbageCollectImpossible(err.Error())
		}
	}

	return nil
}
