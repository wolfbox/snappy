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

/* This part of the code implements enough of the click file format
   to install a "snap" package
   Limitations:
   - no per-user registration
   - no user-level hooks
   - more(?)
*/

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"text/template"
	"time"

	"launchpad.net/snappy/clickdeb"
	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/policy"
	"launchpad.net/snappy/systemd"

	"github.com/mvo5/goconfigparser"
)

type clickAppHook map[string]string

type clickManifest struct {
	Name          string                  `json:"name"`
	Version       string                  `json:"version"`
	Architecture  []string                `json:"architecture,omitempty"`
	Type          SnapType                `json:"type,omitempty"`
	Framework     string                  `json:"framework,omitempty"`
	Description   string                  `json:"description,omitempty"`
	Icon          string                  `json:"icon,omitempty"`
	InstalledSize string                  `json:"installed-size,omitempty"`
	Maintainer    string                  `json:"maintainer,omitempty"`
	Title         string                  `json:"title,omitempty"`
	Hooks         map[string]clickAppHook `json:"hooks,omitempty"`
}

type clickHook struct {
	name    string
	exec    string
	user    string
	pattern string
}

const (
	// from debsig-verify-0.9/debsigs.h
	dsSuccess           = 0
	dsFailNosigs        = 10
	dsFailUnknownOrigin = 11
	dsFailNopolicies    = 12
	dsFailBadsig        = 13
	dsFailInternal      = 14
)

// ignore hooks of this type
var ignoreHooks = map[string]bool{
	"bin-path":       true,
	"snappy-systemd": true,
}

// wait this time between TERM and KILL
var killWait = 5 * time.Second

// servicesBinariesStringsWhitelist is the whitelist of legal chars
// in the "binaries" and "services" section of the package.yaml
const servicesBinariesStringsWhitelist = `^[A-Za-z0-9/. _#:-]*$`

// Execute the hook.Exec command
func execHook(execCmd string) (err error) {
	// the spec says this is passed to the shell
	cmd := exec.Command("sh", "-c", execCmd)
	if err = cmd.Run(); err != nil {
		if exitCode, err := helpers.ExitCode(err); err != nil {
			return &ErrHookFailed{cmd: execCmd,
				exitCode: exitCode}
		}
		return err
	}

	return nil
}

// This function checks if the given exitCode is "ok" when running with
// --allow-unauthenticated. We allow package with no signature or with
// a unknown policy or with no policies at all. We do not allow overriding
// bad signatures
func allowUnauthenticatedOkExitCode(exitCode int) bool {
	return (exitCode == dsFailNosigs ||
		exitCode == dsFailUnknownOrigin ||
		exitCode == dsFailNopolicies)
}

// Tiny wrapper around the debsig-verify commandline
func runDebsigVerifyImpl(clickFile string, allowUnauthenticated bool) (err error) {
	cmd := exec.Command("debsig-verify", clickFile)
	if err := cmd.Run(); err != nil {
		exitCode, err := helpers.ExitCode(err)
		if err == nil {
			if allowUnauthenticated && allowUnauthenticatedOkExitCode(exitCode) {
				log.Println("Signature check failed, but installing anyway as requested")
				return nil
			}
			return &ErrSignature{exitCode: exitCode}
		}
		// not a exit code error, something else, pass on
		return err
	}
	return nil
}

var runDebsigVerify = runDebsigVerifyImpl

func auditClick(snapFile string, allowUnauthenticated bool) (err error) {
	// FIXME: check what more we need to do here, click is also doing
	//        permission checks
	return runDebsigVerify(snapFile, allowUnauthenticated)
}

func readClickManifest(data []byte) (manifest clickManifest, err error) {
	r := bytes.NewReader(data)
	dec := json.NewDecoder(r)
	err = dec.Decode(&manifest)
	return manifest, err
}

func readClickHookFile(hookFile string) (hook clickHook, err error) {
	// FIXME: fugly, write deb822 style parser if we keep this
	// FIXME2: the hook file will go probably entirely and gets
	//         implemented natively in go so ok for now :)
	cfg := goconfigparser.New()
	content, err := ioutil.ReadFile(hookFile)
	if err != nil {
		fmt.Printf("WARNING: failed to read %s", hookFile)
		return hook, err
	}
	err = cfg.Read(strings.NewReader("[hook]\n" + string(content)))
	if err != nil {
		fmt.Printf("WARNING: failed to parse %s", hookFile)
		return hook, err
	}
	hook.name, _ = cfg.Get("hook", "Hook-Name")
	hook.exec, _ = cfg.Get("hook", "Exec")
	hook.user, _ = cfg.Get("hook", "User")
	hook.pattern, _ = cfg.Get("hook", "Pattern")
	// FIXME: error on supported hook features like
	//    User-Level: yes
	//    Trigger: yes
	//    Single-Version: yes

	// urgh, click allows empty "Hook-Name"
	if hook.name == "" {
		hook.name = strings.Split(filepath.Base(hookFile), ".")[0]
	}

	return hook, err
}

