package makefile

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"text/template"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// Use a real executable without compiling a second Go program. An open stdin
// keeps cat running while make replaces its on-disk binary.
func TestGoBuildReplacesRunningBinary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux executable inode semantics")
	}
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Fatal(err)
	}
	oldBinary, err := os.ReadFile(cat) // #nosec G304 -- the local cat executable
	if err != nil {
		t.Fatal(err)
	}
	newBinary := append(bytes.Clone(oldBinary), []byte("new binary\n")...)
	checkedIn, err := os.ReadFile("../../../../Makefile.gen.go.mk")
	if err != nil {
		t.Fatal(err)
	}
	makefiles := []struct {
		name string
		body []byte
	}{
		{name: "case 0: CLI", body: renderGoBinaryMakefile(t, gen.FlavourCLI)},
		{name: "case 1: generic", body: renderGoBinaryMakefile(t, gen.FlavourGeneric)},
		{name: "case 2: checked in", body: checkedIn},
	}
	targets := []struct {
		name   string
		goal   string
		target string
		suffix string
	}{
		{name: "case 0: build", goal: "build", target: "fixture", suffix: "linux-amd64"},
		{name: "case 1: build-darwin", goal: "build-darwin", target: "fixture-darwin", suffix: "darwin-amd64"},
		{name: "case 2: build-darwin-arm64", goal: "build-darwin-arm64", target: "fixture-darwin-arm64", suffix: "darwin-arm64"},
		{name: "case 3: build-linux", goal: "build-linux", target: "fixture-linux", suffix: "linux-amd64"},
		{name: "case 4: build-linux-arm64", goal: "build-linux-arm64", target: "fixture-linux-arm64", suffix: "linux-arm64"},
		{name: "case 5: build-windows-amd64", goal: "build-windows-amd64", target: "fixture-windows-amd64.exe", suffix: "windows-amd64.exe"},
		{name: "case 6: build-docker", goal: "build-docker", target: "fixture", suffix: "linux-amd64"},
	}
	for _, mf := range makefiles {
		t.Run(mf.name, func(t *testing.T) {
			for _, tc := range targets {
				t.Run(tc.name, func(t *testing.T) {
					dir := newGoBinaryFixture(t, mf.body)
					target := filepath.Join(dir, tc.target)
					source := filepath.Join(dir, "fixture-v1.2.3-"+tc.suffix)
					writeGoBinaryFixture(t, target, oldBinary)
					writeGoBinaryFixture(t, source, newBinary)
					stamp := time.Unix(1700000000, 0)
					if err := os.Chtimes(target, stamp.Add(-time.Hour), stamp.Add(-time.Hour)); err != nil {
						t.Fatal(err)
					}
					if err := os.Chtimes(source, stamp, stamp); err != nil {
						t.Fatal(err)
					}
					previous, err := os.Stat(target)
					if err != nil {
						t.Fatal(err)
					}
					// #nosec G204 -- an isolated copy of cat, with no arguments
					process := exec.CommandContext(t.Context(), target)
					stdin, err := process.StdinPipe()
					if err != nil {
						t.Fatal(err)
					}
					defer stdin.Close()
					var echoed bytes.Buffer
					process.Stdout = &echoed
					if err := process.Start(); err != nil {
						t.Fatal(err)
					}
					defer func() {
						if process.ProcessState == nil {
							_ = process.Process.Kill()
							_ = process.Wait()
						}
					}()

					out, err := runGoBinaryMake(t, dir, tc.goal)
					if err != nil {
						t.Fatalf("make with running binary: %v\n%s", err, out)
					}
					assertGoBinaryFile(t, target, newBinary)
					assertGoBinaryFile(t, source, newBinary)
					installed, err := os.Stat(target)
					if err != nil {
						t.Fatal(err)
					}
					if os.SameFile(previous, installed) || installed.Mode().Perm() != 0o751 || !installed.ModTime().Equal(stamp) {
						t.Errorf("replacement must have a new inode and retain source mode/timestamp: %v", installed)
					}
					if _, err := stdin.Write([]byte("still running\n")); err != nil {
						t.Fatal(err)
					}
					if err := stdin.Close(); err != nil {
						t.Fatal(err)
					}
					if err := process.Wait(); err != nil || echoed.String() != "still running\n" {
						t.Errorf("previous process: error = %v, output = %q", err, echoed.String())
					}
					assertGoBinaryTempsRemoved(t, dir, tc.target)
				})
			}
		})
	}
}

