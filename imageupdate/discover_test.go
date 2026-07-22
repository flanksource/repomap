package imageupdate

import "testing"

const deploymentNoNS = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
spec:
  template:
    spec:
      containers:
        - name: app
          image: nginx:1.0.0
`

const deploymentPinnedNS = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: other
spec:
  template:
    spec:
      containers:
        - name: app
          image: nginx:1.0.0
`

// imageTarget returns the single TargetImage in targets, failing if there is not
// exactly one.
func imageTarget(t *testing.T, targets []UpdateTarget) UpdateTarget {
	t.Helper()
	var found []UpdateTarget
	for _, tg := range targets {
		if tg.Kind == TargetImage {
			found = append(found, tg)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one image target, got %d: %+v", len(found), targets)
	}
	return found[0]
}

func TestDiscoverTargets_KustomizationNamespaceApplied(t *testing.T) {
	contents := map[string]string{
		"kenya/kustomization.yaml": "namespace: hf-qa-kenya\nresources:\n  - app.yaml\n",
		"kenya/app.yaml":           deploymentNoNS,
	}
	res := DiscoverTargets(contents, "kenya/")
	if ns := imageTarget(t, res.Targets).Ref.Namespace; ns != "hf-qa-kenya" {
		t.Errorf("target namespace = %q, want hf-qa-kenya (imposed by kustomization)", ns)
	}
}

func TestDiscoverTargets_KustomizationNamespaceOverridesPinned(t *testing.T) {
	contents := map[string]string{
		"kenya/kustomization.yaml": "namespace: hf-qa-kenya\nresources:\n  - app.yaml\n",
		"kenya/app.yaml":           deploymentPinnedNS,
	}
	res := DiscoverTargets(contents, "kenya/")
	if ns := imageTarget(t, res.Targets).Ref.Namespace; ns != "hf-qa-kenya" {
		t.Errorf("target namespace = %q, want hf-qa-kenya (kustomization wins over metadata.namespace)", ns)
	}
}