func systemClickHooks() (hooks map[string]clickHook, err error) {
	hooks = make(map[string]clickHook)

	hookFiles, err := filepath.Glob(path.Join(clickSystemHooksDir, "*.hook"))
	if err != nil {
		return nil, err
	}
	for _, f := range hookFiles {
		hook, err := readClickHookFile(f)
		if err != nil {
			log.Printf("Can't read hook file %s: %s", f, err)
			continue
		}
		hooks[hook.name] = hook
	}

	return hooks, err
}

func expandHookPattern(name, app, version, pattern string) (expanded string) {
	id := fmt.Sprintf("%s_%s_%s", name, app, version)
	// FIXME: support the other patterns (and see if they are used at all):
	//        - short-id
	//        - user (probably not!)
	//        - home (probably not!)
	//        - $$ (?)
	return strings.Replace(pattern, "${id}", id, -1)
}

type iterHooksFunc func(src, dst string, systemHook clickHook) error

// iterHooks will run the callback "f" for the given manifest
// so that the call back can arrange e.g. a new link
func iterHooks(manifest clickManifest, inhibitHooks bool, f iterHooksFunc) error {
	systemHooks, err := systemClickHooks()
	if err != nil {
		return err
	}

	for app, hook := range manifest.Hooks {
		for hookName, hookSourceFile := range hook {
			// ignore hooks that only exist for compatibility
			// with the old snappy-python (like bin-path,
			// snappy-systemd)
			if ignoreHooks[hookName] {
				continue
			}

			systemHook, ok := systemHooks[hookName]
			if !ok {
				log.Printf("WARNING: Skipping hook %s", hookName)
				continue
			}

			dst := filepath.Join(globalRootDir, expandHookPattern(manifest.Name, app, manifest.Version, systemHook.pattern))

			if _, err := os.Stat(dst); err == nil {
				if err := os.Remove(dst); err != nil {
					log.Printf("Warning: failed to remove %s: %s", dst, err)
				}
			}

			// run iter func here
			if err := f(hookSourceFile, dst, systemHook); err != nil {
				return err
			}

			if systemHook.exec != "" && !inhibitHooks {
				if err := execHook(systemHook.exec); err != nil {
					os.Remove(dst)
					return err
				}
			}
		}
	}

	return nil
}

func installClickHooks(targetDir string, manifest clickManifest, inhibitHooks bool) error {
	return iterHooks(manifest, inhibitHooks, func(src, dst string, systemHook clickHook) error {
		// setup the new link target here, iterHooks will take
		// care of running the hook
		realSrc := stripGlobalRootDir(path.Join(targetDir, src))
		if err := os.Symlink(realSrc, dst); err != nil {
			return err
		}

		return nil
	})
}

func removeClickHooks(manifest clickManifest, inhibitHooks bool) (err error) {
	return iterHooks(manifest, inhibitHooks, func(src, dst string, systemHook clickHook) error {
		// nothing we need to do here, the iterHookss will remove
		// the hook symlink and call the hook itself
		return nil
	})
}

func readClickManifestFromClickDir(clickDir string) (manifest clickManifest, err error) {
	manifestFiles, err := filepath.Glob(path.Join(clickDir, ".click", "info", "*.manifest"))
	if err != nil {
		return manifest, err
	}
	if len(manifestFiles) != 1 {
		return manifest, fmt.Errorf("Error: got %v manifests in %v", len(manifestFiles), clickDir)
	}
	manifestData, err := ioutil.ReadFile(manifestFiles[0])
	manifest, err = readClickManifest([]byte(manifestData))
	return manifest, err
}

func removeClick(clickDir string, inter interacter) (err error) {
	manifest, err := readClickManifestFromClickDir(clickDir)
	if err != nil {
		return err
	}

	if err := removeClickHooks(manifest, false); err != nil {
		return err
	}

	// maybe remove current symlink
	currentSymlink := path.Join(path.Dir(clickDir), "current")
	p, _ := filepath.EvalSymlinks(currentSymlink)
	if clickDir == p {
		if err := unsetActiveClick(p, false, inter); err != nil {
			return err
		}
	}

	err = os.RemoveAll(clickDir)
	if err != nil {
		return err
	}

	os.Remove(filepath.Dir(clickDir))

	return nil
}

func writeHashesFile(d *clickdeb.ClickDeb, instDir string) error {
	hashesFile := filepath.Join(instDir, "meta", "hashes.yaml")
	hashesData, err := d.ControlMember("hashes.yaml")
	if err != nil {
		return err
	}

	return ioutil.WriteFile(hashesFile, hashesData, 0644)
}

