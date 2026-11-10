package makefile

import (
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// Compile real packages: passing main.go alone must fail for the sibling fixture.
func TestGoBuildMainPackage(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the generated Makefile requires a POSIX shell")
	}
	for _, source := range []string{"main.go", "cmd/main.go"} {
		for _, multiFile := range []bool{false, true} {
			name := source + "/single"
			if multiFile {
				name = source + "/multiple"
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				body := "package main\nfunc main() {}\n"
				if multiFile {
					body = "package main\nfunc main() { sibling() }\n"
				}
				writeGoInstallFixture(t, dir, gen.FlavourSlice{gen.FlavourCLI}, source, body)
				if multiFile {
					if err := os.WriteFile(filepath.Join(dir, filepath.Dir(source), "sibling.go"), []byte("package main\nfunc sibling() {}\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/install-fixture\n\ngo 1.26.5\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				for _, target := range []string{"build", "build-linux", "install"} {
					cmd := goInstallCommand(t, dir, runtime.GOOS)
					cmd.Args[3] = target
					gopath := filepath.Join(dir, "gopath")
					cmd.Env = append(cmd.Env, "GOPATH="+gopath, "GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOENV=off",
						"GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("make %s: %v\n%s", target, err, out)
					}
					binary := filepath.Join(dir, "fixture")
					switch target {
					case "build-linux":
						binary = filepath.Join(dir, "fixture-linux")
					case "install":
						binary = filepath.Join(gopath, "bin", "fixture")
					}
					if _, err := buildinfo.ReadFile(binary); err != nil {
						t.Fatalf("make %s did not produce a Go binary: %v", target, err)
					}
				}
			})
		}
	}
}
