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
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"launchpad.net/snappy/progress"
)

// SnapType represents the kind of snap (app, core, frameworks, oem)
type SnapType string

// SystemConfig is a config map holding configs for multiple packages
type SystemConfig map[string]interface{}

// The various types of snap parts we support
const (
	SnapTypeApp       SnapType = "app"
	SnapTypeCore      SnapType = "core"
	SnapTypeFramework SnapType = "framework"
	SnapTypeOem       SnapType = "oem"
)

// Services implements snappy packages that offer services
type Services interface {
	Services() []Service
}

// Configuration allows requesting an oem snappy package type's config
type Configuration interface {
	OemConfig() SystemConfig
}

// Dirname of a Part is the Name, in most cases qualified with the
// Namespace
func Dirname(p Part) string {
	if t := p.Type(); t == SnapTypeFramework || t == SnapTypeOem {
		return p.Name()
	}
	return p.Name() + "." + p.Namespace()
}

// Part representation of a snappy part
type Part interface {

	// query
	Name() string
	Version() string
	Description() string
	Namespace() string

	Hash() string
	IsActive() bool
	IsInstalled() bool
	// Will become active on the next reboot
	NeedsReboot() bool

	// returns the date when the snap was last updated
	Date() time.Time

	// returns the channel of the part
	Channel() string

	// returns the path to the icon (local or uri)
	Icon() string

	// Returns app, framework, core
	Type() SnapType

	InstalledSize() int64
	DownloadSize() int64

	// Install the snap
	Install(pb progress.Meter, flags InstallFlags) (name string, err error)
	// Uninstall the snap
	Uninstall(pb progress.Meter) error
	// Config takes a yaml configuration and returns the full snap
	// config with the changes. Note that "configuration" may be empty.
	Config(configuration []byte) (newConfig string, err error)
	// make a inactive part active
	SetActive(pb progress.Meter) error

	// get the list of frameworks needed by the part
	Frameworks() ([]string, error)
}

// Repository is the interface for a collection of snaps
type Repository interface {

	// query
	Description() string

	// action
	Details(snappName string) ([]Part, error)

	Updates() ([]Part, error)
	Installed() ([]Part, error)
}

// MetaRepository contains all available single repositories can can be used
// to query in a single place
type MetaRepository struct {
	all []Repository
}

// NewMetaStoreRepository returns a MetaRepository of stores
func NewMetaStoreRepository() *MetaRepository {
	m := new(MetaRepository)
	m.all = []Repository{}

	if repo := NewUbuntuStoreSnapRepository(); repo != nil {
		m.all = append(m.all, repo)
	}

	return m
}

// NewMetaLocalRepository returns a MetaRepository of stores
func NewMetaLocalRepository() *MetaRepository {
	m := new(MetaRepository)
	m.all = []Repository{}

	if repo := NewSystemImageRepository(); repo != nil {
		m.all = append(m.all, repo)
	}
	if repo := NewLocalSnapRepository(snapAppsDir); repo != nil {
		m.all = append(m.all, repo)
	}
	if repo := NewLocalSnapRepository(snapOemDir); repo != nil {
		m.all = append(m.all, repo)
	}

	return m
}

// NewMetaRepository returns a new MetaRepository
func NewMetaRepository() *MetaRepository {
	// FIXME: make this a configuration file

	m := new(MetaRepository)
	m.all = []Repository{}
	// its ok if repos fail if e.g. no dbus is available
	if repo := NewSystemImageRepository(); repo != nil {
		m.all = append(m.all, repo)
	}
	if repo := NewUbuntuStoreSnapRepository(); repo != nil {
		m.all = append(m.all, repo)
	}
	if repo := NewLocalSnapRepository(snapAppsDir); repo != nil {
		m.all = append(m.all, repo)
	}
	if repo := NewLocalSnapRepository(snapOemDir); repo != nil {
		m.all = append(m.all, repo)
	}

	return m
}

