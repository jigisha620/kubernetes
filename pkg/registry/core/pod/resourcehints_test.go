/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pod

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	podtest "k8s.io/kubernetes/pkg/api/pod/testing"
	api "k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/kubernetes/pkg/features"
)

func hintedPod(cpu string) *api.Pod {
	pod := podtest.MakePod("p", podtest.SetContainers(podtest.MakeContainer("app")))
	pod.ResourceVersion = "1"
	if cpu != "" {
		pod.Status.ResourceHints = []api.PodResourceHint{{
			Manager:    "example.com/test",
			UpdateTime: metav1.NewTime(time.Now()),
			Replacement: &api.ResourceHintValues{Containers: []api.ContainerResourceHint{{
				Name:     "app",
				Requests: api.ResourceList{api.ResourceCPU: resource.MustParse(cpu)},
			}}},
		}}
	}
	return pod
}

func TestResourceHintsStrategyOnlyChangesHints(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.PodResourceHints, true)
	ctx := genericapirequest.NewDefaultContext()

	oldPod := hintedPod("")
	oldPod.Status.Phase = api.PodRunning
	newPod := hintedPod("3")
	newPod.Labels = map[string]string{"changed": "true"}
	newPod.Spec.Containers[0].Image = "other"
	newPod.Status.Phase = api.PodFailed

	ResourceHintsStrategy.PrepareForUpdate(ctx, newPod, oldPod)

	if diff := cmp.Diff(oldPod.Spec, newPod.Spec); diff != "" {
		t.Errorf("spec changed (-old +new):\n%s", diff)
	}
	if newPod.Status.Phase != api.PodRunning {
		t.Errorf("phase changed to %s", newPod.Status.Phase)
	}
	if newPod.Labels["changed"] == "true" {
		t.Errorf("labels changed through resourcehints")
	}
	if len(newPod.Status.ResourceHints) != 1 {
		t.Fatalf("hints not applied: %v", newPod.Status.ResourceHints)
	}
	if errs := ResourceHintsStrategy.ValidateUpdate(ctx, newPod, oldPod); len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}

func TestStatusAndResizeStrategiesPreserveHints(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.PodResourceHints, true)
	ctx := genericapirequest.NewDefaultContext()

	for name, prepare := range map[string]func(obj, old *api.Pod){
		"status": func(obj, old *api.Pod) { StatusStrategy.PrepareForUpdate(ctx, obj, old) },
		"resize": func(obj, old *api.Pod) { ResizeStrategy.PrepareForUpdate(ctx, obj, old) },
		"pod":    func(obj, old *api.Pod) { Strategy.PrepareForUpdate(ctx, obj, old) },
	} {
		t.Run(name, func(t *testing.T) {
			oldPod := hintedPod("3")
			newPod := hintedPod("")
			newPod.Status.Phase = api.PodRunning
			prepare(newPod, oldPod)
			if diff := cmp.Diff(oldPod.Status.ResourceHints, newPod.Status.ResourceHints); diff != "" {
				t.Errorf("hints not preserved (-old +new):\n%s", diff)
			}
		})
	}
}

func TestResourceHintsDroppedWhenGateDisabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.PodResourceHints, false)
	ctx := genericapirequest.NewDefaultContext()

	oldPod := hintedPod("")
	newPod := hintedPod("3")
	ResourceHintsStrategy.PrepareForUpdate(ctx, newPod, oldPod)
	if len(newPod.Status.ResourceHints) != 0 {
		t.Errorf("expected hints dropped with gate off, got %v", newPod.Status.ResourceHints)
	}

	oldPod = hintedPod("3")
	newPod = hintedPod("2")
	ResourceHintsStrategy.PrepareForUpdate(ctx, newPod, oldPod)
	if len(newPod.Status.ResourceHints) != 1 {
		t.Errorf("expected hints kept when already in use, got %v", newPod.Status.ResourceHints)
	}
}