// generate the name
func generateBinaryName(m *packageYaml, binary Binary) string {
	var binName string
	if m.Type == SnapTypeFramework {
		binName = filepath.Base(binary.Name)
	} else {
		binName = fmt.Sprintf("%s.%s", m.Name, filepath.Base(binary.Name))
	}

	return filepath.Join(snapBinariesDir, binName)
}

func binPathForBinary(pkgPath string, binary Binary) string {
	return filepath.Join(pkgPath, binary.Exec)
}

func verifyBinariesYaml(binary Binary) error {
	return verifyStructStringsAgainstWhitelist(binary, servicesBinariesStringsWhitelist)
}

func generateSnapBinaryWrapper(binary Binary, pkgPath, aaProfile string, m *packageYaml) (string, error) {
	wrapperTemplate := `#!/bin/sh
# !!!never remove this line!!!
##TARGET={{.Target}}

set -e

TMPDIR="/tmp/snaps/{{.UdevAppName}}/{{.Version}}/tmp"
if [ ! -d "$TMPDIR" ]; then
    mkdir -p -m1777 "$TMPDIR"
fi
export TMPDIR
export TEMPDIR="$TMPDIR"

# app paths (deprecated)
export SNAPP_APP_PATH="{{.Path}}"
export SNAPP_APP_DATA_PATH="/var/lib/{{.Path}}"
export SNAPP_APP_USER_DATA_PATH="$HOME/{{.Path}}"
export SNAPP_APP_TMPDIR="$TMPDIR"
export SNAPP_OLD_PWD="$(pwd)"

# app info
export SNAP_NAME="{{.Name}}"
export SNAP_ORIGIN="{{.Namespace}}"
export SNAP_FULLNAME="{{.UdevAppName}}"

# app paths
export SNAP_APP_PATH="{{.Path}}"
export SNAP_APP_DATA_PATH="/var/lib/{{.Path}}"
export SNAP_APP_USER_DATA_PATH="$HOME/{{.Path}}"
export SNAP_APP_TMPDIR="$TMPDIR"

# FIXME: this will need to become snappy arch or something
export SNAPPY_APP_ARCH="$(dpkg --print-architecture)"

if [ ! -d "$SNAP_APP_USER_DATA_PATH" ]; then
   mkdir -p "$SNAP_APP_USER_DATA_PATH"
fi
export HOME="$SNAP_APP_USER_DATA_PATH"

# export old pwd
export SNAP_OLD_PWD="$(pwd)"
cd {{.Path}}
ubuntu-core-launcher {{.UdevAppName}} {{.AaProfile}} {{.Target}} "$@"
`

	// it's fine for this to error out; we might be in a framework or sth
	namespace, _ := namespaceFromYamlPath(filepath.Join(pkgPath, "meta", "package.yaml"))

	if err := verifyBinariesYaml(binary); err != nil {
		return "", err
	}

	actualBinPath := binPathForBinary(pkgPath, binary)
	udevPartName, err := getUdevPartName(m, pkgPath)
	if err != nil {
		return "", err
	}

	var templateOut bytes.Buffer
	t := template.Must(template.New("wrapper").Parse(wrapperTemplate))
	wrapperData := struct {
		Name        string
		Version     string
		Target      string
		Path        string
		AaProfile   string
		UdevAppName string
		Namespace   string
	}{
		Name:        m.Name,
		Version:     m.Version,
		Target:      actualBinPath,
		Path:        pkgPath,
		AaProfile:   aaProfile,
		UdevAppName: udevPartName,
		Namespace:   namespace,
	}
	t.Execute(&templateOut, wrapperData)

	return templateOut.String(), nil
}

// verifyStructStringsAgainstWhitelist takes a struct and ensures that
// the given whitelist regexp matches all string fields of the struct
func verifyStructStringsAgainstWhitelist(s interface{}, whitelist string) error {
	r, err := regexp.Compile(whitelist)
	if err != nil {
		return err
	}

	// check all members of the services struct against our whitelist
	t := reflect.TypeOf(s)
	v := reflect.ValueOf(s)
	for i := 0; i < t.NumField(); i++ {

		// PkgPath means its a unexported field and we can ignore it
		if t.Field(i).PkgPath != "" {
			continue
		}
		if v.Field(i).Kind() == reflect.Ptr {
			vi := v.Field(i).Elem()
			if vi.Kind() == reflect.Struct {
				return verifyStructStringsAgainstWhitelist(vi.Interface(), whitelist)
			}
		}
		if v.Field(i).Kind() == reflect.Struct {
			vi := v.Field(i).Interface()
			return verifyStructStringsAgainstWhitelist(vi, whitelist)
		}
		if v.Field(i).Kind() == reflect.String {
			key := t.Field(i).Name
			value := v.Field(i).String()
			if !r.MatchString(value) {
				return &ErrStructIllegalContent{
					field:     key,
					content:   value,
					whitelist: whitelist,
				}
			}
		}
	}

	return nil
}

