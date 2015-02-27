package helpers

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	. "launchpad.net/gocheck"
)

func Test(t *testing.T) { TestingT(t) }

type HTestSuite struct{}

var _ = Suite(&HTestSuite{})

func (ts *HTestSuite) TestUnpack(c *C) {

	// setup tmpdir
	tmpdir := c.MkDir()
	tmpfile := filepath.Join(tmpdir, "foo.tar.gz")

	// ok, slightly silly
	path := "/etc/fstab"

	// create test data
	cmd := exec.Command("tar", "cvzf", tmpfile, path)
	output, err := cmd.CombinedOutput()
	c.Assert(err, IsNil)
	if !strings.Contains(string(output), "/etc/fstab") {
		c.Error("Can not find expected output from tar")
	}

	// unpack
	unpackdir := filepath.Join(tmpdir, "t")
	err = unpackTar(tmpfile, unpackdir)
	c.Assert(err, IsNil)

	_, err = os.Open(filepath.Join(tmpdir, "t/etc/fstab"))
	c.Assert(err, IsNil)
}

func (ts *HTestSuite) TestGetMapFromValidYaml(c *C) {
	m, err := getMapFromYaml([]byte("name: value"))
	c.Assert(err, IsNil)
	me := map[string]interface{}{"name": "value"}
	if !reflect.DeepEqual(m, me) {
		c.Error(fmt.Sprintf("Unexpected map %v != %v", m, me))
	}
}

func (ts *HTestSuite) TestGetMapFromInvalidYaml(c *C) {
	_, err := getMapFromYaml([]byte("%lala%"))
	c.Assert(err, NotNil)
}

func (ts *HTestSuite) TestArchitectue(c *C) {
	goarch = "arm"
	c.Check(Architecture(), Equals, "armhf")

	goarch = "amd64"
	c.Check(Architecture(), Equals, "amd64")

	goarch = "386"
	c.Check(Architecture(), Equals, "i386")
}

func (ts *HTestSuite) TestChdir(c *C) {
	tmpdir := c.MkDir()

	cwd, err := os.Getwd()
	c.Assert(err, IsNil)
	c.Assert(cwd, Not(Equals), tmpdir)
	ChDir(tmpdir, func() {
		cwd, err := os.Getwd()
		c.Assert(err, IsNil)
		c.Assert(cwd, Equals, tmpdir)
	})
}

func (ts *HTestSuite) TestExitCode(c *C) {
	cmd := exec.Command("true")
	err := cmd.Run()
	c.Assert(err, IsNil)

	cmd = exec.Command("false")
	err = cmd.Run()
	c.Assert(err, NotNil)
	e, err := ExitCode(err)
	c.Assert(err, IsNil)
	c.Assert(e, Equals, 1)

	cmd = exec.Command("sh", "-c", "exit 7")
	err = cmd.Run()
	e, err = ExitCode(err)
	c.Assert(e, Equals, 7)

	// ensure that non exec.ExitError values give a error
	_, err = os.Stat("/random/file/that/is/not/there")
	c.Assert(err, NotNil)
	_, err = ExitCode(err)
	c.Assert(err, NotNil)
}

func (ts *HTestSuite) TestEnsureDir(c *C) {
	tempdir := c.MkDir()

	target := filepath.Join(tempdir, "meep")
	err := EnsureDir(target, 0755)
	c.Assert(err, IsNil)
	st, err := os.Stat(target)
	c.Assert(err, IsNil)
	c.Assert(st.IsDir(), Equals, true)
	c.Assert(st.Mode(), Equals, os.ModeDir|0755)
}

func (ts *HTestSuite) TestMakeMapFromEnvList(c *C) {
	envList := []string{
		"PATH=/usr/bin:/bin",
		"DBUS_SESSION_BUS_ADDRESS=unix:abstract=something1234",
	}
	envMap := MakeMapFromEnvList(envList)
	c.Assert(envMap, DeepEquals, map[string]string{
		"PATH": "/usr/bin:/bin",
		"DBUS_SESSION_BUS_ADDRESS": "unix:abstract=something1234",
	})
}

func (ts *HTestSuite) TestMakeMapFromEnvListInvalidInput(c *C) {
	envList := []string{
		"nonsesne",
	}
	envMap := MakeMapFromEnvList(envList)
	c.Assert(envMap, DeepEquals, map[string]string(nil))
}

func (ts *HTestSuite) TestSha512sum(c *C) {
	tempdir := c.MkDir()

	p := filepath.Join(tempdir, "foo")
	err := ioutil.WriteFile(p, []byte("x"), 0644)
	c.Assert(err, IsNil)
	hashsum, err := Sha512sum(p)
	c.Assert(err, IsNil)
	c.Assert(hashsum, Equals, "a4abd4448c49562d828115d13a1fccea927f52b4d5459297f8b43e42da89238bc13626e43dcb38ddb082488927ec904fb42057443983e88585179d50551afe62")
}
