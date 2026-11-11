package file

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
	"text/template"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/apptest/internal/params"
)

// TestBasicSuiteFrameworkAPI checks the generated Go source, which is embedded
// text and therefore isn't compiled when devctl itself is built. The offline
// fixture exposes the signatures from apptest-framework v1.16.1, the latest
// release on the module path that gen apptest's go get -u updates:
// https://github.com/giantswarm/apptest-framework/blob/5d775c2bd50b2d26ffb6ec82d74fbc0f8334a7da/pkg/suite/suite.go
// https://github.com/giantswarm/apptest-framework/blob/5d775c2bd50b2d26ffb6ec82d74fbc0f8334a7da/pkg/config/config.go
// No framework code is downloaded or executed, so this cannot create clusters.
func TestBasicSuiteFrameworkAPI(t *testing.T) {
	imports := frameworkAPI{}
	check := func(path, source string) *types.Package {
		t.Helper()
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path+".go", source, parser.AllErrors)
		if err != nil {
			t.Fatal(err)
		}
		config := types.Config{Importer: imports}
		pkg, err := config.Check(path, fset, []*ast.File{file}, nil)
		if err != nil {
			t.Fatalf("type-check %s: %v", path, err)
		}
		return pkg
	}
	imports["testing"] = check("testing", "package testing; type T struct{}")
	imports["github.com/giantswarm/apptest-framework/pkg/config"] = check(
		"github.com/giantswarm/apptest-framework/pkg/config", `package config
type TestConfig struct{}
func MustLoad() TestConfig { return TestConfig{} }
`)
	imports["github.com/giantswarm/apptest-framework/pkg/suite"] = check(
		"github.com/giantswarm/apptest-framework/pkg/suite", `package suite
import "testing"
type suite struct{}
func New() *suite { return nil }
func (s *suite) WithInstallNamespace(string) *suite { return s }
func (s *suite) WithIsUpgrade(bool) *suite { return s }
func (s *suite) WithValuesFile(string) *suite { return s }
func (s *suite) AfterClusterReady(func()) *suite { return s }
func (s *suite) BeforeUpgrade(func()) *suite { return s }
func (s *suite) Tests(func()) *suite { return s }
func (s *suite) Run(*testing.T, string) {}
`)

	in := NewCreateBasicSuiteTestInput(params.Params{Dir: "tests/e2e"})
	tmpl, err := template.New("basic-suite").Parse(in.TemplateBody)
	if err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatal(err)
	}
	check("basic", rendered.String())
}

type frameworkAPI map[string]*types.Package

func (i frameworkAPI) Import(path string) (*types.Package, error) {
	pkg, ok := i[path]
	if !ok {
		return nil, fmt.Errorf("unexpected generated-suite import %q", path)
	}
	return pkg, nil
}
