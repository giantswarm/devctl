package rolloutwait

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Helm's annotations on every object of a release.
const (
	annotationReleaseName      = "meta.helm.sh/release-name"
	annotationReleaseNamespace = "meta.helm.sh/release-namespace"
)

// judgement is one workload's rollout: ready, what it still waits for, and
// whether it failed.
type judgement struct {
	ready   bool
	message string
	failed  bool
}

var workloadKinds = []struct {
	kind     string
	resource schema.GroupVersionResource
	judge    func(u *unstructured.Unstructured) judgement
}{
	{"Deployment", deployments, judgeDeployment},
	{"StatefulSet", statefulSets, judgeStatefulSet},
	{"DaemonSet", daemonSets, judgeDaemonSet},
}

// workloads reads the Deployments, StatefulSets and DaemonSets of the Helm
// release in namespace and judges each rollout the way kubectl rollout
// status does. failed is the message of a Deployment past its progress
// deadline, empty otherwise.
func (w *Waiter) workloads(ctx context.Context, namespace, release string) (list []Workload, failed string, err error) {
	list = []Workload{}
	for _, k := range workloadKinds {
		items, err := w.list(ctx, k.resource, namespace)
		if err != nil {
			return nil, "", err
		}
		for i := range items {
			u := &items[i]
			a := u.GetAnnotations()
			if a[annotationReleaseName] != release || a[annotationReleaseNamespace] != namespace {
				continue
			}
			j := k.judge(u)
			list = append(list, Workload{Kind: k.kind, Namespace: u.GetNamespace(), Name: u.GetName(), Ready: j.ready, Message: j.message})
			if j.failed && failed == "" {
				failed = fmt.Sprintf("%s %s/%s: %s", k.kind, u.GetNamespace(), u.GetName(), j.message)
			}
		}
	}
	return list, failed, nil
}

// pending is the first workload that is not ready, as "<kind> <ns>/<name>:
// <message>"; empty when all are.
func pending(list []Workload) string {
	for _, wl := range list {
		if !wl.Ready {
			return fmt.Sprintf("%s %s/%s: %s", wl.Kind, wl.Namespace, wl.Name, wl.Message)
		}
	}
	return ""
}

func num(u *unstructured.Unstructured, fields ...string) int64 {
	n, _, _ := unstructured.NestedInt64(u.Object, fields...)
	return n
}

func str(u *unstructured.Unstructured, fields ...string) string {
	s, _, _ := unstructured.NestedString(u.Object, fields...)
	return s
}

// replicas is spec.replicas, 1 when unset like the API server defaults it.
func replicas(u *unstructured.Unstructured) int64 {
	n, found, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
	if !found {
		return 1
	}
	return n
}

func observed(u *unstructured.Unstructured) (judgement, bool) {
	if num(u, "status", "observedGeneration") < u.GetGeneration() {
		return judgement{message: "waiting for the controller to observe the new generation"}, false
	}
	return judgement{}, true
}

func judgeDeployment(u *unstructured.Unstructured) judgement {
	if j, ok := observed(u); !ok {
		return j
	}
	if c := condition(u, "Progressing"); c.reason == "ProgressDeadlineExceeded" {
		return judgement{message: "progress deadline exceeded: " + c.message, failed: true}
	}
	want, updated := replicas(u), num(u, "status", "updatedReplicas")
	switch {
	case updated < want:
		return judgement{message: fmt.Sprintf("%d of %d replicas updated", updated, want)}
	case num(u, "status", "replicas") > updated:
		return judgement{message: fmt.Sprintf("%d old replicas pending termination", num(u, "status", "replicas")-updated)}
	case num(u, "status", "availableReplicas") < updated:
		return judgement{message: fmt.Sprintf("%d of %d updated replicas available", num(u, "status", "availableReplicas"), updated)}
	}
	return judgement{ready: true}
}

func judgeStatefulSet(u *unstructured.Unstructured) judgement {
	if str(u, "spec", "updateStrategy", "type") == "OnDelete" {
		return judgement{ready: true, message: "OnDelete update strategy: pods update when deleted"}
	}
	if j, ok := observed(u); !ok {
		return j
	}
	want := replicas(u)
	if ready := num(u, "status", "readyReplicas"); ready < want {
		return judgement{message: fmt.Sprintf("%d of %d replicas ready", ready, want)}
	}
	if partition := num(u, "spec", "updateStrategy", "rollingUpdate", "partition"); partition > 0 {
		if updated := num(u, "status", "updatedReplicas"); updated < want-partition {
			return judgement{message: fmt.Sprintf("%d of %d replicas above the partition updated", updated, want-partition)}
		}
		return judgement{ready: true}
	}
	if str(u, "status", "updateRevision") != str(u, "status", "currentRevision") {
		return judgement{message: fmt.Sprintf("%d of %d replicas at revision %s", num(u, "status", "updatedReplicas"), want, str(u, "status", "updateRevision"))}
	}
	return judgement{ready: true}
}

func judgeDaemonSet(u *unstructured.Unstructured) judgement {
	if str(u, "spec", "updateStrategy", "type") == "OnDelete" {
		return judgement{ready: true, message: "OnDelete update strategy: pods update when deleted"}
	}
	if j, ok := observed(u); !ok {
		return j
	}
	want := num(u, "status", "desiredNumberScheduled")
	if updated := num(u, "status", "updatedNumberScheduled"); updated < want {
		return judgement{message: fmt.Sprintf("%d of %d pods updated", updated, want)}
	}
	if available := num(u, "status", "numberAvailable"); available < want {
		return judgement{message: fmt.Sprintf("%d of %d updated pods available", available, want)}
	}
	return judgement{ready: true}
}

// conditionTrue is the status of a condition that holds.
const conditionTrue = "True"

// cond is one status condition.
type cond struct {
	found                   bool
	status, reason, message string
}

// condition reads the status condition of type t.
func condition(u *unstructured.Unstructured, t string) cond {
	list, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok || m["type"] != t {
			continue
		}
		s := func(k string) string { v, _ := m[k].(string); return v }
		return cond{found: true, status: s("status"), reason: s("reason"), message: s("message")}
	}
	return cond{}
}
