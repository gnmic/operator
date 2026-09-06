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
	"fmt"
	"maps"
	"os"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
)

// reconcileTunnelService creates/updates the gRPC tunnel service for the cluster
func (r *ClusterReconciler) reconcileTunnelService(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	if cluster.Spec.GRPCTunnel == nil {
		return nil // No tunnel configured
	}

	serviceName := fmt.Sprintf("%s%s-grpc-tunnel", resourcePrefix, cluster.Name)

	labels := map[string]string{
		"app.kubernetes.io/name":       LabelValueName,
		"app.kubernetes.io/managed-by": LabelValueManagedBy,
		LabelClusterName:               cluster.Name,
		LabelServiceType:               LabelValueServiceTypeTunnel,
	}
	annotations := map[string]string{}

	// default to LoadBalancer if not specified
	serviceType := corev1.ServiceTypeLoadBalancer

	if cluster.Spec.GRPCTunnel.Service != nil {
		if cluster.Spec.GRPCTunnel.Service.Type != "" {
			serviceType = cluster.Spec.GRPCTunnel.Service.Type
		}
		if len(cluster.Spec.GRPCTunnel.Service.Labels) > 0 {
			maps.Copy(labels, cluster.Spec.GRPCTunnel.Service.Labels)
		}
		if len(cluster.Spec.GRPCTunnel.Service.Annotations) > 0 {
			maps.Copy(annotations, cluster.Spec.GRPCTunnel.Service.Annotations)
		}
	}
	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Selector: map[string]string{
				LabelClusterName: cluster.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "tunnel",
					Port:       cluster.Spec.GRPCTunnel.Port,
					TargetPort: intstr.FromInt32(cluster.Spec.GRPCTunnel.Port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	var current corev1.Service
	err := r.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: cluster.Namespace}, &current)
	if apierrors.IsNotFound(err) {
		logger.Info("creating tunnel service", "service", serviceName)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// check if service needs update
	if current.Spec.Type != desired.Spec.Type ||
		len(current.Spec.Ports) != len(desired.Spec.Ports) ||
		current.Spec.Ports[0].Port != desired.Spec.Ports[0].Port ||
		!maps.Equal(current.Spec.Selector, desired.Spec.Selector) {
		current.Spec = desired.Spec
		logger.Info("updating tunnel service", "service", serviceName)
		return r.Update(ctx, &current)
	}

	return nil
}

// cleanupTunnelService deletes the tunnel service for a cluster
func (r *ClusterReconciler) cleanupTunnelService(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	serviceName := fmt.Sprintf("%s%s-grpc-tunnel", resourcePrefix, cluster.Name)
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: cluster.Namespace,
		},
	}

	err := r.Delete(ctx, service)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	logger.Info("deleted tunnel service", "service", serviceName)
	return nil
}

// reconcileControllerCA syncs the controller's CA certificate to the cluster's namespace
// this allows gNMIc pods to verify the controller's client certificate during mTLS
func (r *ClusterReconciler) reconcileControllerCA(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	// read the controller's CA certificate
	caPath := gnmic.GetControllerCAPath()
	caData, err := os.ReadFile(caPath)
	if err != nil {
		// if the CA file doesn't exist, skip (controller TLS not configured)
		if os.IsNotExist(err) {
			logger.Info("controller CA not found, skipping CA sync", "path", caPath)
			return nil
		}
		return fmt.Errorf("failed to read controller CA: %w", err)
	}

	cmName := resourcePrefix + cluster.Name + controllerCACMSfx
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       LabelValueName,
				"app.kubernetes.io/managed-by": LabelValueManagedBy,
				LabelClusterName:               cluster.Name,
			},
		},
		Data: map[string]string{
			"ca.crt": string(caData),
		},
	}

	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	var current corev1.ConfigMap
	err = r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cluster.Namespace}, &current)
	if apierrors.IsNotFound(err) {
		logger.Info("creating controller CA configmap", "configmap", cmName)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// update only if CA data changed
	if current.Data["ca.crt"] != string(caData) {
		logger.Info("updating controller CA configmap", "configmap", cmName)
		current.Data = desired.Data
		return r.Update(ctx, &current)
	}

	return nil
}

// cleanupControllerCA removes the controller CA configmap when the cluster is deleted
func (r *ClusterReconciler) cleanupControllerCA(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	cmName := resourcePrefix + cluster.Name + controllerCACMSfx
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cluster.Namespace,
		},
	}
	return client.IgnoreNotFound(r.Delete(ctx, cm))
}

func (r *ClusterReconciler) reconcileHeadlessService(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	desired := r.buildHeadlessService(cluster)

	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	var current corev1.Service
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, &current)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// update ports only if changed
	if !servicePortsEqual(current.Spec.Ports, desired.Spec.Ports) {
		current.Spec.Ports = desired.Spec.Ports
		return r.Update(ctx, &current)
	}
	return nil
}

func (r *ClusterReconciler) buildHeadlessService(cluster *gnmicv1alpha1.Cluster) *corev1.Service {
	labels := map[string]string{
		"app.kubernetes.io/name":       LabelValueName,
		"app.kubernetes.io/managed-by": LabelValueManagedBy,
		LabelClusterName:               cluster.Name,
		LabelServiceType:               LabelValueServiceTypeHeadless,
	}
	restPort := int32(defaultRestPort)
	if cluster.Spec.API != nil && cluster.Spec.API.RestPort != 0 {
		restPort = cluster.Spec.API.RestPort
	}

	ports := []corev1.ServicePort{
		{
			Name:     "rest",
			Port:     restPort,
			Protocol: corev1.ProtocolTCP,
		},
	}
	if cluster.Spec.API != nil && cluster.Spec.API.GNMIPort != 0 {
		ports = append(ports, corev1.ServicePort{
			Name:     "gnmi",
			Port:     cluster.Spec.API.GNMIPort,
			Protocol: corev1.ProtocolTCP,
		})
	}

	svcName := fmt.Sprintf("%s%s", resourcePrefix, cluster.Name)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector: map[string]string{
				LabelClusterName: cluster.Name,
			},
			Ports: ports,
		},
	}
}
