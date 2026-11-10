package file

import (
	"bytes"
	"testing"
	"text/template"

	"sigs.k8s.io/yaml"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

// The upstream helm-docs hook is a script that runs the binary from PATH.
// A rev alone therefore does not pin the generator used locally.
func Test_PreCommitConfigHelmDocsPinsExecutable(t *testing.T) {
	in := NewCreatePreCommitConfigInput(params.Params{
		Flavors:    []string{helmchartFlavor},
		HelmCharts: []string{"my-app"},
	})
	tpl, err := template.New(in.Path).Parse(in.TemplateBody)
	if err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatal(err)
	}
	var config struct {
		Repos []struct {
			Repo  string `json:"repo"`
			Rev   string `json:"rev"`
			Hooks []struct {
				ID       string   `json:"id"`
				Language string   `json:"language"`
				Entry    string   `json:"entry"`
				Args     []string `json:"args"`
			} `json:"hooks"`
		} `json:"repos"`
	}
	if err := yaml.Unmarshal(rendered.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Env map[string]string `json:"env"`
	}
	if err := yaml.Unmarshal([]byte(renderAction(t, params.Params{Flavors: []string{helmchartFlavor}})), &workflow); err != nil {
		t.Fatal(err)
	}
	wantVersion := workflow.Env["HELM_DOCS_VERSION"]
	if wantVersion == "" {
		t.Fatal("CI workflow does not pin helm-docs")
	}
	for _, repo := range config.Repos {
		if repo.Repo != "https://github.com/norwoodj/helm-docs" {
			continue
		}
		if repo.Rev != "v"+wantVersion {
			t.Errorf("helm-docs revision = %q, want CI version v%s", repo.Rev, wantVersion)
		}
		if len(repo.Hooks) != 1 {
			t.Fatalf("helm-docs hooks = %d, want 1", len(repo.Hooks))
		}
		hook := repo.Hooks[0]
		if hook.ID != "helm-docs" || hook.Language != "golang" || hook.Entry != "helm-docs" {
			t.Errorf("helm-docs must build and execute its pinned revision, got %+v", hook)
		}
		return
	}
	t.Fatal("no helm-docs repository in generated config")
}
