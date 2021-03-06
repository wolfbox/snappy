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

package partition

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"launchpad.net/snappy/helpers"

	"github.com/mvo5/goconfigparser"
)

const (
	bootloaderUbootDirReal        = "/boot/uboot"
	bootloaderUbootConfigFileReal = "/boot/uboot/uEnv.txt"

	// File created by u-boot itself when
	// bootloaderBootmodeTry == "try" which the
	// successfully booted system must remove to flag to u-boot that
	// this partition is "good".
	bootloaderUbootStampFileReal = "/boot/uboot/snappy-stamp.txt"

	// the main uEnv.txt u-boot config file sources this snappy
	// boot-specific config file.
	bootloaderUbootEnvFileReal = "/boot/uboot/snappy-system.txt"
)

// var to make it testable
var (
	bootloaderUbootDir        = bootloaderUbootDirReal
	bootloaderUbootConfigFile = bootloaderUbootConfigFileReal
	bootloaderUbootStampFile  = bootloaderUbootStampFileReal
	bootloaderUbootEnvFile    = bootloaderUbootEnvFileReal
)

const bootloaderNameUboot bootloaderName = "u-boot"

type uboot struct {
	*bootloaderType

	// full path to rootfs-specific assets on boot partition
	currentBootPath string
	otherBootPath   string
}

// Stores a Name and a Value to be added as a name=value pair in a file.
type configFileChange struct {
	Name  string
	Value string
}

// newUboot create a new Grub bootloader object
func newUboot(partition *Partition) bootLoader {
	if !helpers.FileExists(bootloaderUbootConfigFile) {
		return nil
	}

	b := newBootLoader(partition)
	if b == nil {
		return nil
	}
	u := uboot{bootloaderType: b}
	u.currentBootPath = path.Join(bootloaderUbootDir, u.currentRootfs)
	u.otherBootPath = path.Join(bootloaderUbootDir, u.otherRootfs)

	return &u
}

func (u *uboot) Name() bootloaderName {
	return bootloaderNameUboot
}

// ToggleRootFS make the U-Boot bootloader switch rootfs's.
//
// Approach:
//
// - Assume the device's installed version of u-boot supports
//   CONFIG_SUPPORT_RAW_INITRD (that allows u-boot to boot a
//   standard initrd+kernel on the fat32 disk partition).
// - Copy the "other" rootfs's kernel+initrd to the boot partition,
//   renaming them in the process to ensure the next boot uses the
//   correct versions.
func (u *uboot) ToggleRootFS() (err error) {

	// If the file exists, update it. Otherwise create it.
	//
	// The file _should_ always exist, but since it's on a writable
	// partition, it's possible the admin removed it by mistake. So
	// recreate to allow the system to boot!
	changes := []configFileChange{
		configFileChange{Name: bootloaderRootfsVar,
			Value: string(u.otherRootfs),
		},
		configFileChange{Name: bootloaderBootmodeVar,
			Value: bootloaderBootmodeTry,
		},
	}

	return modifyNameValueFile(bootloaderUbootEnvFile, changes)
}

func (u *uboot) GetBootVar(name string) (value string, err error) {
	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	if err := cfg.ReadFile(bootloaderUbootEnvFile); err != nil {
		return "", nil
	}

	return cfg.Get("", name)
}

func (u *uboot) GetNextBootRootFSName() (label string, err error) {
	value, err := u.GetBootVar(bootloaderRootfsVar)
	if err != nil {
		// should never happen
		return "", err
	}

	return value, nil
}

func (u *uboot) GetRootFSName() string {
	return u.currentRootfs
}

func (u *uboot) GetOtherRootFSName() string {
	return u.otherRootfs
}

