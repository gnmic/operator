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
	"slices"
	"strconv"
	"strings"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
)

// reconcileCertificates creates/updates cert-manager Certificate resources for each pod
// returns true if all certificates are ready, false otherwise
func (r *ClusterReconciler) reconcileCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (bool, error) {
	logger := log.FromContext(ctx)

	if cluster.Spec.API == nil || cluster.Spec.API.TLS == nil || cluster.Spec.API.TLS.IssuerRef == "" {
		return true, nil // TLS not configured, skip
	}

	stsName := fmt.Sprintf("%s%s", resourcePrefix, cluster.Name)
	replicas := *cluster.Spec.Replicas

	allReady := true

	// create/update certificates for each replica
	for i := int32(0); i < replicas; i++ {
		podName := fmt.Sprintf("%s-%d", stsName, i)
		certName := fmt.Sprintf("%s-tls", podName)

		cert := r.buildCertificate(cluster, certName, podName, stsName)

		if err := controllerutil.SetControllerReference(cluster, cert, r.Scheme); err != nil {
			return false, err
		}

		var current certmanagerv1.Certificate
		err := r.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, &current)
		if apierrors.IsNotFound(err) {
			logger.Info("creating certificate", "certificate", certName)
			if err := r.Create(ctx, cert); err != nil {
				return false, err
			}
			allReady = false
			continue
		}
		if err != nil {
			return false, err
		}

		// check if certificate needs update
		if r.certificateNeedsUpdate(&current, cert) {
			current.Spec = cert.Spec
			if err := r.Update(ctx, &current); err != nil {
				return false, err
			}
		}

		// check if certificate is ready
		if !r.isCertificateReady(&current) {
			logger.Info("certificate not ready", "certificate", certName)
			allReady = false
		}
	}

	// clean up certificates for replicas that no longer exist (scale down)
	var certList certmanagerv1.CertificateList
	if err := r.List(ctx, &certList, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		LabelClusterName: cluster.Name,
	}); err != nil {
		return false, err
	}

	for _, cert := range certList.Items {
		// extract the ordinal from the certificate name (for example: "gnmic-cluster1-2-tls" -> 2)
		ordinal := r.extractOrdinalFromCertName(cert.Name, stsName)
		if ordinal >= int(replicas) {
			logger.Info("deleting certificate for scaled-down replica", "certificate", cert.Name)
			if err := r.Delete(ctx, &cert); err != nil && !apierrors.IsNotFound(err) {
				return false, err
			}
		}
	}

	return allReady, nil
}

// buildCertificate creates a cert-manager Certificate spec for a pod
func (r *ClusterReconciler) buildCertificate(cluster *gnmicv1alpha1.Cluster, certName, podName, stsName string) *certmanagerv1.Certificate {
	// build DNS names for the certificate
	// pod DNS: <pod-name>.<service-name>.<namespace>.svc.<cluster-domain>
	dnsNames := []string{
		podName,
		fmt.Sprintf("%s.%s", podName, stsName),
		fmt.Sprintf("%s.%s.%s", podName, stsName, cluster.Namespace),
		fmt.Sprintf("%s.%s.%s.svc", podName, stsName, cluster.Namespace),
		fmt.Sprintf("%s.%s.%s.svc.%s", podName, stsName, cluster.Namespace, gnmic.ClusterDomain()),
	}

	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "gnmic",
				"app.kubernetes.io/managed-by": "gnmic-operator",
				LabelClusterName:               cluster.Name,
			},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: certName,
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: map[string]string{
					LabelClusterName: cluster.Name,
					LabelPodName:     podName,
				},
			},
			IssuerRef: cmmeta.IssuerReference{
				Name: cluster.Spec.API.TLS.IssuerRef,
				Kind: "Issuer", // defaults to Issuer. TODO: configurable to ClusterIssuer ?
			},
			CommonName: podName,
			DNSNames:   dnsNames,
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageServerAuth,
				certmanagerv1.UsageClientAuth,
				certmanagerv1.UsageDigitalSignature,
				certmanagerv1.UsageKeyEncipherment,
			},
		},
	}
}

