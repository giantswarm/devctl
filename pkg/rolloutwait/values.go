package rolloutwait

import (
	"context"
	"crypto/sha1" // #nosec G505 -- compatibility with Flux's legacy values checksum, not a security primitive.
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	goyaml "go.yaml.in/yaml/v4"
	"helm.sh/helm/v4/pkg/strvals"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// staleValuesFailure compares the inputs of a failed attempt with the values
// helm-controller would read now. A valuesFrom update does not increment the
// HelmRelease's generation. The digest covers the merged values, not the raw
// YAML or the ConfigMap's metadata (which can change without new values).
func (w *Waiter) staleValuesFailure(ctx context.Context, d Deployment, hr *unstructured.Unstructured) (Deployment, bool, error) {
	attempted := str(hr, "status", "lastAttemptedConfigDigest")
	legacy := attempted == ""
	if legacy {
		attempted = str(hr, "status", "lastAttemptedValuesChecksum")
	}
	// Older objects may not expose an input digest. Preserve their existing
	// generation-based judgment rather than assuming every failure is stale.
	if attempted == "" {
		return d, false, nil
	}
	values, err := w.helmValues(ctx, hr)
	if err != nil {
		return d, false, err
	}
	var data []byte
	if len(values) != 0 {
		if legacy {
			data, err = yaml.Marshal(values)
		} else {
			// Preserve the JSON-to-YAML scalar conversion used by Flux, then
			// order mapping nodes lexicographically at every depth. v4 no
			// longer supports the v2 MapSlice representation. Flux's v2
			// encoder folds long scalars at 80 columns, so the digest must too.
			data, err = yaml.Marshal(values)
			if err == nil {
				var ordered goyaml.Node
				err = goyaml.Unmarshal(data, &ordered)
				if err == nil {
					sortValues(&ordered)
					data, err = goyaml.Dump(&ordered, goyaml.WithV2Defaults(), goyaml.WithCompactSeqIndent(), goyaml.WithLineWidth(80))
				}
			}
		}
		if err != nil {
			return d, false, fmt.Errorf("%s: cannot encode values for comparison", d.id())
		}
	}
	current := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	if legacy {
		current = fmt.Sprintf("%x", sha1.Sum(data)) // #nosec G401 -- Flux's legacy checksum uses SHA1.
	}
	if current == attempted {
		return d, false, nil
	}
	self := object{resource: helmReleases, kind: KindHelmRelease, namespace: hr.GetNamespace(), name: hr.GetName()}
	d.self = &self
	return d.set(StateProgressing, "runs %s; values have changed since the last attempt; waiting for helm-controller (its interval, or --reconcile)", orNone(d.RunningVersion)), true, nil
}

// helmValues follows Flux's ChartValuesFromReferences precedence: references
// in order, inline values last, with targetPath applied over inline values.
// Parse errors deliberately omit the data and parser error, which can contain
// a Secret's contents. Reads use the caller's existing cluster identity.
func (w *Waiter) helmValues(ctx context.Context, hr *unstructured.Unstructured) (map[string]any, error) {
	inline, _, _ := unstructured.NestedMap(hr.Object, "spec", "values")
	values := map[string]any{}
	refs, _, _ := unstructured.NestedSlice(hr.Object, "spec", "valuesFrom")
	resources := map[string]*unstructured.Unstructured{}
	for _, item := range refs {
		ref, _ := item.(map[string]any)
		kind, _ := ref["kind"].(string)
		name, _ := ref["name"].(string)
		key, _ := ref["valuesKey"].(string)
		if key == "" {
			key = "values.yaml"
		}
		optional, _ := ref["optional"].(bool)
		resource := configMaps
		if kind == kindSecret {
			resource = secrets
		} else if kind != KindConfigMap {
			return nil, fmt.Errorf("%s: unsupported valuesFrom kind %q", hrID(hr), kind)
		}
		o := object{resource: resource, kind: kind, namespace: hr.GetNamespace(), name: name}
		u, cached := resources[o.String()]
		if !cached {
			var err error
			u, err = w.client.Resource(resource).Namespace(o.namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return nil, w.clusterErr("reading "+o.String(), err)
			}
			resources[o.String()] = u
		}
		var data string
		found := false
		if u != nil {
			data, found, _ = unstructured.NestedString(u.Object, "data", key)
		}
		if !found {
			if optional {
				continue
			}
			return nil, fmt.Errorf("%s: cannot compare values: %s or its key %q is missing", hrID(hr), o, key)
		}
		if kind == kindSecret {
			decoded, err := base64.StdEncoding.DecodeString(data)
			if err != nil {
				return nil, fmt.Errorf("%s: cannot decode values from %s", hrID(hr), o)
			}
			data = string(decoded)
		}
		if target, _ := ref["targetPath"].(string); target != "" {
			values = mergeValues(values, inline)
			parse := strvals.ParseInto
			if literal, _ := ref["literal"].(bool); literal {
				data = strings.NewReplacer(`\`, `\\`, `,`, `\,`, `[`, `\[`, `]`, `\]`, `{`, `\{`, `}`, `\}`).Replace(data)
				parse = strvals.ParseIntoString
			} else if (strings.HasPrefix(data, "'") && strings.HasSuffix(data, "'")) || (strings.HasPrefix(data, `"`) && strings.HasSuffix(data, `"`)) {
				data = strings.Trim(data, "'\"")
				parse = strvals.ParseIntoString
			}
			if err := parse(target+"="+data, values); err != nil {
				return nil, fmt.Errorf("%s: cannot parse targetPath values from %s", hrID(hr), o)
			}
			continue
		}
		var parsed map[string]any
		if err := yaml.Unmarshal([]byte(data), &parsed); err != nil {
			return nil, fmt.Errorf("%s: cannot parse values from %s", hrID(hr), o)
		}
		values = mergeValues(values, parsed)
	}
	return mergeValues(values, inline), nil
}

func mergeValues(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if nested, ok := v.(map[string]any); ok {
			if previous, ok := out[k].(map[string]any); ok {
				v = mergeValues(previous, nested)
			}
		}
		out[k] = v
	}
	return out
}

// sortValues preserves scalar tags and styles while replacing the encoder's
// natural key ordering with Flux's lexicographic ordering.
func sortValues(node *goyaml.Node) {
	if node.Kind == goyaml.MappingNode {
		pairs := make([][2]*goyaml.Node, len(node.Content)/2)
		for i := range pairs {
			pairs[i] = [2]*goyaml.Node{node.Content[2*i], node.Content[2*i+1]}
		}
		sort.Slice(pairs, func(i, j int) bool { return pairs[i][0].Value < pairs[j][0].Value })
		for i, pair := range pairs {
			node.Content[2*i], node.Content[2*i+1] = pair[0], pair[1]
		}
	}
	for _, child := range node.Content {
		sortValues(child)
	}
}