// FIXME: put into utils package
func readLines(path string) (lines []string, err error) {

	file, err := os.Open(path)

	if err != nil {
		return nil, err
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return lines, scanner.Err()
}

// FIXME: put into utils package
func writeLines(lines []string, path string) (err error) {

	file, err := os.Create(path)

	if err != nil {
		return err
	}

	defer file.Close()

	writer := bufio.NewWriter(file)

	for _, line := range lines {
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func (u *uboot) MarkCurrentBootSuccessful() (err error) {
	changes := []configFileChange{
		configFileChange{Name: bootloaderBootmodeVar,
			Value: bootloaderBootmodeSuccess,
		},
	}

	if err := modifyNameValueFile(bootloaderUbootEnvFile, changes); err != nil {
		return err
	}

	return os.RemoveAll(bootloaderUbootStampFile)
}

func (u *uboot) SyncBootFiles() (err error) {
	srcDir := u.currentBootPath
	destDir := u.otherBootPath

	// always start from scratch: all files here are owned by us.
	os.RemoveAll(destDir)

	return runCommand("/bin/cp", "-a", srcDir, destDir)
}

func (u *uboot) HandleAssets() (err error) {
	// check if we have anything, if there is no hardware yaml, there is nothing
	// to process.
	hardware, err := u.partition.hardwareSpec()
	if err == ErrNoHardwareYaml {
		return nil
	} else if err != nil {
		return err
	}
	// ensure to remove the file once we are done
	defer os.Remove(u.partition.hardwareSpecFile)

	// validate bootloader
	if hardware.Bootloader != u.Name() {
		return fmt.Errorf(
			"bootloader is of type %s but hardware spec requires %s",
			u.Name(),
			hardware.Bootloader)
	}

	// validate partition layout
	if u.partition.dualRootPartitions() && hardware.PartitionLayout != bootloaderSystemAB {
		return fmt.Errorf("hardware spec requires dual root partitions")
	}

	// ensure we have the destdir
	destDir := u.otherBootPath
	if err := os.MkdirAll(destDir, dirMode); err != nil {
		return err
	}

	// install kernel+initrd
	for _, file := range []string{hardware.Kernel, hardware.Initrd} {

		if file == "" {
			continue
		}

		// expand path
		path := path.Join(u.partition.cacheDir(), file)

		if !helpers.FileExists(path) {
			return fmt.Errorf("can not find file %s", path)
		}

		// ensure we remove the dir later
		defer os.RemoveAll(filepath.Dir(path))

		if err := runCommand("/bin/cp", path, destDir); err != nil {
			return err
		}
	}

	// TODO: look at the OEM package for dtb changes too once that is
	//       fully speced

	// install .dtb files
	dtbSrcDir := filepath.Join(u.partition.cacheDir(), hardware.DtbDir)
	if helpers.FileExists(dtbSrcDir) {
		// ensure we cleanup the source dir
		defer os.RemoveAll(dtbSrcDir)

		dtbDestDir := path.Join(destDir, "dtbs")
		if err := os.MkdirAll(dtbDestDir, dirMode); err != nil {
			return err
		}

		files, err := filepath.Glob(path.Join(dtbSrcDir, "*"))
		if err != nil {
			return err
		}

		for _, file := range files {
			if err := runCommand("/bin/cp", file, dtbDestDir); err != nil {
				return err
			}
		}
	}

	flashAssetsDir := u.partition.flashAssetsDir()

	if helpers.FileExists(flashAssetsDir) {
		// FIXME: we don't currently do anything with the
		// MLO + uImage files since they are not specified in
		// the hardware spec. So for now, just remove them.

		if err := os.RemoveAll(flashAssetsDir); err != nil {
			return err
		}
	}

	return err
}

// Write lines to file atomically. File does not have to preexist.
// FIXME: put into utils package
func atomicFileUpdate(file string, lines []string) (err error) {
	tmpFile := fmt.Sprintf("%s.NEW", file)

	if err := writeLines(lines, tmpFile); err != nil {
		return err
	}

	// atomic update
	if err := os.Rename(tmpFile, file); err != nil {
		return err
	}

	return nil
}

// Rewrite the specified file, applying the specified set of changes.
// Lines not in the changes slice are left alone.
// If the original file does not contain any of the name entries (from
// the corresponding configFileChange objects), those entries are
// appended to the file.
//
// FIXME: put into utils package
func modifyNameValueFile(file string, changes []configFileChange) (err error) {
	var updated []configFileChange

	lines, err := readLines(file)
	if err != nil {
		return err
	}

	var new []string

	for _, line := range lines {
		for _, change := range changes {
			if strings.HasPrefix(line, fmt.Sprintf("%s=", change.Name)) {
				line = fmt.Sprintf("%s=%s", change.Name, change.Value)
				updated = append(updated, change)
			}
		}
		new = append(new, line)
	}

	lines = new

	for _, change := range changes {
		got := false
		for _, update := range updated {
			if update.Name == change.Name {
				got = true
				break
			}
		}

		if !got {
			// name/value pair did not exist in original
			// file, so append
			lines = append(lines, fmt.Sprintf("%s=%s",
				change.Name, change.Value))
		}
	}

	return atomicFileUpdate(file, lines)
}

func (u *uboot) AdditionalBindMounts() []string {
	// nothing additional to system-boot required on uboot
	return []string{}
}