// certificateNeedsUpdate checks if the certificate spec has changed
func (r *ClusterReconciler) certificateNeedsUpdate(current, desired *certmanagerv1.Certificate) bool {
	if current.Spec.SecretName != desired.Spec.SecretName {
		return true
	}
	if current.Spec.IssuerRef.Name != desired.Spec.IssuerRef.Name {
		return true
	}
	if current.Spec.CommonName != desired.Spec.CommonName {
		return true
	}
	if !slices.Equal(current.Spec.DNSNames, desired.Spec.DNSNames) {
		return true
	}
	return false
}

// isCertificateReady checks if a certificate has the Ready condition set to True
func (r *ClusterReconciler) isCertificateReady(cert *certmanagerv1.Certificate) bool {
	for _, condition := range cert.Status.Conditions {
		if condition.Type == certmanagerv1.CertificateConditionReady {
			return condition.Status == cmmeta.ConditionTrue
		}
	}
	return false
}

// extractOrdinalFromCertName extracts the StatefulSet ordinal from a certificate name
// for example: "gnmic-cluster1-2-tls" with stsName "gnmic-cluster1" returns 2
func (r *ClusterReconciler) extractOrdinalFromCertName(certName, stsName string) int {
	// remove the "-tls" suffix and the stsName prefix
	suffix := strings.TrimPrefix(certName, stsName+"-")
	suffix = strings.TrimSuffix(suffix, "-tls")
	ordinal, err := strconv.Atoi(suffix)
	if err != nil {
		return -1 // invalid
	}
	return ordinal
}

// cleanupCertificates deletes all certificates for a cluster
func (r *ClusterReconciler) cleanupCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	var certList certmanagerv1.CertificateList
	if err := r.List(ctx, &certList, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		LabelClusterName: cluster.Name,
	}); err != nil {
		return client.IgnoreNotFound(err)
	}

	for _, cert := range certList.Items {
		logger.Info("deleting certificate", "certificate", cert.Name)
		if err := r.Delete(ctx, &cert); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// reconcileTunnelCertificates creates/updates cert-manager Certificate resources for tunnel TLS
// returns true if all certificates are ready, false otherwise
func (r *ClusterReconciler) reconcileTunnelCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (bool, error) {
	logger := log.FromContext(ctx)

	if cluster.Spec.GRPCTunnel == nil || cluster.Spec.GRPCTunnel.TLS == nil || cluster.Spec.GRPCTunnel.TLS.IssuerRef == "" {
		return true, nil // tunnel TLS not configured, skip
	}

	stsName := fmt.Sprintf("%s%s", resourcePrefix, cluster.Name)
	replicas := *cluster.Spec.Replicas

	allReady := true

	// create/update certificates for each replica
	for i := int32(0); i < replicas; i++ {
		podName := fmt.Sprintf("%s-%d", stsName, i)
		certName := fmt.Sprintf("%s-tunnel-tls", podName)

		cert := r.buildTunnelCertificate(cluster, certName, podName, stsName)

		if err := controllerutil.SetControllerReference(cluster, cert, r.Scheme); err != nil {
			return false, err
		}

		var current certmanagerv1.Certificate
		err := r.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, &current)
		if apierrors.IsNotFound(err) {
			logger.Info("creating tunnel certificate", "certificate", certName)
			if err := r.Create(ctx, cert); err != nil {
				return false, err
			}
			allReady = false
			continue
		}
		if err != nil {
			return false, err
		}

		// check if certificate needs update
		if r.certificateNeedsUpdate(&current, cert) {
			current.Spec = cert.Spec
			if err := r.Update(ctx, &current); err != nil {
				return false, err
			}
		}

		// check if certificate is ready
		if !r.isCertificateReady(&current) {
			logger.Info("tunnel certificate not ready", "certificate", certName)
			allReady = false
		}
	}

	// clean up certificates for replicas that no longer exist (scale down)
	var certList certmanagerv1.CertificateList
	if err := r.List(ctx, &certList, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		LabelClusterName: cluster.Name,
		LabelCertType:    LabelValueCertTypeTunnel,
	}); err != nil {
		return false, err
	}

	for _, cert := range certList.Items {
		// extract the ordinal from the certificate name (for example: "gnmic-cluster1-2-tunnel-tls" -> 2)
		ordinal := r.extractOrdinalFromTunnelCertName(cert.Name, stsName)
		if ordinal >= int(replicas) {
			logger.Info("deleting tunnel certificate for scaled-down replica", "certificate", cert.Name)
			if err := r.Delete(ctx, &cert); err != nil && !apierrors.IsNotFound(err) {
				return false, err
			}
		}
	}

	return allReady, nil
}