// Installed returns all installed parts
func (m *MetaRepository) Installed() (parts []Part, err error) {
	for _, r := range m.all {
		installed, err := r.Installed()
		if err != nil {
			return parts, err
		}
		parts = append(parts, installed...)
	}

	return parts, err
}

// Updates returns all updatable parts
func (m *MetaRepository) Updates() (parts []Part, err error) {
	for _, r := range m.all {
		updates, err := r.Updates()
		if err != nil {
			return parts, err
		}
		parts = append(parts, updates...)
	}

	return parts, err
}

// Details returns details for the given snap name
func (m *MetaRepository) Details(snapyName string) (parts []Part, err error) {
	for _, r := range m.all {
		results, err := r.Details(snapyName)
		// ignore network errors here, we will also collect
		// local results
		_, netError := err.(net.Error)
		_, urlError := err.(*url.Error)
		switch {
		case err == ErrPackageNotFound || netError || urlError:
			continue
		case err != nil:
			return parts, err
		}
		parts = append(parts, results...)
	}

	return parts, err
}

// ActiveSnapsByType returns all installed snaps with the given type
func ActiveSnapsByType(snapTs ...SnapType) (res []Part, err error) {
	m := NewMetaRepository()
	installed, err := m.Installed()
	if err != nil {
		return nil, err
	}

	for _, part := range installed {
		if !part.IsActive() {
			continue
		}
		for i := range snapTs {
			if part.Type() == snapTs[i] {
				res = append(res, part)
			}
		}
	}

	return res, nil
}

// ActiveSnapNamesByType returns all installed snap names with the given type
var ActiveSnapNamesByType = activeSnapNamesByTypeImpl

func activeSnapNamesByTypeImpl(snapTs ...SnapType) (res []string, err error) {
	installed, err := ActiveSnapsByType(snapTs...)
	for _, part := range installed {
		res = append(res, part.Name())
	}

	return res, nil
}

// ActiveSnapByName returns all active snaps with the given name
func ActiveSnapByName(needle string) Part {
	m := NewMetaRepository()
	installed, err := m.Installed()
	if err != nil {
		return nil
	}
	for _, part := range installed {
		if !part.IsActive() {
			continue
		}
		if part.Name() == needle {
			return part
		}
	}

	return nil
}

// FindSnapsByName returns all snaps with the given name in the "haystack"
// slice of parts (useful for filtering)
func FindSnapsByName(needle string, haystack []Part) (res []Part) {
	name, namespace := splitNamespace(needle)
	ignorens := namespace == ""

	for _, part := range haystack {
		if part.Name() == name && (ignorens || part.Namespace() == namespace) {
			res = append(res, part)
		}
	}

	return res
}

func splitNamespace(name string) (string, string) {
	idx := strings.LastIndexAny(name, ".")
	if idx > -1 {
		return name[:idx], name[idx+1:]
	}

	return name, ""
}

// FindSnapsByNameAndVersion returns the parts with the name/version in the
// given slice of parts
func FindSnapsByNameAndVersion(needle, version string, haystack []Part) []Part {
	name, namespace := splitNamespace(needle)
	ignorens := namespace == ""
	var found []Part

	for _, part := range haystack {
		if part.Name() == name && part.Version() == version && (ignorens || part.Namespace() == namespace) {
			found = append(found, part)
		}
	}

	return found
}

// MakeSnapActiveByNameAndVersion makes the given snap version the active
// version
func makeSnapActiveByNameAndVersion(pkg, ver string) error {
	m := NewMetaRepository()
	installed, err := m.Installed()
	if err != nil {
		return err
	}

	parts := FindSnapsByNameAndVersion(pkg, ver, installed)
	switch len(parts) {
	case 0:
		return fmt.Errorf("Can not find %s with version %s", pkg, ver)
	case 1:
		return parts[0].SetActive(nil)
	default:
		return fmt.Errorf("More than one %s with version %s", pkg, ver)
	}
}

// PackageNameActive checks whether a fork of the given name is active in the system
func PackageNameActive(name string) bool {
	return ActiveSnapByName(name) != nil
}
