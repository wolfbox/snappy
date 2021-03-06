package snappy

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
	"launchpad.net/snappy/helpers"
)

type apparmorJSONTemplate struct {
	Template      string   `json:"template"`
	PolicyGroups  []string `json:"policy_groups"`
	PolicyVendor  string   `json:"policy_vendor"`
	PolicyVersion float64  `json:"policy_version"`
}

type securitySeccompOverride struct {
	Template      string   `yaml:"security-template,omitempty"`
	PolicyGroups  []string `yaml:"caps,omitempty"`
	Syscalls      []string `yaml:"syscalls,omitempty"`
	PolicyVendor  string   `yaml:"policy-vendor"`
	PolicyVersion float64  `yaml:"policy-version"`
}

const defaultTemplate = "default"

var defaultPolicyGroups = []string{"networking"}

// TODO: autodetect, this won't work for personal
const defaultPolicyVendor = "ubuntu-core"
const defaultPolicyVersion = 15.04

func (s *SecurityDefinitions) generateApparmorJSONContent() ([]byte, error) {
	t := apparmorJSONTemplate{
		Template:      s.SecurityTemplate,
		PolicyGroups:  s.SecurityCaps,
		PolicyVendor:  defaultPolicyVendor,
		PolicyVersion: defaultPolicyVersion,
	}

	// FIXME: this is snappy specific, on other systems like the
	//        phone we may want different defaults.
	if t.Template == "" && t.PolicyGroups == nil {
		t.PolicyGroups = defaultPolicyGroups
	}

	// never write a null value out into the json
	if t.PolicyGroups == nil {
		t.PolicyGroups = []string{}
	}

	if t.Template == "" {
		t.Template = defaultTemplate
	}

	outStr, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, err
	}

	return outStr, nil
}

func handleApparmor(buildDir string, m *packageYaml, hookName string, s *SecurityDefinitions) error {

	// ensure we have a hook
	if _, ok := m.Integration[hookName]; !ok {
		m.Integration[hookName] = clickAppHook{}
	}

	// legacy use of "Integration" - the user should
	// use the new format, nothing needs to be done
	_, hasApparmor := m.Integration[hookName]["apparmor"]
	_, hasApparmorProfile := m.Integration[hookName]["apparmor-profile"]
	if hasApparmor || hasApparmorProfile {
		return nil
	}

	// see if we have a custom security policy
	if s.SecurityPolicy != nil && s.SecurityPolicy.Apparmor != "" {
		m.Integration[hookName]["apparmor-profile"] = s.SecurityPolicy.Apparmor
		return nil
	}

	// see if we have a security override
	if s.SecurityOverride != nil && s.SecurityOverride.Apparmor != "" {
		m.Integration[hookName]["apparmor"] = s.SecurityOverride.Apparmor
		return nil
	}

	// generate apparmor template
	apparmorJSONFile := filepath.Join("meta", hookName+".apparmor")
	securityJSONContent, err := s.generateApparmorJSONContent()
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(buildDir, apparmorJSONFile), securityJSONContent, 0644); err != nil {
		return err
	}

	m.Integration[hookName]["apparmor"] = apparmorJSONFile

	return nil
}

func getSecurityProfile(m *packageYaml, appName, baseDir string) (string, error) {
	cleanedName := strings.Replace(appName, "/", "-", -1)
	if m.Type == SnapTypeFramework || m.Type == SnapTypeOem {
		return fmt.Sprintf("%s_%s_%s", m.Name, cleanedName, m.Version), nil
	}

	namespace, err := namespaceFromYamlPath(filepath.Join(baseDir, "meta", "package.yaml"))

	return fmt.Sprintf("%s.%s_%s_%s", m.Name, namespace, cleanedName, m.Version), err
}

var runScFilterGen = runScFilterGenImpl

func runScFilterGenImpl(argv ...string) ([]byte, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	return cmd.Output()
}

// seccomp specific
func generateSeccompPolicy(baseDir, appName string, sd SecurityDefinitions) ([]byte, error) {
	if sd.SecurityPolicy != nil && sd.SecurityPolicy.Seccomp != "" {
		fn := filepath.Join(baseDir, sd.SecurityPolicy.Seccomp)
		content, err := ioutil.ReadFile(fn)
		if err != nil {
			log.Printf("WARNING: failed to read %s\n", fn)
		}
		return content, err
	}

	helpers.EnsureDir(snapSeccompDir, 0755)

	// defaults
	policyVendor := defaultPolicyVendor
	policyVersion := defaultPolicyVersion
	template := defaultTemplate
	caps := []string{}
	for _, p := range defaultPolicyGroups {
		caps = append(caps, p)
	}
	syscalls := []string{}

	if sd.SecurityOverride != nil {
		fn := filepath.Join(baseDir, sd.SecurityOverride.Seccomp)
		var s securitySeccompOverride
		err := readSeccompOverride(fn, &s)
		if err != nil {
			log.Printf("WARNING: failed to read %s\n", fn)
			return nil, err
		}

		if s.Template != "" {
			template = s.Template
		}
		if s.PolicyVendor != "" {
			policyVendor = s.PolicyVendor
		}
		if s.PolicyVersion != 0 {
			policyVersion = s.PolicyVersion
		}
		caps = s.PolicyGroups
		syscalls = s.Syscalls
	} else {
		if sd.SecurityTemplate != "" {
			template = sd.SecurityTemplate
		}
		if sd.SecurityCaps != nil {
			caps = sd.SecurityCaps
		}
	}

	// Build up the command line
	args := []string{
		"sc-filtergen",
		fmt.Sprintf("--include-policy-dir=%s", filepath.Dir(snapSeccompDir)),
		fmt.Sprintf("--policy-vendor=%s", policyVendor),
		fmt.Sprintf("--policy-version=%.2f", policyVersion),
		fmt.Sprintf("--template=%s", template),
	}
	if len(caps) > 0 {
		args = append(args, fmt.Sprintf("--policy-groups=%s", strings.Join(caps, ",")))
	}
	if len(syscalls) > 0 {
		args = append(args, fmt.Sprintf("--syscalls=%s", strings.Join(syscalls, ",")))
	}

	content, err := runScFilterGen(args...)
	if err != nil {
		log.Printf("WARNING: %v failed\n", args)
	}

	return content, err
}

func readSeccompOverride(yamlPath string, s *securitySeccompOverride) error {
	yamlData, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(yamlData, &s)
	if err != nil {
		log.Printf("ERROR: Can not parse '%s'", yamlData)
		return err
	}
	// These must always be specified together
	if s.PolicyVersion == 0 && s.PolicyVendor != "" {
		s.PolicyVendor = ""
		log.Printf("WARNING: policy-version not set with policy-vendor. Skipping 'policy-vendor'\n")
	} else if s.PolicyVersion != 0 && s.PolicyVendor == "" {
		s.PolicyVersion = 0
		log.Printf("WARNING: policy-vendor not set with policy-version. Skipping 'policy-version'\n")
	}

	return nil
}
