package mod

import (
	"io/ioutil"
	"path"
)

func ReadGomod(dir string) ([]byte, error) {
	return ioutil.ReadFile(path.Join(dir, "go.mod"))
}