func verifyServiceYaml(service Service) error {
	return verifyStructStringsAgainstWhitelist(service, servicesBinariesStringsWhitelist)
}

func generateSnapServicesFile(service Service, baseDir string, aaProfile string, m *packageYaml) (string, error) {
	if err := verifyServiceYaml(service); err != nil {
		return "", err
	}

	udevPartName, err := getUdevPartName(m, baseDir)
	if err != nil {
		return "", err
	}

	return systemd.New(globalRootDir, nil).GenServiceFile(
		&systemd.ServiceDescription{
			AppName:     m.Name,
			ServiceName: service.Name,
			Version:     m.Version,
			Description: service.Description,
			AppPath:     baseDir,
			Start:       service.Start,
			Stop:        service.Stop,
			PostStop:    service.PostStop,
			StopTimeout: time.Duration(service.StopTimeout),
			AaProfile:   aaProfile,
			IsFramework: m.Type == SnapTypeFramework,
			BusName:     service.BusName,
			UdevAppName: udevPartName,
		}), nil
}

func generateServiceFileName(m *packageYaml, service Service) string {
	return filepath.Join(snapServicesDir, fmt.Sprintf("%s_%s_%s.service", m.Name, service.Name, m.Version))
}

func generateBusPolicyFileName(m *packageYaml, service Service) string {
	return filepath.Join(snapBusPolicyDir, fmt.Sprintf("%s_%s_%s.conf", m.Name, service.Name, m.Version))
}

// takes a directory and removes the global root, this is needed
// when the SetRoot option is used and we need to generate
// content for the "Services" and "Binaries" section
var stripGlobalRootDir = stripGlobalRootDirImpl

func stripGlobalRootDirImpl(dir string) string {
	if globalRootDir == "/" {
		return dir
	}

	return dir[len(globalRootDir):]
}

func checkPackageForNameClashes(baseDir string) error {
	m, err := parsePackageYamlFile(filepath.Join(baseDir, "meta", "package.yaml"))
	if err != nil {
		return err
	}

	return m.checkForNameClashes()
}

func addPackageServices(baseDir string, inhibitHooks bool, inter interacter) error {
	m, err := parsePackageYamlFile(filepath.Join(baseDir, "meta", "package.yaml"))
	if err != nil {
		return err
	}

	for _, service := range m.Services {
		aaProfile, err := getSecurityProfile(m, service.Name, baseDir)
		if err != nil {
			return err
		}
		// this will remove the global base dir when generating the
		// service file, this ensures that /apps/foo/1.0/bin/start
		// is in the service file when the SetRoot() option
		// is used
		realBaseDir := stripGlobalRootDir(baseDir)
		content, err := generateSnapServicesFile(service, realBaseDir, aaProfile, m)
		if err != nil {
			return err
		}
		serviceFilename := generateServiceFileName(m, service)
		helpers.EnsureDir(filepath.Dir(serviceFilename), 0755)
		if err := ioutil.WriteFile(serviceFilename, []byte(content), 0644); err != nil {
			return err
		}

		// If necessary, generate the DBus policy file so the framework
		// service is allowed to start
		if m.Type == SnapTypeFramework && service.BusName != "" {
			content, err := genBusPolicyFile(service.BusName)
			if err != nil {
				return err
			}
			policyFilename := generateBusPolicyFileName(m, service)
			helpers.EnsureDir(filepath.Dir(policyFilename), 0755)
			if err := ioutil.WriteFile(policyFilename, []byte(content), 0644); err != nil {
				return err
			}
		}

		// daemon-reload and start only if we are not in the
		// inhibitHooks mode
		//
		// *but* always run enable (which just sets a symlink)
		serviceName := filepath.Base(generateServiceFileName(m, service))
		sysd := systemd.New(globalRootDir, inter)
		if !inhibitHooks {
			if err := sysd.DaemonReload(); err != nil {
				return err
			}
		}

		// we always enable the service even in inhibit hooks
		if err := sysd.Enable(serviceName); err != nil {
			return err
		}

		if !inhibitHooks {
			if err := sysd.Start(serviceName); err != nil {
				return err
			}
		}
	}

	return nil
}

