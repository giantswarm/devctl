package makefile

import (
	"bytes"
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// TestGoInstallDisablesCGO exercises the generated recipe with cgo enabled in
// the caller, including its source selection, quoting and build error handling.
func TestGoInstallDisablesCGO(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}

	testCases := []struct {
		name     string
		source   string
		os       string
		flavours gen.FlavourSlice
		buildErr bool
	}{
		{name: "case 0: Linux root source", source: "main.go", os: "linux"},
		{name: "case 1: Linux CLI cmd source", source: "cmd/main.go", os: "linux", flavours: gen.FlavourSlice{gen.FlavourCLI}},
		{name: "case 2: Darwin root source", source: "main.go", os: "darwin"},
		{name: "case 3: build failure", source: "main.go", os: "linux", buildErr: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeGoInstallFixture(t, dir, tc.flavours, tc.source, "package main\nfunc main() {}\n")
			bin := filepath.Join(dir, "tools")
			if err := os.MkdirAll(bin, 0o750); err != nil {
				t.Fatal(err)
			}
			const fakeGo = `#!/bin/sh
case "$1 $2" in
	'env GOPATH') printf '%s\n' "$INSTALL_GOPATH" ;;
	'build -ldflags')
		printf '%s\n' "$CGO_ENABLED" "$@" > "$INSTALL_CALLS"
		exit "$INSTALL_EXIT"
		;;
	*) exit 99 ;;
esac
`
			if err := os.WriteFile(filepath.Join(bin, "go"), []byte(fakeGo), 0o700); err != nil { //nolint:gosec // executable fixture confined to t.TempDir
				t.Fatal(err)
			}
			calls := filepath.Join(dir, "go-calls")
			gopath := filepath.Join(dir, "gopath with spaces")
			exit := "0"
			if tc.buildErr {
				exit = "7"
			}
			cmd := goInstallCommand(t, dir, tc.os)
			cmd.Env = append(cmd.Env,
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"INSTALL_CALLS="+calls, "INSTALL_GOPATH="+gopath, "INSTALL_EXIT="+exit,
			)
			out, err := cmd.CombinedOutput()
			if (err != nil) != tc.buildErr {
				t.Fatalf("make install: %v, want error %t\n%s", err, tc.buildErr, out)
			}
			got, err := os.ReadFile(filepath.Clean(calls))
			if err != nil {
				t.Fatal(err)
			}
			extldflags := ""
			if tc.os == "linux" {
				extldflags = "-static"
			}
			want := []string{
				"0", "build", "-ldflags",
				"-w -linkmode 'auto' -extldflags '" + extldflags + "' -X 'example.com/install-fixture/pkg/project.version=1.2.3' -X 'example.com/install-fixture/pkg/project.buildTimestamp=fixed' -X 'example.com/install-fixture/pkg/project.gitSHA=abc'",
				"-o", filepath.Join(gopath, "bin", "fixture"), tc.source,
			}
			if diff := cmp.Diff(want, strings.Split(strings.TrimSuffix(string(got), "\n"), "\n")); diff != "" {
				t.Errorf("build environment and arguments (-want +got):\n%s", diff)
			}
		})
	}
}

// TestGoInstallBinary checks the built artifact, not just the recipe: the
// installed binary must contain no cgo and resolve a local name without GODEBUG.
func TestGoInstallBinary(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the generated Makefile requires a POSIX shell")
	}
	dir := t.TempDir()
	writeGoInstallFixture(t, dir, gen.FlavourSlice{gen.FlavourCLI}, "cmd/main.go", `package main
import (
	"context"
	"net"
	"os"
	"time"
)
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupHost(ctx, "localhost")
	if err != nil {
		panic(err)
	}
	for _, address := range addresses {
		if net.ParseIP(address).IsLoopback() {
			return
		}
	}
	os.Exit(1)
}
`)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/install-fixture\n\ngo 1.26.5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gopath := filepath.Join(dir, "gopath with spaces")
	cmd := goInstallCommand(t, dir, runtime.GOOS)
	cmd.Env = append(cmd.Env, "GOPATH="+gopath, "GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOENV=off",
		"GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make install: %v\n%s", err, out)
	}
	binary := filepath.Join(gopath, "bin", "fixture")
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	cgo := "missing"
	for _, setting := range info.Settings {
		if setting.Key == "CGO_ENABLED" {
			cgo = setting.Value
		}
	}
	if cgo != "0" {
		t.Fatalf("installed binary CGO_ENABLED = %q, want 0", cgo)
	}
	lookup := exec.CommandContext(t.Context(), binary) //nolint:gosec // the fixture just built in t.TempDir
	lookup.Env = append(os.Environ(), "GODEBUG=")
	if out, err := lookup.CombinedOutput(); err != nil {
		t.Fatalf("installed binary hostname lookup: %v\n%s", err, out)
	}
}

func writeGoInstallFixture(t *testing.T, dir string, flavours gen.FlavourSlice, source, body string) {
	t.Helper()
	m, err := New(Config{Flavours: flavours})
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
	if err := os.WriteFile(filepath.Join(dir, in.Path), rendered.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, source)), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, source), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func goInstallCommand(t *testing.T, dir, targetOS string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "make", "-f", "Makefile.gen.go.mk", "install",
		"APPLICATION=fixture", "MODULE=example.com/install-fixture", "VERSION=1.2.3",
		"BUILDTIMESTAMP=fixed", "GITSHA1=abc", "OS="+targetOS)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1", "MAKEFLAGS=")
	return cmd
}