// buildTunnelCertificate creates a cert-manager Certificate spec for tunnel TLS
func (r *ClusterReconciler) buildTunnelCertificate(cluster *gnmicv1alpha1.Cluster, certName, podName, stsName string) *certmanagerv1.Certificate {
	// build DNS names for the certificate
	dnsNames := []string{
		podName,
		fmt.Sprintf("%s.%s", podName, stsName),
		fmt.Sprintf("%s.%s.%s", podName, stsName, cluster.Namespace),
		fmt.Sprintf("%s.%s.%s.svc", podName, stsName, cluster.Namespace),
		fmt.Sprintf("%s.%s.%s.svc.%s", podName, stsName, cluster.Namespace, gnmic.ClusterDomain()),
	}

	// also add the tunnel service DNS names if service is configured
	if cluster.Spec.GRPCTunnel.Service != nil {
		tunnelServiceName := fmt.Sprintf("%s%s-grpc-tunnel", resourcePrefix, cluster.Name)
		dnsNames = append(dnsNames,
			tunnelServiceName,
			fmt.Sprintf("%s.%s", tunnelServiceName, cluster.Namespace),
			fmt.Sprintf("%s.%s.svc", tunnelServiceName, cluster.Namespace),
			fmt.Sprintf("%s.%s.svc.%s", tunnelServiceName, cluster.Namespace, gnmic.ClusterDomain()),
		)
	}

	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       LabelValueName,
				"app.kubernetes.io/managed-by": LabelValueManagedBy,
				LabelClusterName:               cluster.Name,
				LabelCertType:                  LabelValueCertTypeTunnel,
			},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: certName,
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: map[string]string{
					LabelClusterName: cluster.Name,
					LabelPodName:     podName,
					LabelCertType:    LabelValueCertTypeTunnel,
				},
			},
			IssuerRef: cmmeta.IssuerReference{
				Name: cluster.Spec.GRPCTunnel.TLS.IssuerRef,
				Kind: "Issuer",
			},
			CommonName: podName,
			DNSNames:   dnsNames,
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageServerAuth,
				certmanagerv1.UsageClientAuth,
				certmanagerv1.UsageDigitalSignature,
				certmanagerv1.UsageKeyEncipherment,
			},
		},
	}
}

// extractOrdinalFromTunnelCertName extracts the StatefulSet ordinal from a tunnel certificate name
// e.g., "gnmic-cluster1-2-tunnel-tls" with stsName "gnmic-cluster1" returns 2
func (r *ClusterReconciler) extractOrdinalFromTunnelCertName(certName, stsName string) int {
	// remove the "-tunnel-tls" suffix and the stsName prefix
	suffix := strings.TrimPrefix(certName, stsName+"-")
	suffix = strings.TrimSuffix(suffix, "-tunnel-tls")
	ordinal, err := strconv.Atoi(suffix)
	if err != nil {
		return -1 // Invalid
	}
	return ordinal
}