func removePackageServices(baseDir string, inter interacter) error {
	m, err := parsePackageYamlFile(filepath.Join(baseDir, "meta", "package.yaml"))
	if err != nil {
		return err
	}
	sysd := systemd.New(globalRootDir, inter)
	for _, service := range m.Services {
		serviceName := filepath.Base(generateServiceFileName(m, service))
		if err := sysd.Disable(serviceName); err != nil {
			return err
		}
		if err := sysd.Stop(serviceName, time.Duration(service.StopTimeout)); err != nil {
			if !systemd.IsTimeout(err) {
				return err
			}
			inter.Notify(fmt.Sprintf("%s refused to stop, killing.", serviceName))
			// ignore errors for kill; nothing we'd do differently at this point
			sysd.Kill(serviceName, "TERM")
			time.Sleep(killWait)
			sysd.Kill(serviceName, "KILL")
		}

		if err := os.Remove(generateServiceFileName(m, service)); err != nil && !os.IsNotExist(err) {
			log.Printf("Warning: failed to remove service file for %s: %v", serviceName, err)
		}

		// Also remove DBus system policy file
		if err := os.Remove(generateBusPolicyFileName(m, service)); err != nil && !os.IsNotExist(err) {
			log.Printf("Warning: failed to remove bus policy file for service %s: %v", serviceName, err)
		}
	}

	// only reload if we actually had services
	if len(m.Services) > 0 {
		if err := sysd.DaemonReload(); err != nil {
			return err
		}
	}

	return nil
}

func addPackageBinaries(baseDir string) error {
	m, err := parsePackageYamlFile(filepath.Join(baseDir, "meta", "package.yaml"))
	if err != nil {
		return err
	}

	if err := os.MkdirAll(snapBinariesDir, 0755); err != nil {
		return err
	}

	for _, binary := range m.Binaries {
		aaProfile, err := getSecurityProfile(m, binary.Name, baseDir)
		if err != nil {
			return err
		}
		// this will remove the global base dir when generating the
		// service file, this ensures that /apps/foo/1.0/bin/start
		// is in the service file when the SetRoot() option
		// is used
		realBaseDir := stripGlobalRootDir(baseDir)
		content, err := generateSnapBinaryWrapper(binary, realBaseDir, aaProfile, m)
		if err != nil {
			return err
		}

		if err := ioutil.WriteFile(generateBinaryName(m, binary), []byte(content), 0755); err != nil {
			return err
		}
	}

	return nil
}

func removePackageBinaries(baseDir string) error {
	m, err := parsePackageYamlFile(filepath.Join(baseDir, "meta", "package.yaml"))
	if err != nil {
		return err
	}
	for _, binary := range m.Binaries {
		os.Remove(generateBinaryName(m, binary))
	}

	return nil
}

func addOneSecurityPolicy(m *packageYaml, name string, sd SecurityDefinitions, baseDir string) error {
	profileName, err := getSecurityProfile(m, filepath.Base(name), baseDir)
	if err != nil {
		return err
	}
	content, err := generateSeccompPolicy(baseDir, name, sd)
	if err != nil {
		return err
	}

	fn := filepath.Join(snapSeccompDir, profileName)
	if err := ioutil.WriteFile(fn, content, 0644); err != nil {
		return err
	}

	return nil
}

func (m *packageYaml) addSecurityPolicy(baseDir string) error {
	// TODO: move apparmor policy generation here too, its currently
	//       done via the click hooks but we really want to generate
	//       it all here

	for _, svc := range m.Services {
		if err := addOneSecurityPolicy(m, svc.Name, svc.SecurityDefinitions, baseDir); err != nil {
			return err
		}
	}

	for _, bin := range m.Binaries {
		if err := addOneSecurityPolicy(m, bin.Name, bin.SecurityDefinitions, baseDir); err != nil {
			return err
		}
	}

	return nil
}

