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
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
)

// reconcileCertificates creates/updates the cert-manager Certificate the
// cluster's pods present on the REST and gNMI ports, and reports whether it is
// ready.
//
// One certificate per cluster, with wildcard DNS names, rather than one per
// pod. Per-pod certificates put the replica count into the pod template -- one
// Secret projection per ordinal -- so every scale operation rolled every pod,
// and a new ordinal had to wait for its own issuance before it could start. A
// server certificate gains nothing from a per-pod key: the operator verifies
// the pod's hostname, and the wildcard matches every pod under the headless
// Service. Client TLS already worked this way.
func (r *ClusterReconciler) reconcileCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (bool, error) {
	if cluster.Spec.API == nil || cluster.Spec.API.TLS == nil || cluster.Spec.API.TLS.IssuerRef == "" {
		return true, nil // TLS not configured, skip
	}
	return r.ensureCertificate(ctx, cluster, r.buildCertificate(cluster))
}

// ensureCertificate creates or updates one Certificate and reports whether it
// is Ready.
func (r *ClusterReconciler) ensureCertificate(ctx context.Context, cluster *gnmicv1alpha1.Cluster, cert *certmanagerv1.Certificate) (bool, error) {
	logger := log.FromContext(ctx)

	if err := controllerutil.SetControllerReference(cluster, cert, r.Scheme); err != nil {
		return false, err
	}
	var current certmanagerv1.Certificate
	err := r.Get(ctx, client.ObjectKeyFromObject(cert), &current)
	if apierrors.IsNotFound(err) {
		logger.Info("creating certificate", "certificate", cert.Name)
		if err := r.Create(ctx, cert); err != nil {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if r.certificateNeedsUpdate(&current, cert) {
		current.Spec = cert.Spec
		if err := r.Update(ctx, &current); err != nil {
			return false, err
		}
	}
	if !r.isCertificateReady(&current) {
		logger.Info("certificate not ready", "certificate", cert.Name)
		return false, nil
	}
	return true, nil
}

// apiCertificateName is the Certificate (and Secret) for the REST/gNMI server
// certificate. Distinct from the per-pod names an earlier operator used
// (<sts>-<ordinal>-tls), which cleanupLegacyPodCertificates removes.
func apiCertificateName(cluster *gnmicv1alpha1.Cluster) string {
	return resourcePrefix + cluster.Name + "-api-tls"
}

// tunnelCertificateName is the Certificate (and Secret) for the tunnel server.
func tunnelCertificateName(cluster *gnmicv1alpha1.Cluster) string {
	return resourcePrefix + cluster.Name + "-tunnel-tls"
}

// serverDNSNames are the names a cluster-wide server certificate must cover:
// every pod under the headless Service, through a wildcard, and the Service
// name itself, which resolves to an arbitrary pod. Bare pod hostnames are not
// included -- a wildcard cannot express them, and the operator dials the fully
// qualified form.
func serverDNSNames(stsName, namespace string) []string {
	domain := gnmic.ClusterDomain()
	return []string{
		"*." + stsName,
		fmt.Sprintf("*.%s.%s", stsName, namespace),
		fmt.Sprintf("*.%s.%s.svc", stsName, namespace),
		fmt.Sprintf("*.%s.%s.svc.%s", stsName, namespace, domain),
		stsName,
		fmt.Sprintf("%s.%s", stsName, namespace),
		fmt.Sprintf("%s.%s.svc", stsName, namespace),
		fmt.Sprintf("%s.%s.svc.%s", stsName, namespace, domain),
	}
}

// buildCertificate is the cert-manager Certificate spec for the cluster's
// REST/gNMI server certificate.
func (r *ClusterReconciler) buildCertificate(cluster *gnmicv1alpha1.Cluster) *certmanagerv1.Certificate {
	stsName := resourcePrefix + cluster.Name
	name := apiCertificateName(cluster)
	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       LabelValueName,
				"app.kubernetes.io/managed-by": LabelValueManagedBy,
				LabelClusterName:               cluster.Name,
				LabelCertType:                  LabelValueCertTypeAPI,
			},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: name,
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: map[string]string{
					LabelClusterName: cluster.Name,
					LabelCertType:    LabelValueCertTypeAPI,
				},
			},
			IssuerRef: cmmeta.IssuerReference{
				Name: cluster.Spec.API.TLS.IssuerRef,
				Kind: "Issuer", // defaults to Issuer. TODO: configurable to ClusterIssuer ?
			},
			CommonName: stsName,
			DNSNames:   serverDNSNames(stsName, cluster.Namespace),
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

// extractOrdinalFromCertName extracts the StatefulSet ordinal from a legacy per-pod
// certificate name; -1 for anything else (including the cluster-wide certificates).
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

// reconcileTunnelCertificates creates/updates the cert-manager Certificate the
// cluster's pods present on the gRPC tunnel port, and reports whether it is
// ready. One certificate per cluster; see reconcileCertificates.
func (r *ClusterReconciler) reconcileTunnelCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (bool, error) {
	if cluster.Spec.GRPCTunnel == nil || cluster.Spec.GRPCTunnel.TLS == nil || cluster.Spec.GRPCTunnel.TLS.IssuerRef == "" {
		return true, nil // tunnel TLS not configured, skip
	}
	return r.ensureCertificate(ctx, cluster, r.buildTunnelCertificate(cluster))
}

// buildTunnelCertificate is the cert-manager Certificate spec for the cluster's
// tunnel server certificate. Devices dial the tunnel Service, so its names are
// included alongside the pod wildcard.
func (r *ClusterReconciler) buildTunnelCertificate(cluster *gnmicv1alpha1.Cluster) *certmanagerv1.Certificate {
	stsName := resourcePrefix + cluster.Name
	name := tunnelCertificateName(cluster)
	tunnelServiceName := fmt.Sprintf("%s%s-grpc-tunnel", resourcePrefix, cluster.Name)
	dnsNames := append(serverDNSNames(stsName, cluster.Namespace),
		tunnelServiceName,
		fmt.Sprintf("%s.%s", tunnelServiceName, cluster.Namespace),
		fmt.Sprintf("%s.%s.svc", tunnelServiceName, cluster.Namespace),
		fmt.Sprintf("%s.%s.svc.%s", tunnelServiceName, cluster.Namespace, gnmic.ClusterDomain()),
	)

	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       LabelValueName,
				"app.kubernetes.io/managed-by": LabelValueManagedBy,
				LabelClusterName:               cluster.Name,
				LabelCertType:                  LabelValueCertTypeTunnel,
			},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: name,
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: map[string]string{
					LabelClusterName: cluster.Name,
					LabelCertType:    LabelValueCertTypeTunnel,
				},
			},
			IssuerRef: cmmeta.IssuerReference{
				Name: cluster.Spec.GRPCTunnel.TLS.IssuerRef,
				Kind: "Issuer",
			},
			CommonName: stsName,
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