func TestGoBuildAtomicPublication(t *testing.T) {
	const oldBinary = "previous complete binary\n"
	const newBinary = "new complete binary\n"
	testCases := []struct {
		name   string
		absent bool
		fail   string
	}{
		{name: "case 0: replace"},
		{name: "case 1: first install", absent: true},
		{name: "case 2: partial copy fails", fail: "cp"},
		{name: "case 3: first copy fails", absent: true, fail: "cp"},
		{name: "case 4: rename fails", fail: "mv"},
		{name: "case 5: temporary file creation fails", fail: "mktemp"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newGoBinaryFixture(t, renderGoBinaryMakefile(t, gen.FlavourCLI))
			writeGoBinaryFixture(t, filepath.Join(dir, "fixture-v1.2.3-linux-amd64"), []byte(newBinary))
			writeGoBinaryFixture(t, filepath.Join(dir, "expected-old"), []byte(oldBinary))
			if !tc.absent {
				writeGoBinaryFixture(t, filepath.Join(dir, "fixture"), []byte(oldBinary))
				stamp := time.Unix(1700000000, 0)
				if err := os.Chtimes(filepath.Join(dir, "fixture"), stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
			// Observe the public path in the middle of the copy, deterministically.
			copyScript := "#!/bin/sh\nset -e\nprintf partial > \"$3\"\n"
			if tc.absent {
				copyScript += "test ! -e fixture\n"
			} else {
				copyScript += "cmp -s fixture expected-old\n"
			}
			if tc.fail == "cp" {
				copyScript += "exit 23\n"
			} else {
				copyScript += "exec real-cp \"$@\"\n"
			}
			// Move the fixture's tool link aside before writing the stub.
			if err := os.Rename(filepath.Join(dir, "bin", "cp"), filepath.Join(dir, "bin", "real-cp")); err != nil {
				t.Fatal(err)
			}
			writeGoBinaryFixture(t, filepath.Join(dir, "bin", "cp"), []byte(copyScript))
			if tc.fail == "mv" || tc.fail == "mktemp" {
				tool := filepath.Join(dir, "bin", tc.fail)
				if err := os.Rename(tool, tool+".real"); err != nil {
					t.Fatal(err)
				}
				writeGoBinaryFixture(t, tool, []byte("#!/bin/sh\nexit 24\n"))
			}
			out, err := runGoBinaryMake(t, dir, "build")
			if (err != nil) != (tc.fail != "") {
				t.Errorf("make build: error = %v, failure injection = %q\n%s", err, tc.fail, out)
			}
			if tc.fail == "" {
				assertGoBinaryFile(t, filepath.Join(dir, "fixture"), []byte(newBinary))
			} else if tc.absent {
				if _, err := os.Stat(filepath.Join(dir, "fixture")); !os.IsNotExist(err) {
					t.Errorf("failed first install exposed a target: %v", err)
				}
			} else {
				assertGoBinaryFile(t, filepath.Join(dir, "fixture"), []byte(oldBinary))
			}
			assertGoBinaryFile(t, filepath.Join(dir, "fixture-v1.2.3-linux-amd64"), []byte(newBinary))
			assertGoBinaryTempsRemoved(t, dir, "fixture")
		})
	}
}

func renderGoBinaryMakefile(t *testing.T, flavour gen.Flavour) []byte {
	t.Helper()
	m, err := New(Config{Flavours: gen.FlavourSlice{flavour}})
	if err != nil {
		t.Fatal(err)
	}
	in := m.MakefileGenGo()[0]
	tpl, err := template.New("makefile").Parse(in.TemplateBody)
	if err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatal(err)
	}
	return rendered.Bytes()
}

func newGoBinaryFixture(t *testing.T, body []byte) string {
	t.Helper()
	dir := t.TempDir()
	writeGoBinaryFixture(t, filepath.Join(dir, "Makefile.gen.go.mk"), body)
	if err := os.Mkdir(filepath.Join(dir, "bin"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cp", "mv", "mktemp", "rm", "cmp"} {
		tool, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(tool, filepath.Join(dir, "bin", name)); err != nil {
			t.Fatal(err)
		}
	}
	// := shell expressions are evaluated even with command-line overrides.
	// Stub metadata tools and refuse any unexpected compilation or Docker call.
	for _, name := range []string{"cut", "date", "git", "find", "gitsemver"} {
		writeGoBinaryFixture(t, filepath.Join(dir, "bin", name), []byte("#!/bin/sh\n"))
	}
	writeGoBinaryFixture(t, filepath.Join(dir, "bin", "go"), []byte("#!/bin/sh\ncase \"$1\" in list|env) exit 0;; *) exit 99;; esac\n"))
	writeGoBinaryFixture(t, filepath.Join(dir, "bin", "docker"), []byte("#!/bin/sh\ntest \"$*\" = 'build -t fixture:1.2.3 .'\n"))
	return dir
}

func runGoBinaryMake(t *testing.T, dir, target string) ([]byte, error) {
	t.Helper()
	makeBin, err := exec.LookPath("make")
	if err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- make and its arguments are controlled offline fixtures
	cmd := exec.CommandContext(t.Context(), makeBin, "-f", "Makefile.gen.go.mk", target,
		"APPLICATION=fixture", "MODULE=example.invalid/fixture", "VERSION=1.2.3", "OS=linux",
		"SOURCES=", "BUILDTIMESTAMP=unused", "GITSHA1=unused", "RACE=")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+filepath.Join(dir, "bin"))
	return cmd.CombinedOutput()
}

func writeGoBinaryFixture(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.WriteFile(path, body, 0o751); err != nil { // #nosec G306 -- executable test fixtures under t.TempDir()
		t.Fatal(err)
	}
}

func assertGoBinaryFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path) // #nosec G304 -- a fixed fixture path under t.TempDir()
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("%s does not contain the expected complete binary: %v", filepath.Base(path), err)
	}
}

func assertGoBinaryTempsRemoved(t *testing.T, dir, target string) {
	t.Helper()
	leftovers, err := filepath.Glob(filepath.Join(dir, target+".tmp.*"))
	if err != nil || len(leftovers) != 0 {
		t.Errorf("temporary files left after install: %v, error = %v", leftovers, err)
	}
}
