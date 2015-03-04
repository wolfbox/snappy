package snappy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"launchpad.net/snappy/helpers"
)

const staticPreinst = `#! /bin/sh
echo "Snap packages may not be installed directly using dpkg."
echo "Use 'snappy install' instead."
exit 1
`

const defaultApparmorJSON = `{
    "template": "default",
    "policy_groups": [
        "networking"
    ],
    "policy_vendor": "ubuntu-snappy",
    "policy_version": 1.3
}`

func parseReadme(readme string) (title, description string, err error) {
	file, err := os.Open(readme)
	if err != nil {
		return "", "", err
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if title == "" {
			title = scanner.Text()
			continue
		}

		if description != "" && scanner.Text() == "" {
			break
		}
		description += scanner.Text()
	}
	if title == "" {
		return "", "", ErrReadmeInvalid
	}
	if description == "" {
		description = "no description"
	}

	return title, description, nil
}

func handleBinaries(buildDir string, m *packageYaml) error {
	for _, v := range m.Binaries {
		hookName := filepath.Base(v["name"])

		if _, ok := m.Integration[hookName]; !ok {
			m.Integration[hookName] = make(map[string]string)
		}
		m.Integration[hookName]["bin-path"] = v["name"]

		_, hasApparmor := m.Integration[hookName]["apparmor"]
		_, hasApparmorProfile := m.Integration[hookName]["apparmor-profile"]
		if !hasApparmor && !hasApparmorProfile {
			defaultApparmorJSONFile := filepath.Join("meta", hookName+".apparmor")
			if err := ioutil.WriteFile(filepath.Join(buildDir, defaultApparmorJSONFile), []byte(defaultApparmorJSON), 0644); err != nil {
				return err
			}
			m.Integration[hookName]["apparmor"] = defaultApparmorJSONFile
		}
	}

	return nil
}

func handleServices(buildDir string, m *packageYaml) error {
	_, description, err := parseReadme(filepath.Join(buildDir, "meta", "readme.md"))
	if err != nil {
		return err
	}

	for _, v := range m.Services {
		hookName := filepath.Base(v["name"])

		if _, ok := m.Integration[hookName]; !ok {
			m.Integration[hookName] = make(map[string]string)
		}

		// generate snappyd systemd unit json
		if v["description"] == "" {
			v["description"] = description
		}
		snappySystemdContent, err := json.MarshalIndent(v, "", " ")
		if err != nil {
			return err
		}
		snappySystemdContentFile := filepath.Join("meta", hookName+".snappy-systemd")
		if err := ioutil.WriteFile(filepath.Join(buildDir, snappySystemdContentFile), []byte(snappySystemdContent), 0644); err != nil {
			return err
		}
		m.Integration[hookName]["snappy-systemd"] = snappySystemdContentFile

		// generate apparmor
		_, hasApparmor := m.Integration[hookName]["apparmor"]
		_, hasApparmorProfile := m.Integration[hookName]["apparmor-profile"]
		if !hasApparmor && !hasApparmorProfile {
			defaultApparmorJSONFile := filepath.Join("meta", hookName+".apparmor")
			if err := ioutil.WriteFile(filepath.Join(buildDir, defaultApparmorJSONFile), []byte(defaultApparmorJSON), 0644); err != nil {
				return err
			}
			m.Integration[hookName]["apparmor"] = defaultApparmorJSONFile
		}
	}

	return nil
}

func handleConfigHookApparmor(buildDir string, m *packageYaml) error {
	configHookFile := filepath.Join(buildDir, "meta", "hooks", "config")
	if !helpers.FileExists(configHookFile) {
		return nil
	}

	hookName := "snappy-config"
	defaultApparmorJSONFile := filepath.Join("meta", hookName+".apparmor")
	if err := ioutil.WriteFile(filepath.Join(buildDir, defaultApparmorJSONFile), []byte(defaultApparmorJSON), 0644); err != nil {
		return err
	}
	m.Integration[hookName] = make(map[string]string)
	m.Integration[hookName]["apparmor"] = defaultApparmorJSONFile

	return nil
}

// the du(1) command, useful to override for testing
var duCmd = "du"

func getDirSize(buildDir string) (string, error) {
	cmd := exec.Command(duCmd, "-s", "--apparent-size", buildDir)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.Fields(string(output))[0], nil
}

