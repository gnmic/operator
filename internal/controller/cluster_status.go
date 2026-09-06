/*
Copyright 2025.

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

package controller

import (
	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// clusterStatusEqual compares two ClusterStatus structs for equality
func clusterStatusEqual(a, b gnmicv1alpha1.ClusterStatus) bool {
	if a.ReadyReplicas != b.ReadyReplicas ||
		a.PipelinesCount != b.PipelinesCount ||
		a.TargetsCount != b.TargetsCount ||
		a.UnassignedTargets != b.UnassignedTargets ||
		a.SubscriptionsCount != b.SubscriptionsCount ||
		a.InputsCount != b.InputsCount ||
		a.OutputsCount != b.OutputsCount {
		return false
	}
	if len(a.Conditions) != len(b.Conditions) {
		return false
	}
	for i := range a.Conditions {
		if a.Conditions[i].Type != b.Conditions[i].Type ||
			a.Conditions[i].Status != b.Conditions[i].Status ||
			a.Conditions[i].Reason != b.Conditions[i].Reason ||
			a.Conditions[i].Message != b.Conditions[i].Message {
			return false
		}
	}
	return true
}