// statefulSetRolledOut reports whether every pod of the StatefulSet exists, is
// Ready, and runs the current template revision -- i.e. no rollout is in
// flight or pending.
func statefulSetRolledOut(sts *appsv1.StatefulSet) bool {
	if sts == nil {
		return false
	}
	want := ptr.Deref(sts.Spec.Replicas, 1)
	st := sts.Status
	return st.ObservedGeneration >= sts.Generation &&
		st.Replicas == want && st.ReadyReplicas == want &&
		st.CurrentRevision != "" && st.CurrentRevision == st.UpdateRevision
}

// cleanupLegacyPodCertificates removes the per-pod Certificates an earlier
// operator issued (<sts>-<ordinal>-tls, <sts>-<ordinal>-tunnel-tls), once
// every pod runs a template that no longer mounts them. Before that, an old
// pod still projects their Secrets and cert-manager would stop renewing files
// that pod depends on. The Certificates are told apart from the cluster-wide
// ones by their ordinal names.
//
// cert-manager does not delete a Certificate's Secret with it, and Secrets are
// read-only for this operator (see the RBAC markers), so the legacy Secrets
// stay behind, labelled with the pod name they served. The documentation gives
// the one-line command to remove them.
func (r *ClusterReconciler) cleanupLegacyPodCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)
	stsName := resourcePrefix + cluster.Name

	var certs certmanagerv1.CertificateList
	if err := r.List(ctx, &certs, client.InNamespace(cluster.Namespace), client.MatchingLabels{LabelClusterName: cluster.Name}); err != nil {
		return err
	}
	for i := range certs.Items {
		c := &certs.Items[i]
		if r.extractOrdinalFromCertName(c.Name, stsName) < 0 && r.extractOrdinalFromTunnelCertName(c.Name, stsName) < 0 {
			continue
		}
		logger.Info("deleting legacy per-pod certificate; its secret is left for manual removal",
			"certificate", c.Name, "secret", c.Spec.SecretName)
		if err := r.Delete(ctx, c); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