func removeOneSecurityPolicy(m *packageYaml, name, baseDir string) error {
	profileName, err := getSecurityProfile(m, filepath.Base(name), baseDir)
	if err != nil {
		return err
	}
	fn := filepath.Join(snapSeccompDir, profileName)
	if err := os.Remove(fn); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func (m *packageYaml) removeSecurityPolicy(baseDir string) error {
	// TODO: move apparmor policy removal here
	for _, service := range m.Services {
		if err := removeOneSecurityPolicy(m, service.Name, baseDir); err != nil {
			return err
		}
	}

	for _, binary := range m.Binaries {
		if err := removeOneSecurityPolicy(m, binary.Name, baseDir); err != nil {
			return err
		}
	}

	return nil
}

// takes a name and PATH (colon separated) and returns the full qualified path
func findBinaryInPath(name, path string) string {
	for _, entry := range strings.Split(path, ":") {
		fname := filepath.Join(entry, name)
		if st, err := os.Stat(fname); err == nil {
			// check for any x bit
			if st.Mode()&0111 != 0 {
				return fname
			}
		}
	}

	return ""
}

// unpackWithDropPrivs is a helper that will unapck the ClickDeb content
// into the target dir and drop privs when doing this.
//
// To do this reliably in go we need to exec a helper as we can not
// just fork() and drop privs in the child (no support for stock fork in go)
func unpackWithDropPrivs(d *clickdeb.ClickDeb, instDir string) error {
	// no need to drop privs, we are not root
	if !helpers.ShouldDropPrivs() {
		return d.Unpack(instDir)
	}

	// find priv helper executable
	privHelper := ""
	for _, path := range []string{"PATH", "GOPATH"} {
		privHelper = findBinaryInPath("snappy", os.Getenv(path))
		if privHelper != "" {
			break
		}
	}
	if privHelper == "" {
		return ErrUnpackHelperNotFound
	}

	cmd := exec.Command(privHelper, "internal-unpack", d.Name(), instDir, globalRootDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return &ErrUnpackFailed{
			snapFile: d.Name(),
			instDir:  instDir,
			origErr:  err,
		}
	}

	return nil
}

type agreer interface {
	Agreed(intro, license string) bool
}

type interacter interface {
	agreer
	Notify(status string)
}

// this rewrites the json manifest to include the namespace in the on-disk
// manifest.json to be compatible with click again
func writeCompatManifestJSON(clickMetaDir string, manifestData []byte, namespace string) error {
	var cm clickManifest
	if err := json.Unmarshal(manifestData, &cm); err != nil {
		return err
	}

	if cm.Type != SnapTypeFramework && cm.Type != SnapTypeOem {
		// add the namespace to the name
		cm.Name = fmt.Sprintf("%s.%s", cm.Name, namespace)
	}

	outStr, err := json.MarshalIndent(cm, "", "  ")
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(path.Join(clickMetaDir, cm.Name+".manifest"), []byte(outStr), 0644); err != nil {
		return err
	}

	return nil
}

func installClick(snapFile string, flags InstallFlags, inter interacter, namespace string) (name string, err error) {
	allowUnauthenticated := (flags & AllowUnauthenticated) != 0
	if err := auditClick(snapFile, allowUnauthenticated); err != nil {
		return "", err
		// ?
		//return SnapAuditError
	}

	d, err := clickdeb.Open(snapFile)
	if err != nil {
		return "", err
	}
	defer d.Close()

	manifestData, err := d.ControlMember("manifest")
	if err != nil {
		log.Printf("Snap inspect failed: %s", snapFile)
		return "", err
	}

	manifest, err := readClickManifest([]byte(manifestData))
	if err != nil {
		return "", err
	}

	yamlData, err := d.MetaMember("package.yaml")
	if err != nil {
		return "", err
	}

	m, err := parsePackageYamlData(yamlData)
	if err != nil {
		return "", err
	}

	if err := m.checkForPackageInstalled(namespace); err != nil {
		return "", err
	}

	if err := m.checkForNameClashes(); err != nil {
		return "", err
	}

	if err := m.checkForFrameworks(); err != nil {
		return "", err
	}

	targetDir := snapAppsDir
	// the "oem" parts are special
	if manifest.Type == SnapTypeOem {
		targetDir = snapOemDir

		// TODO do the following at a higher level once the store publishes snap types
		// this is horrible
		if allowOEM := (flags & AllowOEM) != 0; !allowOEM {
			if currentOEM, err := getOem(); err == nil {
				if currentOEM.Name != manifest.Name {
					fmt.Println(currentOEM.Name, manifest.Name)
					return "", ErrOEMPackageInstall
				}
			} else {
				// there should always be an oem package now
				return "", ErrOEMPackageInstall
			}
		}

		if err := installOemHardwareUdevRules(m); err != nil {
			return "", err
		}
	}

	fullName := manifest.Name
	// namespacing only applies to apps.
	if manifest.Type != SnapTypeFramework && manifest.Type != SnapTypeOem {
		fullName += "." + namespace
	}
	instDir := filepath.Join(targetDir, fullName, manifest.Version)
	currentActiveDir, _ := filepath.EvalSymlinks(filepath.Join(instDir, "..", "current"))

	if err := m.checkLicenseAgreement(inter, d, currentActiveDir); err != nil {
		return "", err
	}

	dataDir := filepath.Join(snapDataDir, fullName, manifest.Version)

	if err := helpers.EnsureDir(instDir, 0755); err != nil {
		log.Printf("WARNING: Can not create %s", instDir)
	}

	// if anything goes wrong here we cleanup
	defer func() {
		if err != nil {
			if e := os.RemoveAll(instDir); e != nil && !os.IsNotExist(e) {
				log.Printf("Warning: failed to remove %s: %s", instDir, e)
			}
		}
	}()

	// we need to call the external helper so that we can reliable drop
	// privs
	if err := unpackWithDropPrivs(d, instDir); err != nil {
		return "", err
	}

	// legacy, the hooks (e.g. apparmor) need this. Once we converted
	// all hooks this can go away
	clickMetaDir := path.Join(instDir, ".click", "info")
	if err := os.MkdirAll(clickMetaDir, 0755); err != nil {
		return "", err
	}
	if err := writeCompatManifestJSON(clickMetaDir, manifestData, namespace); err != nil {
		return "", err
	}

	// write the hashes now
	if err := writeHashesFile(d, instDir); err != nil {
		return "", err
	}

	inhibitHooks := (flags & InhibitHooks) != 0

	// deal with the data:
	//
	// if there was a previous version, stop it
	// from being active so that it stops running and can no longer be
	// started then copy the data
	//
	// otherwise just create a empty data dir
	if currentActiveDir != "" {
		oldManifest, err := readClickManifestFromClickDir(currentActiveDir)
		if err != nil {
			return "", err
		}

		// we need to stop making it active
		err = unsetActiveClick(currentActiveDir, inhibitHooks, inter)
		defer func() {
			if err != nil {
				if cerr := setActiveClick(currentActiveDir, inhibitHooks, inter); cerr != nil {
					log.Printf("setting old version back to active failed: %v", cerr)
				}
			}
		}()
		if err != nil {
			return "", err
		}

		err = copySnapData(fullName, oldManifest.Version, manifest.Version)
	} else {
		err = helpers.EnsureDir(dataDir, 0755)
	}

	defer func() {
		if err != nil {
			if cerr := removeSnapData(fullName, manifest.Version); cerr != nil {
				log.Printf("when clenaning up data for %s %s: %v", manifest.Name, manifest.Version, cerr)
			}
		}
	}()

	if err != nil {
		return "", err
	}

	// and finally make active
	err = setActiveClick(instDir, inhibitHooks, inter)
	defer func() {
		if err != nil && currentActiveDir != "" {
			if cerr := setActiveClick(currentActiveDir, inhibitHooks, inter); cerr != nil {
				log.Printf("when setting old %s version back to active: %v", manifest.Name, cerr)
			}
		}
	}()
	if err != nil {
		return "", err
	}

	// oh, one more thing: refresh the security bits
	if !inhibitHooks {
		part, err := NewSnapPartFromYaml(filepath.Join(instDir, "meta", "package.yaml"), namespace, m)
		if err != nil {
			return "", err
		}

		deps, err := part.Dependents()
		if err != nil {
			return "", err
		}

		sysd := systemd.New(globalRootDir, inter)
		stopped := make(map[string]time.Duration)
		defer func() {
			if err != nil {
				for serviceName := range stopped {
					if e := sysd.Start(serviceName); e != nil {
						inter.Notify(fmt.Sprintf("unable to restart %s with the old %s: %s", serviceName, part.Name(), e))
					}
				}
			}
		}()

		for _, dep := range deps {
			if !dep.IsActive() {
				continue
			}
			for _, svc := range dep.Services() {
				serviceName := filepath.Base(generateServiceFileName(dep.m, svc))
				timeout := time.Duration(svc.StopTimeout)
				if err = sysd.Stop(serviceName, timeout); err != nil {
					inter.Notify(fmt.Sprintf("unable to stop %s; aborting install: %s", serviceName, err))
					return "", err
				}
				stopped[serviceName] = timeout
			}
		}

		if err := part.RefreshDependentsSecurity(currentActiveDir, inter); err != nil {
			return "", err
		}

		started := make(map[string]time.Duration)
		defer func() {
			if err != nil {
				for serviceName, timeout := range started {
					if e := sysd.Stop(serviceName, timeout); e != nil {
						inter.Notify(fmt.Sprintf("unable to stop %s with the old %s: %s", serviceName, part.Name(), e))
					}
				}
			}
		}()
		for serviceName, timeout := range stopped {
			if err = sysd.Start(serviceName); err != nil {
				inter.Notify(fmt.Sprintf("unable to restart %s; aborting install: %s", serviceName, err))
				return "", err
			}
			started[serviceName] = timeout
		}
	}

	return manifest.Name, nil
}

// removeSnapData removes the data for the given version of the given snap
func removeSnapData(fullName, version string) error {
	dirs, err := snapDataDirs(fullName, version)
	if err != nil {
		return err
	}

	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			return err
		}
		os.Remove(filepath.Dir(dir))
	}

	return nil
}

