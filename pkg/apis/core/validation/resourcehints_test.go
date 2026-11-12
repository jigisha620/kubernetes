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

package validation

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	podtest "k8s.io/kubernetes/pkg/api/pod/testing"
	"k8s.io/kubernetes/pkg/apis/core"
)

func TestValidatePodResourceHints(t *testing.T) {
	now := metav1.NewTime(time.Now())
	cpu := func(v string) core.ResourceList { return core.ResourceList{core.ResourceCPU: resource.MustParse(v)} }
	values := func(name, v string) *core.ResourceHintValues {
		return &core.ResourceHintValues{Containers: []core.ContainerResourceHint{{Name: name, Requests: cpu(v)}}}
	}
	valid := core.PodResourceHint{
		Manager:     "autoscaling.k8s.io/vpa-updater",
		UpdateTime:  now,
		Replacement: values("app", "3"),
	}
	with := func(f func(h *core.PodResourceHint)) []core.PodResourceHint {
		h := *valid.DeepCopy()
		f(&h)
		return []core.PodResourceHint{h}
	}
	many := func(n int) []core.PodResourceHint {
		var hs []core.PodResourceHint
		for i := range n {
			h := *valid.DeepCopy()
			h.Manager = fmt.Sprintf("example.com/m%d", i)
			hs = append(hs, h)
		}
		return hs
	}

	cases := map[string]struct {
		hints     []core.PodResourceHint
		podLevel  bool
		wantTypes []field.ErrorType
	}{
		"valid":              {hints: []core.PodResourceHint{valid}},
		"four managers":      {hints: many(4)},
		"too many":           {hints: many(5), wantTypes: []field.ErrorType{field.ErrorTypeTooMany}},
		"duplicate manager":  {hints: append(many(1), many(1)...), wantTypes: []field.ErrorType{field.ErrorTypeDuplicate}},
		"missing manager":    {hints: with(func(h *core.PodResourceHint) { h.Manager = "" }), wantTypes: []field.ErrorType{field.ErrorTypeRequired}},
		"unprefixed manager": {hints: with(func(h *core.PodResourceHint) { h.Manager = "vpa" }), wantTypes: []field.ErrorType{field.ErrorTypeInvalid}},
		"missing updateTime": {hints: with(func(h *core.PodResourceHint) { h.UpdateTime = metav1.Time{} }), wantTypes: []field.ErrorType{field.ErrorTypeRequired}},
		"future updateTime":  {hints: with(func(h *core.PodResourceHint) { h.UpdateTime = metav1.NewTime(time.Now().Add(time.Hour)) }), wantTypes: []field.ErrorType{field.ErrorTypeInvalid}},
		"unknown container":  {hints: with(func(h *core.PodResourceHint) { h.Replacement = values("nope", "1") }), wantTypes: []field.ErrorType{field.ErrorTypeNotFound}},
		"negative quantity":  {hints: with(func(h *core.PodResourceHint) { h.Replacement = values("app", "-1") }), wantTypes: []field.ErrorType{field.ErrorTypeInvalid}},
		"bad resource name": {hints: with(func(h *core.PodResourceHint) {
			h.Replacement.Containers[0].Requests = core.ResourceList{"bogus": resource.MustParse("1")}
		}), wantTypes: []field.ErrorType{field.ErrorTypeInvalid}},
		"podRequests without plr": {hints: with(func(h *core.PodResourceHint) { h.Replacement = &core.ResourceHintValues{PodRequests: cpu("1")} }), wantTypes: []field.ErrorType{field.ErrorTypeForbidden}},
		"podRequests with plr":    {hints: with(func(h *core.PodResourceHint) { h.Replacement = &core.ResourceHintValues{PodRequests: cpu("1")} }), podLevel: true},
		"init container":          {hints: with(func(h *core.PodResourceHint) { h.Replacement = values("init", "100m") })},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pod := podtest.MakePod("p",
				podtest.SetContainers(podtest.MakeContainer("app")),
				podtest.SetInitContainers(podtest.MakeContainer("init")),
			)
			if tc.podLevel {
				pod.Spec.Resources = &core.ResourceRequirements{Requests: cpu("1")}
			}
			pod.ResourceVersion = "1"
			old := pod.DeepCopy()
			pod.Status.ResourceHints = tc.hints
			errs := ValidatePodResourceHintsUpdate(pod, old)
			if len(errs) != len(tc.wantTypes) {
				t.Fatalf("got errors %v, want types %v", errs, tc.wantTypes)
			}
			for i, e := range errs {
				if e.Type != tc.wantTypes[i] {
					t.Errorf("error %d: got %v, want %v (%v)", i, e.Type, tc.wantTypes[i], e)
				}
			}
		})
	}
}
