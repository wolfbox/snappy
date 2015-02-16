package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "launchpad.net/gocheck"
)

const configPassthroughScript = `#!/bin/sh

# temp location to store cfg
CFG="%s/config.out"

# just dump out for the tes
cat - > $CFG

# and config
cat $CFG
`

const configErrorScript = `#!/bin/sh

printf "error: some error"
exit 1
`

const configYaml = `
config:
  hello-world:
    key: value
`

func (s *SnapTestSuite) makeMockSnapWithConfig(c *C, configScript string) (snapDir string, err error) {
	yamlFile, err := s.makeMockSnap()
	c.Assert(err, IsNil)
	snapDir = filepath.Dir(yamlFile)
	err = os.Mkdir(filepath.Join(snapDir, "hooks"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(snapDir, "hooks", "config"), []byte(configScript), 0755)
	c.Assert(err, IsNil)

	return snapDir, nil
}

func (s *SnapTestSuite) TestConfigSimple(c *C) {
	mockConfig := fmt.Sprintf(configPassthroughScript, s.tempdir)
	snapDir, err := s.makeMockSnapWithConfig(c, mockConfig)
	c.Assert(err, IsNil)

	newConfig, err := snapConfig(snapDir, configYaml)
	c.Assert(err, IsNil)
	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "config.out"))
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, []byte(configYaml))
	c.Assert(newConfig, Equals, configYaml)
}

func (s *SnapTestSuite) TestConfigError(c *C) {
	snapDir, err := s.makeMockSnapWithConfig(c, configErrorScript)
	c.Assert(err, IsNil)

	newConfig, err := snapConfig(snapDir, configYaml)
	c.Assert(err, NotNil)
	c.Assert(newConfig, Equals, "")
	fmt.Println(err)
	c.Assert(strings.HasSuffix(err.Error(), "failed with: 'error: some error'"), Equals, true)
}
