package snappy

import (
	"io/ioutil"
	"os"
	"path/filepath"
)

const (
	packageHello = `
name: hello-app
version: 1.10
vendor: Michael Vogt <mvo@ubuntu.com>
icon: meta/hello.svg
binaries:
 - name: bin/hello
`
)

func makeMockSnap(tempdir string) (yamlFile string, err error) {
	metaDir := filepath.Join(tempdir, "apps", "hello-app", "1.10", "meta")
	err = os.MkdirAll(metaDir, 0777)
	if err != nil {
		return "", err
	}
	yamlFile = filepath.Join(metaDir, "package.yaml")
	ioutil.WriteFile(yamlFile, []byte(packageHello), 0666)

	return yamlFile, err
}