func writeDebianControl(buildDir string, m *packageYaml) error {
	debianDir := filepath.Join(buildDir, "DEBIAN")
	if err := os.MkdirAll(debianDir, 0755); err != nil {
		return err
	}

	// get "du" output, a deb needs the size in 1k blocks
	installedSize, err := getDirSize(buildDir)
	if err != nil {
		return err
	}

	// title description
	title, description, err := parseReadme(filepath.Join(buildDir, "meta", "readme.md"))
	if err != nil {
		return err
	}

	// debian control

	// FIXME: use template package
	controlContent := fmt.Sprintf(`Package: %s
Version: %s
Architecture: %s
Maintainer: %s
Installed-Size: %s
Description: %s
 %s
`, m.Name, m.Version, m.Architecture, m.Vendor, installedSize, title, description)
	if err := ioutil.WriteFile(filepath.Join(debianDir, "control"), []byte(controlContent), 0644); err != nil {
		return err
	}

	// write static preinst
	return ioutil.WriteFile(filepath.Join(debianDir, "preinst"), []byte(staticPreinst), 0755)
}

func writeClickManifest(buildDir string, m *packageYaml) error {
	// get "du" output
	installedSize, err := getDirSize(buildDir)
	if err != nil {
		return err
	}

	// title description
	title, description, err := parseReadme(filepath.Join(buildDir, "meta", "readme.md"))
	if err != nil {
		return err
	}

	cm := clickManifest{
		Name:          m.Name,
		Version:       m.Version,
		Framework:     m.Framework,
		Icon:          m.Icon,
		InstalledSize: installedSize,
		Title:         title,
		Description:   description,
		Hooks:         m.Integration,
	}
	manifestContent, err := json.MarshalIndent(cm, "", " ")
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(filepath.Join(buildDir, "DEBIAN", "manifest"), []byte(manifestContent), 0644); err != nil {
		return err
	}

	return nil
}

func copyToBuildDir(sourceDir, buildDir string) error {
	// FIXME: too simplistic, we need a ignore pattern for stuff
	//        like "*~" etc
	os.Remove(buildDir)

	return exec.Command("cp", "-a", sourceDir, buildDir).Run()
}

// Build the given sourceDirectory and return the generated snap file
func Build(sourceDir string) (string, error) {

	// ensure we have valid content
	m, err := readPackageYaml(filepath.Join(sourceDir, "meta", "package.yaml"))
	if err != nil {
		return "", err
	}

	// create build dir
	buildDir, err := ioutil.TempDir("", "snappy-build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(buildDir)

	if err := copyToBuildDir(sourceDir, buildDir); err != nil {
		return "", err
	}

	// FIXME: the store needs this right now
	if !strings.Contains(m.Framework, "ubuntu-core-15.04-dev1") {
		l := strings.Split(m.Framework, ",")
		if l[0] == "" {
			m.Framework = "ubuntu-core-15.04-dev1"
		} else {
			l = append(l, "ubuntu-core-15.04-dev1")
			m.Framework = strings.Join(l, ", ")
		}
	}

	// defaults
	if m.Architecture == "" {
		m.Architecture = "all"
	}
	if m.Integration == nil {
		m.Integration = make(map[string]clickAppHook)
	}

	// generate compat hooks for binaries
	if err := handleBinaries(buildDir, m); err != nil {
		return "", err
	}

	// generate compat hooks for services
	if err := handleServices(buildDir, m); err != nil {
		return "", err
	}

	// generate config hook apparmor
	if err := handleConfigHookApparmor(buildDir, m); err != nil {
		return "", err
	}

	if err := writeDebianControl(buildDir, m); err != nil {
		return "", err
	}

	// manifest
	if err := writeClickManifest(buildDir, m); err != nil {
		return "", err
	}

	// build the package
	snapName := fmt.Sprintf("%s_%s_%s.snap", m.Name, m.Version, m.Architecture)
	// FIXME: we want a native build here without dpkg-deb to be
	//        about to build on non-ubuntu/debian systems
	cmd := exec.Command("fakeroot", "dpkg-deb", "--build", buildDir, snapName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		retCode, _ := helpers.ExitCode(err)
		return "", fmt.Errorf("failed with %d: %s", retCode, output)
	}

	return snapName, nil
}