// snapDataDirs returns the list of data directories for the given snap version
func snapDataDirs(fullName, version string) ([]string, error) {
	// collect the directories, homes first
	dirs, err := filepath.Glob(filepath.Join(snapDataHomeGlob, fullName, version))
	if err != nil {
		return nil, err
	}
	// then system data
	systemPath := filepath.Join(snapDataDir, fullName, version)
	dirs = append(dirs, systemPath)

	return dirs, nil
}

// Copy all data for "fullName" from "oldVersion" to "newVersion"
// (but never overwrite)
func copySnapData(fullName, oldVersion, newVersion string) (err error) {
	oldDataDirs, err := snapDataDirs(fullName, oldVersion)
	if err != nil {
		return err
	}

	for _, oldDir := range oldDataDirs {
		// replace the trailing "../$old-ver" with the "../$new-ver"
		newDir := filepath.Join(filepath.Dir(oldDir), newVersion)
		if err := copySnapDataDirectory(oldDir, newDir); err != nil {
			return err
		}
	}

	return nil
}

// Lowlevel copy the snap data (but never override existing data)
func copySnapDataDirectory(oldPath, newPath string) (err error) {
	if _, err := os.Stat(oldPath); err == nil {
		if _, err := os.Stat(newPath); err != nil {
			// there is no golang "CopyFile"
			cmd := exec.Command("cp", "-a", oldPath, newPath)
			if err := cmd.Run(); err != nil {
				if exitCode, err := helpers.ExitCode(err); err != nil {
					return &ErrDataCopyFailed{
						oldPath:  oldPath,
						newPath:  newPath,
						exitCode: exitCode}
				}
				return err
			}
		}
	}
	return nil
}