// cleanupTunnelCertificates deletes all tunnel certificates for a cluster
func (r *ClusterReconciler) cleanupTunnelCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	var certList certmanagerv1.CertificateList
	if err := r.List(ctx, &certList, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		LabelClusterName: cluster.Name,
		LabelCertType:    LabelValueCertTypeTunnel,
	}); err != nil {
		return client.IgnoreNotFound(err)
	}

	for _, cert := range certList.Items {
		logger.Info("deleting tunnel certificate", "certificate", cert.Name)
		if err := r.Delete(ctx, &cert); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// reconcileClientTLSCertificates creates/updates a single cert-manager Certificate for client TLS
// (used by gNMIc to connect to targets with mTLS)
// A single certificate is shared by all pods in the cluster for simplicity and to avoid
// volume changes when scaling.
// returns true if the certificate is ready, false otherwise
func (r *ClusterReconciler) reconcileClientTLSCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (bool, error) {
	logger := log.FromContext(ctx)

	if cluster.Spec.ClientTLS == nil || cluster.Spec.ClientTLS.IssuerRef == "" {
		return true, nil // client TLS not needed, skip
	}

	certName := fmt.Sprintf("%s%s-client-tls", resourcePrefix, cluster.Name)
	cert := r.buildClientTLSCertificate(cluster, certName)

	if err := controllerutil.SetControllerReference(cluster, cert, r.Scheme); err != nil {
		return false, err
	}

	var current certmanagerv1.Certificate
	err := r.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, &current)
	if apierrors.IsNotFound(err) {
		logger.Info("creating client TLS certificate", "certificate", certName)
		if err := r.Create(ctx, cert); err != nil {
			return false, err
		}
		return false, nil // certificate just created, not ready yet
	}
	if err != nil {
		return false, err
	}

	// check if certificate needs update
	if r.certificateNeedsUpdate(&current, cert) {
		current.Spec = cert.Spec
		if err := r.Update(ctx, &current); err != nil {
			return false, err
		}
	}

	// check if certificate is ready
	if !r.isCertificateReady(&current) {
		logger.Info("client TLS certificate not ready", "certificate", certName)
		return false, nil
	}

	return true, nil
}

// buildClientTLSCertificate creates a cert-manager Certificate spec for client TLS
// (used by gNMIc to authenticate to targets)
// Uses cluster-name.namespace as CommonName - shared by all pods in the cluster
func (r *ClusterReconciler) buildClientTLSCertificate(cluster *gnmicv1alpha1.Cluster, certName string) *certmanagerv1.Certificate {
	// Use cluster-name.namespace as the common name for the client certificate
	// This certificate is shared by all pods in the cluster
	commonName := fmt.Sprintf("%s.%s", cluster.Name, cluster.Namespace)

	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       LabelValueName,
				"app.kubernetes.io/managed-by": LabelValueManagedBy,
				LabelClusterName:               cluster.Name,
				LabelCertType:                  LabelValueCertTypeClient,
			},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: certName,
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: map[string]string{
					LabelClusterName: cluster.Name,
					LabelCertType:    LabelValueCertTypeClient,
				},
			},
			IssuerRef: cmmeta.IssuerReference{
				Name: cluster.Spec.ClientTLS.IssuerRef,
				Kind: "Issuer",
			},
			CommonName: commonName,
			DNSNames:   []string{commonName},
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageClientAuth,
				certmanagerv1.UsageDigitalSignature,
				certmanagerv1.UsageKeyEncipherment,
			},
		},
	}
}

// cleanupClientTLSCertificates deletes the client TLS certificate for a cluster
func (r *ClusterReconciler) cleanupClientTLSCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	certName := fmt.Sprintf("%s%s-client-tls", resourcePrefix, cluster.Name)
	cert := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
		},
	}

	logger.Info("deleting client TLS certificate", "certificate", certName)
	if err := r.Delete(ctx, cert); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

// cleanupClientTLSCertificatesLegacy deletes any legacy per-pod client TLS certificates
// This is for backwards compatibility when upgrading from per-pod to single certificate
func (r *ClusterReconciler) cleanupClientTLSCertificatesLegacy(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	var certList certmanagerv1.CertificateList
	if err := r.List(ctx, &certList, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		LabelClusterName: cluster.Name,
		LabelCertType:    LabelValueCertTypeClient,
	}); err != nil {
		return client.IgnoreNotFound(err)
	}

	for _, cert := range certList.Items {
		logger.Info("deleting client TLS certificate", "certificate", cert.Name)
		if err := r.Delete(ctx, &cert); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}
