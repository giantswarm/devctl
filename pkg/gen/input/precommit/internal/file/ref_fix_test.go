package file

import (
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"testing"
)

// refFixTestFixture is Test_RefFixPython's input, shared here so both implementations run
// against the exact same schema shape.
const refFixTestFixture = `{
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "additionalProperties": false,
    "properties": {
        "affinity": {
            "$ref": "#/$defs/io.k8s.api.core.v1.Affinity",
            "additionalProperties": false,
            "description": "Pod affinity — “typographic” quotes"
        },
        "plain": {
            "additionalProperties": false,
            "properties": {"a": {"type": "string"}},
            "type": "object"
        }
    }
}`

// Test_RefFixGo_MatchesPython proves the Go port (refFixGo) is structurally equivalent to
// the generated hook's Python one-liner (refFixPython) on the same input: decoding both
// outputs must produce identical data, even though their raw bytes differ (indentation,
// key order). That is the correctness bar the pipeline needs, because `schemalint
// normalize` -- the pipeline's last step -- re-serializes canonically regardless of either
// step's output formatting.
func Test_RefFixGo_MatchesPython(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}

	goOut, err := refFixGo([]byte(refFixTestFixture))
	if err != nil {
		t.Fatalf("refFixGo: %v", err)
	}

	path := t.TempDir() + "/values.schema.json"
	if err := os.WriteFile(path, []byte(refFixTestFixture), 0600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	cmd := exec.Command(python, "-c", refFixPython, path) // #nosec G204 -- interpreter from exec.LookPath, script is a package constant, path is a t.TempDir() path, test-only
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("python fix step failed: %v\n%s", err, out)
	}
	pyOut, err := os.ReadFile(path) // #nosec G304 -- t.TempDir() path, test-only
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	var goDecoded, pyDecoded interface{}
	if err := json.Unmarshal(goOut, &goDecoded); err != nil {
		t.Fatalf("go output is not valid JSON: %v\n%s", err, goOut)
	}
	if err := json.Unmarshal(pyOut, &pyDecoded); err != nil {
		t.Fatalf("python output is not valid JSON: %v\n%s", err, pyOut)
	}

	if !reflect.DeepEqual(goDecoded, pyDecoded) {
		t.Errorf("refFixGo and refFixPython produced different structures:\ngo:     %s\npython: %s", goOut, pyOut)
	}
}