func unsetActiveClick(clickDir string, inhibitHooks bool, inter interacter) error {
	currentSymlink := filepath.Join(clickDir, "..", "current")

	// sanity check
	currentActiveDir, err := filepath.EvalSymlinks(currentSymlink)
	if err != nil {
		return err
	}
	if clickDir != currentActiveDir {
		return ErrSnapNotActive
	}

	// remove generated services, binaries, clickHooks, security policy
	if err := removePackageBinaries(clickDir); err != nil {
		return err
	}

	if err := removePackageServices(clickDir, inter); err != nil {
		return err
	}

	m, err := parsePackageYamlFile(filepath.Join(clickDir, "meta", "package.yaml"))
	if err != nil {
		return err
	}
	if err := m.removeSecurityPolicy(clickDir); err != nil {
		return err
	}

	manifest, err := readClickManifestFromClickDir(clickDir)
	if err != nil {
		return err
	}

	if manifest.Type == SnapTypeFramework {

		if err := policy.Remove(m.Name, clickDir); err != nil {
			return err
		}
	}

	if err := removeClickHooks(manifest, inhibitHooks); err != nil {
		return err
	}

	// and finally the current symlink
	if err := os.Remove(currentSymlink); err != nil {
		log.Printf("Warning: failed to remove %s: %s", currentSymlink, err)
	}

	return nil
}

func setActiveClick(baseDir string, inhibitHooks bool, inter interacter) error {
	currentActiveSymlink := filepath.Join(baseDir, "..", "current")
	currentActiveDir, _ := filepath.EvalSymlinks(currentActiveSymlink)

	// already active, nothing to do
	if baseDir == currentActiveDir {
		return nil
	}

	// there is already an active part
	if currentActiveDir != "" {
		unsetActiveClick(currentActiveDir, inhibitHooks, inter)
	}

	// make new part active
	newActiveManifest, err := readClickManifestFromClickDir(baseDir)
	if err != nil {
		return err
	}

	// yes, its confusing, we have two manifests, this is the important
	// one, the YAML one
	m, err := parsePackageYamlFile(filepath.Join(baseDir, "meta", "package.yaml"))
	if err != nil {
		return err
	}

	if newActiveManifest.Type == SnapTypeFramework {
		if err := policy.Install(m.Name, baseDir); err != nil {
			return err
		}
	}

	if err := installClickHooks(baseDir, newActiveManifest, inhibitHooks); err != nil {
		// cleanup the failed hooks
		removeClickHooks(newActiveManifest, inhibitHooks)
		return err
	}

	// generate the security policy from the package.yaml
	if err := m.addSecurityPolicy(baseDir); err != nil {
		return err
	}

	// add the "binaries:" from the package.yaml
	if err := addPackageBinaries(baseDir); err != nil {
		return err
	}
	// add the "services:" from the package.yaml
	if err := addPackageServices(baseDir, inhibitHooks, inter); err != nil {
		return err
	}

	// FIXME: we want to get rid of the current symlink
	if err := os.Remove(currentActiveSymlink); err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: failed to remove %s: %s", currentActiveSymlink, err)
	}

	// symlink is relative to parent dir
	return os.Symlink(filepath.Base(baseDir), currentActiveSymlink)
}

// RunHooks will run all click system hooks
func RunHooks() error {
	systemHooks, err := systemClickHooks()
	if err != nil {
		return err
	}

	for _, hook := range systemHooks {
		if hook.exec != "" {
			if err := execHook(hook.exec); err != nil {
				return err
			}
		}
	}

	return nil
}
