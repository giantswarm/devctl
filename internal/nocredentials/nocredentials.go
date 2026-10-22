// Package nocredentials runs an engine package's tests with no credential
// store a laptop has: an engine acts only with the clients its caller builds
// from tokens, so every case passes when each store fails on any access.
package nocredentials

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// Readers are the packages that read a credential store or a kubeconfig;
// an engine package imports none of them.
var Readers = []string{
	"github.com/giantswarm/devctl/v8/pkg/authstore",
	"github.com/giantswarm/devctl/v8/internal/laptop",
	"github.com/zalando/go-keyring",
	"github.com/docker/cli/cli/config",
	"k8s.io/client-go/tools/clientcmd",
}

// Main is a TestMain: it checks that the package in the working directory
// imports none of [Readers], makes every credential store fail on any
// access, and runs the tests. devctl's keyring file, the docker
// configuration and KUBECONFIG sit below a regular file, so any read, write
// or lock of them fails; the GitHub token variables are unset.
func Main(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	if err := importsNoReader("."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	dir, err := os.MkdirTemp("", "devctl-no-credentials-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := failEveryStore(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

// failEveryStore points every store below a regular file in dir and proves
// the keyring fails.
func failEveryStore(dir string) error {
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		return err
	}
	env := map[string]string{
		agentcli.EnvKeyringFile: filepath.Join(file, "keyring.json"),
		"DOCKER_CONFIG":         filepath.Join(file, "docker"),
		"KUBECONFIG":            filepath.Join(file, "kubeconfig"),
	}
	for key, value := range env {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	for _, key := range append([]string{"GH_TOKEN"}, authstore.GitHubEnvVars...) {
		if err := os.Unsetenv(key); err != nil {
			return err
		}
	}
	_, err := authstore.OpenStore(agentcli.EndpointsFromEnv()).Get("github")
	if err == nil || errors.Is(err, authstore.ErrNotFound) {
		return fmt.Errorf("the credential store must fail on any access, Get answered %v", err)
	}
	return nil
}

// importsNoReader parses the non-test Go files of dir and refuses an import
// of [Readers].
func importsNoReader(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return err
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			for _, reader := range Readers {
				if path == reader || strings.HasPrefix(path, reader+"/") {
					return fmt.Errorf("%s imports %s: an engine takes its clients from the caller and reads no credential store or kubeconfig", name, path)
				}
			}
		}
	}
	return nil
}
