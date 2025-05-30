package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/release-operator/v4/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestMarshalReleaseYAML_GoldenFile tests that the YAML marshalling of a release matches the golden file.
func TestMarshalReleaseYAML_GoldenFile(t *testing.T) {
	testTime := time.Date(2025, 5, 28, 15, 52, 8, 0, time.UTC)
	testRelease := v1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name: "aws-31.0.0-test",
		},
		Spec: v1alpha1.ReleaseSpec{
			Date:  &metav1.Time{Time: testTime},
			State: "active",
			Apps: []v1alpha1.ReleaseSpecApp{
				{
					Name:    "aws-ebs-csi-driver",
					Version: "3.0.5",
					DependsOn: []string{
						"cloud-provider-aws",
					},
				},
				{
					Name:    "aws-nth-bundle",
					Version: "1.2.1",
				},
				{
					Name:    "cert-manager",
					Version: "3.9.1",
					DependsOn: []string{
						"prometheus-operator-crd",
					},
				},
				{
					Name:    "cilium-crossplane-resources",
					Catalog: "cluster",
					Version: "0.2.1",
				},
				{
					Name:    "network-policies",
					Catalog: "cluster",
					Version: "0.1.1",
					DependsOn: []string{
						"cilium",
					},
				},
				{
					Name:    "security-bundle",
					Catalog: "giantswarm",
					Version: "1.10.1",
					DependsOn: []string{
						"prometheus-operator-crd",
					},
				},
				{
					Name:    "teleport-kube-agent",
					Version: "0.10.5",
				},
			},
			Components: []v1alpha1.ReleaseSpecComponent{
				{
					Name:    "cluster-aws",
					Catalog: "cluster",
					Version: "3.2.2",
				},
				{
					Name:    "flatcar",
					Version: "4152.2.3",
				},
				{
					Name:    "kubernetes",
					Version: "1.33.1",
				},
				{
					Name:    "os-tooling",
					Version: "1.26.1",
				},
			},
		},
	}

	actualYAML, err := marshalReleaseYAML(testRelease)
	if err != nil {
		t.Fatalf("marshalReleaseYAML failed: %v", err)
	}

	goldenPath := filepath.Join("testdata", "golden-release.yaml")
	expectedYAML, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to read golden file %s: %v", goldenPath, err)
	}

	if string(actualYAML) != string(expectedYAML) {
		t.Errorf("Generated YAML does not match golden file.\nExpected:\n%s\nActual:\n%s", string(expectedYAML), string(actualYAML))

		// Write the actual output for debugging if it doesn't match the golden file
		debugPath := filepath.Join("testdata", "actual-output.yaml")
		if writeErr := os.WriteFile(debugPath, actualYAML, 0644); writeErr == nil {
			t.Logf("Actual output written to %s for debugging", debugPath)
		}
	}
}

// TestMarshalReleaseYAML_FieldOrdering tests that fields are in the correct order.
func TestMarshalReleaseYAML_FieldOrdering(t *testing.T) {
	testRelease := v1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-release",
		},
		Spec: v1alpha1.ReleaseSpec{
			Date:  &metav1.Time{Time: time.Now()},
			State: "active",
			Apps: []v1alpha1.ReleaseSpecApp{
				{
					Name:    "test-app",
					Catalog: "test-catalog",
					Version: "1.0.0",
					DependsOn: []string{
						"dependency-1",
						"dependency-2",
					},
				},
			},
		},
	}

	actualYAML, err := marshalReleaseYAML(testRelease)
	if err != nil {
		t.Fatalf("marshalReleaseYAML failed: %v", err)
	}

	yamlStr := string(actualYAML)

	namePos := strings.Index(yamlStr, "name: test-app")
	catalogPos := strings.Index(yamlStr, "catalog: test-catalog")
	versionPos := strings.Index(yamlStr, "version: 1.0.0")
	dependsOnPos := strings.Index(yamlStr, "dependsOn:")

	if namePos == -1 || catalogPos == -1 || versionPos == -1 || dependsOnPos == -1 {
		t.Fatal("Could not find expected fields in YAML output")
	}

	// Verify ordering: name < catalog < version < dependsOn
	if !(namePos < catalogPos && catalogPos < versionPos && versionPos < dependsOnPos) {
		t.Errorf("Fields are not in the correct order. Expected: name < catalog < version < dependsOn")
		t.Logf("Positions - name: %d, catalog: %d, version: %d, dependsOn: %d", namePos, catalogPos, versionPos, dependsOnPos)
		t.Logf("Generated YAML:\n%s", yamlStr)
	}
}

// TestMarshalReleaseYAML_NoStatus tests that the status field is not included in the output.
func TestMarshalReleaseYAML_NoStatus(t *testing.T) {
	testRelease := v1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-release",
		},
		Spec: v1alpha1.ReleaseSpec{
			Date:  &metav1.Time{Time: time.Now()},
			State: "active",
		},
	}

	actualYAML, err := marshalReleaseYAML(testRelease)
	if err != nil {
		t.Fatalf("marshalReleaseYAML failed: %v", err)
	}

	yamlStr := string(actualYAML)

	if strings.Contains(yamlStr, "status:") {
		t.Errorf("Output should not contain 'status:' field")
	}
	if strings.Contains(yamlStr, "creationTimestamp:") {
		t.Errorf("Output should not contain 'creationTimestamp:' field")
	}
}
