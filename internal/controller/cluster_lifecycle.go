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
	"context"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *ClusterReconciler) ensureStatefulSetAbsent(ctx context.Context, nn types.NamespacedName) error {
	var sts appsv1.StatefulSet
	if err := r.Get(ctx, nn, &sts); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return r.Delete(ctx, &sts)
}

func (r *ClusterReconciler) ensureServiceAbsent(ctx context.Context, nn types.NamespacedName) error {
	var service corev1.Service
	if err := r.Get(ctx, nn, &service); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return r.Delete(ctx, &service)
}

// fetchCluster loads the Cluster named by req. When it is already gone the child
// resources that outlive it are removed here, since no finalizer will run for it.
// found is false whenever the caller should stop, error or not.
func (r *ClusterReconciler) fetchCluster(ctx context.Context, req ctrl.Request) (*gnmicv1alpha1.Cluster, bool, error) {
	var cluster gnmicv1alpha1.Cluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.cleanupClusterResources(ctx, req.Namespace, req.Name, nil); err != nil {
				return nil, false, err
			}
			r.cleanupPlan(req.Namespace, req.Name)
		}
		return nil, false, client.IgnoreNotFound(err)
	}
	return &cluster, true, nil
}

// finalizeCluster handles a Cluster carrying a deletion timestamp: remove everything
// the operator created for it, then drop the finalizer so the API server can delete it.
func (r *ClusterReconciler) finalizeCluster(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (ctrl.Result, error) {
	r.cleanupPlan(cluster.Namespace, cluster.Name)
	if !controllerutil.ContainsFinalizer(cluster, clusterFinalizer) {
		return ctrl.Result{}, nil
	}
	if err := r.cleanupClusterResources(ctx, cluster.Namespace, cluster.Name, cluster); err != nil {
		return ctrl.Result{}, err
	}
	controllerutil.RemoveFinalizer(cluster, clusterFinalizer)
	if err := r.Update(ctx, cluster); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// ensureFinalizer adds the cluster finalizer when it is missing. done is true when the
// object was written and the caller must return: reconciles trigger on generation
// change, so the next pass has to run against the updated object.
func (r *ClusterReconciler) ensureFinalizer(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (ctrl.Result, bool, error) {
	if controllerutil.ContainsFinalizer(cluster, clusterFinalizer) {
		return ctrl.Result{}, false, nil
	}
	controllerutil.AddFinalizer(cluster, clusterFinalizer)
	if err := r.Update(ctx, cluster); err != nil {
		return ctrl.Result{}, true, err
	}
	return ctrl.Result{Requeue: true}, true, nil
}

// cleanupClusterResources deletes the StatefulSet, headless Service and Prometheus
// output Services created for the named cluster. When the Cluster object is still
// available its TLS certificates, controller CA copy and tunnel Service go too; on the
// not-found path there is no spec to consult, so those are skipped.
func (r *ClusterReconciler) cleanupClusterResources(ctx context.Context, namespace, name string, cluster *gnmicv1alpha1.Cluster) error {
	nn := types.NamespacedName{Name: resourcePrefix + name, Namespace: namespace}
	if err := r.ensureStatefulSetAbsent(ctx, nn); err != nil {
		return err
	}
	if err := r.ensureServiceAbsent(ctx, nn); err != nil {
		return err
	}
	if err := r.cleanupPrometheusServices(ctx, namespace, name); err != nil {
		return err
	}
	if cluster == nil {
		return nil
	}
	// certificates are only owned by the operator when cert-manager issues them;
	// with the CSI driver there is nothing of ours to remove.
	if certManagerIssued(apiTLS(cluster)) {
		if err := r.cleanupCertificates(ctx, cluster); err != nil {
			return err
		}
	}
	if tls := apiTLS(cluster); tls != nil && tls.IssuerRef != "" {
		if err := r.cleanupControllerCA(ctx, cluster); err != nil {
			return err
		}
	}
	if certManagerIssued(tunnelTLS(cluster)) {
		if err := r.cleanupTunnelCertificates(ctx, cluster); err != nil {
			return err
		}
	}
	if certManagerIssued(cluster.Spec.ClientTLS) {
		if err := r.cleanupClientTLSCertificates(ctx, cluster); err != nil {
			return err
		}
	}
	if cluster.Spec.GRPCTunnel != nil {
		if err := r.cleanupTunnelService(ctx, cluster); err != nil {
			return err
		}
	}
	return nil
}
