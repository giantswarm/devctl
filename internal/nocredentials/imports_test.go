package nocredentials

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportsNoReaderRejectsCredentialReaders(t *testing.T) {
	// Spell out the architectural boundary independently of Readers so
	// removing a reader from the guard cannot silently remove its test too.
	readers := []string{
		"github.com/giantswarm/devctl/v8/pkg/authstore",
		"github.com/giantswarm/devctl/v8/internal/laptop",
		"github.com/zalando/go-keyring",
		"github.com/docker/cli/cli/config",
		"k8s.io/client-go/tools/clientcmd",
	}
	for _, reader := range readers {
		for _, path := range []string{reader, reader + "/child"} {
			t.Run(path, func(t *testing.T) {
				dir := t.TempDir()
				writeImportFixture(t, dir, "engine.go", fmt.Sprintf("package engine\nimport _ %q\n", path))
				err := importsNoReader(dir)
				if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "engine.go") {
					t.Fatalf("want rejection naming the file and reader %s, got %v", path, err)
				}
			})
		}
	}
}

func TestImportsNoReaderChecksEveryPlatform(t *testing.T) {
	for _, tc := range []struct {
		file       string
		header     string
		importSpec string
	}{
		{"engine_linux.go", "", `_ "github.com/giantswarm/devctl/v8/pkg/authstore"`},
		{"engine_darwin.go", "", `. "github.com/giantswarm/devctl/v8/pkg/authstore"`},
		{"engine_windows.go", "", "reader `github.com/giantswarm/devctl/v8/pkg/authstore`"},
		{"engine_tagged.go", "//go:build nocredentials_fixture\n\n", `"github.com/giantswarm/devctl/v8/pkg/\x61uthstore"`},
	} {
		t.Run(tc.file, func(t *testing.T) {
			dir := t.TempDir()
			writeImportFixture(t, dir, tc.file, tc.header+"package engine\nimport "+tc.importSpec+"\n")
			err := importsNoReader(dir)
			if err == nil || !strings.Contains(err.Error(), tc.file) || !strings.Contains(err.Error(), "/pkg/authstore") {
				t.Fatalf("want the credential import rejected regardless of the host platform or build tags, got %v", err)
			}
		})
	}
}

func TestImportsNoReaderAllowsClientsAndTestFixtures(t *testing.T) {
	dir := t.TempDir()
	writeImportFixture(t, dir, "engine.go", `package engine
import (
    "github.com/giantswarm/devctl/v8/pkg/githubclient"
    "github.com/google/go-containerregistry/pkg/authn"
    "k8s.io/client-go/rest"
    "github.com/giantswarm/devctl/v8/pkg/authstoreextra"
)
`)
	writeImportFixture(t, dir, "engine_test.go", `package engine
import "github.com/giantswarm/devctl/v8/pkg/authstore"
`)
	writeImportFixture(t, dir, "engine_windows_test.go", `package engine
import "k8s.io/client-go/tools/clientcmd"
`)
	// A nested fixture is not a source file of the package being guarded.
	writeImportFixture(t, dir, "testdata/reader.go", `package fixture
import "github.com/zalando/go-keyring"
`)
	if err := importsNoReader(dir); err != nil {
		t.Fatalf("injected clients, unrelated package names and test fixtures are allowed: %v", err)
	}
}

func TestImportsNoReaderRejectsUnreadableImports(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(fmt.Sprintf("directory=%t", directory), func(t *testing.T) {
			dir := t.TempDir()
			if directory {
				if err := os.Mkdir(filepath.Join(dir, "engine.go"), 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				writeImportFixture(t, dir, "engine.go", "package engine\nimport (\n")
			}
			if err := importsNoReader(dir); err == nil || !strings.Contains(err.Error(), "engine.go") {
				t.Fatalf("want an import scan error naming the file, got %v", err)
			}
		})
	}
}

func writeImportFixture(t *testing.T, dir, name, source string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}
