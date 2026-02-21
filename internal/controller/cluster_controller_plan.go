package controller

import (
	"fmt"

	"github.com/gnmic/operator/internal/gnmic"
)

func (r *ClusterReconciler) GetClusterPlan(namespace, name string) (*gnmic.ApplyPlan, error) {
	r.m.RLock()
	defer r.m.RUnlock()

	plan, ok := r.plans[namespace+"/"+name]
	if !ok {
		return nil, fmt.Errorf("plan not found for cluster %s/%s", namespace, name)
	}
	return plan, nil
}

func (r *ClusterReconciler) cleanupPlan(namespace, name string) {
	r.m.Lock()
	defer r.m.Unlock()
	delete(r.plans, namespace+"/"+name)
}
