package controller

import (
	"fmt"
	"sync"

	"github.com/gnmic/operator/internal/gnmic"
)

// NewClusterReconcilerForTest returns a ClusterReconciler with an initialized plan cache.
// It is intended for unit tests of components that read cached plans (e.g. the API server).
func NewClusterReconcilerForTest() *ClusterReconciler {
	return &ClusterReconciler{
		m:     &sync.RWMutex{},
		plans: make(map[string]*gnmic.ApplyPlan),
	}
}

// CachePlan stores an apply plan in the reconciler's in-memory cache.
func (r *ClusterReconciler) CachePlan(namespace, name string, plan *gnmic.ApplyPlan) {
	r.m.Lock()
	defer r.m.Unlock()
	r.plans[namespace+"/"+name] = plan
}

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
