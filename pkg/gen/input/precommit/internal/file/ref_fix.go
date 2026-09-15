package file

import (
	"bytes"
	"encoding/json"

	"github.com/giantswarm/microerror"
)

// refFixGo is the in-process Go port of refFixPython (see precommit.go): for every JSON
// object that has both `$ref` and `additionalProperties: false` it drops
// `additionalProperties` and sets `unevaluatedProperties: false` instead
// (losisin/helm-values-schema-json#317).
//
// Key order is not preserved (decoding into map[string]any loses it), but that is fine:
// `schemalint normalize`, the pipeline's last step, re-serializes the whole document into
// its own canonical order regardless of what this step produces, so only structural
// equivalence with refFixPython's output -- not byte-for-byte equivalence -- matters here.
// See Test_RefFixGo_MatchesPython, which checks exactly that against Test_RefFixPython's
// fixture.
func refFixGo(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // preserve numeric formatting instead of round-tripping through float64

	var doc interface{}
	if err := dec.Decode(&doc); err != nil {
		return nil, microerror.Mask(err)
	}

	out, err := json.Marshal(refFixWalk(doc))
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return out, nil
}

// refFixWalk recursively applies the fix to every object in the tree. The check is local to
// each object's own keys, so traversal order doesn't matter.
func refFixWalk(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, child := range t {
			t[k] = refFixWalk(child)
		}
		if _, hasRef := t["$ref"]; hasRef {
			// Python's `o.get("additionalProperties") is False` matches only the
			// literal boolean false, not e.g. a missing key or non-bool value.
			if ap, ok := t["additionalProperties"].(bool); ok && !ap {
				delete(t, "additionalProperties")
				t["unevaluatedProperties"] = false
			}
		}
		return t
	case []interface{}:
		for i, child := range t {
			t[i] = refFixWalk(child)
		}
		return t
	default:
		return v
	}
}
