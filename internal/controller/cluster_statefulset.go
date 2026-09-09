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

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
)

func (r *ClusterReconciler) reconcileStatefulSet(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (*appsv1.StatefulSet, error) {
	desired, desiredConfigMap, err := r.buildStatefulSet(cluster)
	if err != nil {
		return nil, err
	}

	// reconcile ConfigMap: only update if content changed
	if err := r.reconcileConfigMap(ctx, cluster, desiredConfigMap); err != nil {
		return nil, err
	}

	var current appsv1.StatefulSet
	err = r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, &current)
	if apierrors.IsNotFound(err) {
		if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
			return nil, err
		}
		return desired, r.Create(ctx, desired)
	}
	if err != nil {
		return nil, err
	}

	// update in-place only the fields we manage
	needsUpdate := false

	if desired := desiredReplicas(cluster); ptr.Deref(current.Spec.Replicas, 0) != desired {
		current.Spec.Replicas = ptr.To(desired)
		needsUpdate = true
	}

	if len(current.Spec.Template.Spec.Containers) == 0 {
		current.Spec.Template.Spec.Containers = desired.Spec.Template.Spec.Containers
		needsUpdate = true
	} else {
		container := &current.Spec.Template.Spec.Containers[0]
		if container.Image != cluster.Spec.Image {
			container.Image = cluster.Spec.Image
			needsUpdate = true
		}
		// update resources if changed
		if !resourcesEqual(container.Resources, cluster.Spec.Resources) {
			container.Resources = cluster.Spec.Resources
			needsUpdate = true
		}
	}

	// keep labels in sync for selectors.
	if current.Spec.Template.Labels == nil {
		current.Spec.Template.Labels = map[string]string{}
	}
	for k, v := range desired.Spec.Template.Labels {
		if current.Spec.Template.Labels[k] != v {
			current.Spec.Template.Labels[k] = v
			needsUpdate = true
		}
	}
	if current.Spec.Selector == nil {
		current.Spec.Selector = desired.Spec.Selector
		needsUpdate = true
	}

	// update volumes if they changed (needed when scaling with TLS enabled, or
	// when clientTLS / api.tls / tunnel TLS is added or removed after create)
	// projected volumes include per-pod certificate secrets, so they change when replicas change
	if !volumesEqual(current.Spec.Template.Spec.Volumes, desired.Spec.Template.Spec.Volumes) {
		current.Spec.Template.Spec.Volumes = desired.Spec.Template.Spec.Volumes
		needsUpdate = true
	}

	// volumeMounts (and the Env they depend on, e.g. POD_NAME for subPathExpr)
	// must stay in lockstep with volumes — otherwise enabling clientTLS/api.tls
	// on a live Cluster adds Secret/ConfigMap volumes that are never mounted,
	// or mounts that fail with CreateContainerConfigError.
	if len(desired.Spec.Template.Spec.Containers) > 0 && len(current.Spec.Template.Spec.Containers) > 0 {
		desiredCtr := &desired.Spec.Template.Spec.Containers[0]
		currentCtr := &current.Spec.Template.Spec.Containers[0]
		if !volumeMountsEqual(currentCtr.VolumeMounts, desiredCtr.VolumeMounts) {
			currentCtr.VolumeMounts = desiredCtr.VolumeMounts
			needsUpdate = true
		}
		if !envVarsEqual(currentCtr.Env, desiredCtr.Env) {
			currentCtr.Env = desiredCtr.Env
			needsUpdate = true
		}
	}

	if needsUpdate {
		if err := controllerutil.SetControllerReference(cluster, &current, r.Scheme); err != nil {
			return nil, err
		}
		if err := r.Update(ctx, &current); err != nil {
			return nil, err
		}
	}
	return &current, nil
}

// volumesEqual compares two volume slices for equality
// This is a simplified comparison that checks volume names and projected sources count
func volumesEqual(a, b []corev1.Volume) bool {
	if len(a) != len(b) {
		return false
	}
	aMap := make(map[string]corev1.Volume)
	for _, v := range a {
		aMap[v.Name] = v
	}
	for _, vb := range b {
		va, ok := aMap[vb.Name]
		if !ok {
			return false
		}
		// compare projected volume sources count (this is what changes on scale)
		if va.Projected != nil && vb.Projected != nil {
			if len(va.Projected.Sources) != len(vb.Projected.Sources) {
				return false
			}
		} else if (va.Projected == nil) != (vb.Projected == nil) {
			return false
		}
		// secret / configMap volume identity (clientTLS, CA bundles)
		if (va.Secret == nil) != (vb.Secret == nil) {
			return false
		}
		if va.Secret != nil && vb.Secret != nil && va.Secret.SecretName != vb.Secret.SecretName {
			return false
		}
		if (va.ConfigMap == nil) != (vb.ConfigMap == nil) {
			return false
		}
		if va.ConfigMap != nil && vb.ConfigMap != nil && va.ConfigMap.Name != vb.ConfigMap.Name {
			return false
		}
	}
	return true
}

// volumeMountsEqual compares mount name+path pairs.
func volumeMountsEqual(a, b []corev1.VolumeMount) bool {
	if len(a) != len(b) {
		return false
	}
	type key struct{ name, path, sub string }
	aSet := make(map[key]struct{}, len(a))
	for _, m := range a {
		aSet[key{m.Name, m.MountPath, m.SubPathExpr}] = struct{}{}
	}
	for _, m := range b {
		if _, ok := aSet[key{m.Name, m.MountPath, m.SubPathExpr}]; !ok {
			return false
		}
	}
	return true
}

// envVarsEqual compares env var names (value/valueFrom identity ignored beyond presence).
func envVarsEqual(a, b []corev1.EnvVar) bool {
	if len(a) != len(b) {
		return false
	}
	aNames := make(map[string]struct{}, len(a))
	for _, e := range a {
		aNames[e.Name] = struct{}{}
	}
	for _, e := range b {
		if _, ok := aNames[e.Name]; !ok {
			return false
		}
	}
	return true
}

// reconcileConfigMap creates or updates the ConfigMap only if content changed
func (r *ClusterReconciler) reconcileConfigMap(ctx context.Context, cluster *gnmicv1alpha1.Cluster, desired *corev1.ConfigMap) error {
	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	var current corev1.ConfigMap
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, &current)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// only update if data changed
	if !maps.Equal(current.Data, desired.Data) {
		current.Data = desired.Data
		return r.Update(ctx, &current)
	}
	return nil
}

func (r *ClusterReconciler) buildConfigMap(cluster *gnmicv1alpha1.Cluster) (*corev1.ConfigMap, error) {
	configMapName := fmt.Sprintf("%s%s-config", resourcePrefix, cluster.Name)

	// build base config content
	content, err := r.buildConfigContent(cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to build config content: %w", err)
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: cluster.Namespace,
		},
		Data: map[string]string{
			gNMIcConfigFile: string(content),
		},
	}, nil
}

// buildConfigContent builds the gnmic configuration from the cluster and its pipelines
func (r *ClusterReconciler) buildConfigContent(cluster *gnmicv1alpha1.Cluster) ([]byte, error) {
	restPort := int32(defaultRestPort)
	if cluster.Spec.API != nil && cluster.Spec.API.RestPort != 0 {
		restPort = cluster.Spec.API.RestPort
	}

	tlsConfig := gnmic.TLSConfigForClusterPod(cluster)
	config := map[string]any{
		"api-server": map[string]any{
			"address":        fmt.Sprintf(":%d", restPort),
			"enable-metrics": true,
			"tls":            tlsConfig,
		},
		"log": true,
	}

	// add tunnel-server configuration if configured
	if cluster.Spec.GRPCTunnel != nil && cluster.Spec.GRPCTunnel.Port != 0 {
		tunnelConfig := map[string]any{
			"address":        fmt.Sprintf(":%d", cluster.Spec.GRPCTunnel.Port),
			"enable-metrics": true,
		}

		// add TLS configuration if specified
		tunnelTLSConfig := gnmic.TunnelServerTLSConfig(cluster)
		if tunnelTLSConfig != nil {
			tunnelConfig["tls"] = tunnelTLSConfig
		}

		config["tunnel-server"] = tunnelConfig
	}

	return yaml.Marshal(config)
}

func (r *ClusterReconciler) buildStatefulSet(cluster *gnmicv1alpha1.Cluster) (*appsv1.StatefulSet, *corev1.ConfigMap, error) {
	// build gNMIc pod base configuration
	configMap, err := r.buildConfigMap(cluster)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build config map: %w", err)
	}

	labels := map[string]string{
		"app.kubernetes.io/name":       LabelValueName,
		"app.kubernetes.io/managed-by": LabelValueManagedBy,
		LabelClusterName:               cluster.Name,
	}

	stsName := fmt.Sprintf("%s%s", resourcePrefix, cluster.Name)

	// base volume mounts for gNMIc container
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: gNMIcConfigPath,
			SubPath:   gNMIcConfigFile,
		},
	}

	// base volumes
	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMap.Name,
					},
				},
			},
		},
	}

	var initContainers []corev1.Container

	// environment variables for the container
	// start with user-defined env vars
	envVars := append([]corev1.EnvVar{}, cluster.Spec.Env...)

	// add TLS volumes if TLS is configured using cert-manager
	if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil && cluster.Spec.API.TLS.IssuerRef != "" {
		if cluster.Spec.API.TLS.UseCSIDriver {
			// use cert-manager CSI driver for per-pod certificate
			volumes = append(volumes, corev1.Volume{
				Name: "tls-certs",
				VolumeSource: corev1.VolumeSource{
					CSI: &corev1.CSIVolumeSource{
						Driver:   "csi.cert-manager.io",
						ReadOnly: ptr.To(true),
						VolumeAttributes: map[string]string{
							"csi.cert-manager.io/issuer-name": cluster.Spec.API.TLS.IssuerRef,
							"csi.cert-manager.io/issuer-kind": "Issuer",
							"csi.cert-manager.io/dns-names":   "${POD_NAME}." + stsName + "." + cluster.Namespace + ".svc." + gnmic.ClusterDomain(),
							// "csi.cert-manager.io/renewBefore": "72h", // TODO: make configurable ?
						},
					},
				},
			})
		} else {
			// use projected volume with subPathExpr to mount the correct certificate per pod
			// each pod has a certificate secret named <stsName>-<ordinal>-tls
			// organize by pod name so subPathExpr can select the right one

			certSources := []corev1.VolumeProjection{}
			for i := int32(0); i < desiredReplicas(cluster); i++ {
				podName := fmt.Sprintf("%s-%d", stsName, i)
				secretName := fmt.Sprintf("%s-tls", podName)
				certSources = append(certSources, corev1.VolumeProjection{
					Secret: &corev1.SecretProjection{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: secretName,
						},
						Items: []corev1.KeyToPath{
							{Key: "tls.crt", Path: fmt.Sprintf("%s/tls.crt", podName)},
							{Key: "tls.key", Path: fmt.Sprintf("%s/tls.key", podName)},
							{Key: "ca.crt", Path: fmt.Sprintf("%s/ca.crt", podName)},
						},
						Optional: ptr.To(true),
					},
				})
			}

			volumes = append(volumes, corev1.Volume{
				Name: "tls-certs",
				VolumeSource: corev1.VolumeSource{
					Projected: &corev1.ProjectedVolumeSource{
						Sources: certSources,
					},
				},
			})
		}

		// add TLS volume mount to main container using subPathExpr to select the pod's certificate
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:        "tls-certs",
			MountPath:   gnmic.CertFilesBasePath,
			SubPathExpr: "$(POD_NAME)", // Selects the subdirectory matching the pod name
			ReadOnly:    true,
		})

		// POD_NAME drives the subPathExpr above.
		envVars = ensurePodNameEnv(envVars)
	}

	// add CA bundle volume if specified (for verifying target certificates)
	if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil && cluster.Spec.API.TLS.BundleRef != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "ca-bundle",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cluster.Spec.API.TLS.BundleRef,
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "ca-bundle",
			MountPath: "/etc/certs/ca",
			ReadOnly:  true,
		})
	}

	// add controller CA volume for mTLS client verification
	if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil && cluster.Spec.API.TLS.IssuerRef != "" {
		controllerCACMName := resourcePrefix + cluster.Name + controllerCACMSfx
		volumes = append(volumes, corev1.Volume{
			Name: "controller-ca",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: controllerCACMName,
					},
					Optional: ptr.To(true), // Optional in case controller CA isn't configured
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "controller-ca",
			MountPath: gnmic.ControllerCAMountPath,
			ReadOnly:  true,
		})
	}

	// add tunnel TLS volumes if gRPC tunnel with TLS is configured
	if cluster.Spec.GRPCTunnel != nil && cluster.Spec.GRPCTunnel.TLS != nil {
		// add tunnel TLS certificates if issuerRef is configured
		if cluster.Spec.GRPCTunnel.TLS.IssuerRef != "" {
			if cluster.Spec.GRPCTunnel.TLS.UseCSIDriver {
				// use cert-manager CSI driver for per-pod certificates
				volumes = append(volumes, corev1.Volume{
					Name: "tunnel-tls-certs",
					VolumeSource: corev1.VolumeSource{
						CSI: &corev1.CSIVolumeSource{
							Driver:   "csi.cert-manager.io",
							ReadOnly: ptr.To(true),
							VolumeAttributes: map[string]string{
								"csi.cert-manager.io/issuer-name": cluster.Spec.GRPCTunnel.TLS.IssuerRef,
								"csi.cert-manager.io/issuer-kind": "Issuer",
								"csi.cert-manager.io/dns-names":   "${POD_NAME}." + stsName + "." + cluster.Namespace + ".svc." + gnmic.ClusterDomain(),
							},
						},
					},
				})
			} else {
				// use projected volume with subPathExpr to mount the correct certificate per pod
				tunnelCertSources := []corev1.VolumeProjection{}
				for i := int32(0); i < desiredReplicas(cluster); i++ {
					podName := fmt.Sprintf("%s-%d", stsName, i)
					secretName := fmt.Sprintf("%s-tunnel-tls", podName)
					tunnelCertSources = append(tunnelCertSources, corev1.VolumeProjection{
						Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretName,
							},
							Items: []corev1.KeyToPath{
								{Key: "tls.crt", Path: fmt.Sprintf("%s/tls.crt", podName)},
								{Key: "tls.key", Path: fmt.Sprintf("%s/tls.key", podName)},
								{Key: "ca.crt", Path: fmt.Sprintf("%s/ca.crt", podName)},
							},
							Optional: ptr.To(true),
						},
					})
				}

				volumes = append(volumes, corev1.Volume{
					Name: "tunnel-tls-certs",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							Sources: tunnelCertSources,
						},
					},
				})
			}

			// add tunnel TLS volume mount
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:        "tunnel-tls-certs",
				MountPath:   gnmic.TunnelCertFilesBasePath,
				SubPathExpr: "$(POD_NAME)",
				ReadOnly:    true,
			})

			// POD_NAME may already be there from the API TLS path.
			envVars = ensurePodNameEnv(envVars)
		}

		// add tunnel CA bundle volume if bundleRef is configured for client certificate verification
		if cluster.Spec.GRPCTunnel.TLS.BundleRef != "" {
			volumes = append(volumes, corev1.Volume{
				Name: "tunnel-ca-bundle",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: cluster.Spec.GRPCTunnel.TLS.BundleRef,
						},
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "tunnel-ca-bundle",
				MountPath: gnmic.TunnelCABundleMountPath,
				ReadOnly:  true,
			})
		}
	}

	// add client TLS volumes if ClientTLS is configured (for mTLS to targets)
	// A single certificate is shared by all pods in the cluster
	if cluster.Spec.ClientTLS != nil {
		// add client TLS certificates if issuerRef is configured
		if cluster.Spec.ClientTLS.IssuerRef != "" {
			// Single certificate shared by all pods - mounted from a secret
			clientCertSecretName := fmt.Sprintf("%s%s-client-tls", resourcePrefix, cluster.Name)

			if cluster.Spec.ClientTLS.UseCSIDriver {
				// use cert-manager CSI driver for client certificates
				// CN and DNS names use cluster-name.namespace
				commonName := fmt.Sprintf("%s.%s", cluster.Name, cluster.Namespace)
				volumes = append(volumes, corev1.Volume{
					Name: "client-tls-certs",
					VolumeSource: corev1.VolumeSource{
						CSI: &corev1.CSIVolumeSource{
							Driver:   "csi.cert-manager.io",
							ReadOnly: ptr.To(true),
							VolumeAttributes: map[string]string{
								"csi.cert-manager.io/issuer-name": cluster.Spec.ClientTLS.IssuerRef,
								"csi.cert-manager.io/issuer-kind": "Issuer",
								"csi.cert-manager.io/common-name": commonName,
								"csi.cert-manager.io/dns-names":   commonName,
							},
						},
					},
				})
			} else {
				// mount the single certificate secret directly
				volumes = append(volumes, corev1.Volume{
					Name: "client-tls-certs",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: clientCertSecretName,
							Optional:   ptr.To(true),
						},
					},
				})
			}

			// add client TLS volume mount (no subPathExpr needed - same cert for all pods)
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "client-tls-certs",
				MountPath: gnmic.ClientTLSCertFilesBasePath,
				ReadOnly:  true,
			})
		}

		// add client CA bundle volume if bundleRef is configured for target server certificate verification
		if cluster.Spec.ClientTLS.BundleRef != "" {
			volumes = append(volumes, corev1.Volume{
				Name: "client-ca-bundle",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: cluster.Spec.ClientTLS.BundleRef,
						},
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "client-ca-bundle",
				MountPath: gnmic.ClientCABundleMountPath,
				ReadOnly:  true,
			})
		}
	}

	return &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      stsName,
				Namespace: cluster.Namespace,
				Labels:    labels,
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas:    cluster.Spec.Replicas,
				ServiceName: stsName, // references the headless service
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						LabelClusterName: cluster.Name,
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
					Spec: corev1.PodSpec{
						InitContainers: initContainers,
						Containers: []corev1.Container{
							{
								Name:  "gnmic",
								Image: cluster.Spec.Image,
								Ports: buildContainerPorts(cluster),
								Command: []string{
									"/app/gnmic",
									"collector",
									"--config",
									"/etc/gnmic/config.yaml",
								},
								VolumeMounts: volumeMounts,
								Resources:    cluster.Spec.Resources,
								Env:          envVars,
							},
						},
						Volumes: volumes,
					},
				},
			},
		},
		configMap, nil
}

func buildContainerPorts(cluster *gnmicv1alpha1.Cluster) []corev1.ContainerPort {
	restPort := int32(defaultRestPort)
	if cluster.Spec.API != nil && cluster.Spec.API.RestPort != 0 {
		restPort = cluster.Spec.API.RestPort
	}
	ports := []corev1.ContainerPort{
		{
			Name:          "rest",
			ContainerPort: restPort,
		},
	}
	if cluster.Spec.API != nil && cluster.Spec.API.GNMIPort != 0 {
		ports = append(ports, corev1.ContainerPort{
			Name:          "gnmi",
			ContainerPort: cluster.Spec.API.GNMIPort,
		})
	}
	if cluster.Spec.GRPCTunnel != nil && cluster.Spec.GRPCTunnel.Port != 0 {
		ports = append(ports, corev1.ContainerPort{
			Name:          "tunnel",
			ContainerPort: cluster.Spec.GRPCTunnel.Port,
		})
	}
	return ports
}

func resourcesEqual(a, b corev1.ResourceRequirements) bool {
	// compare requests
	if !resourceListEqual(a.Requests, b.Requests) {
		return false
	}
	// compare limits
	if !resourceListEqual(a.Limits, b.Limits) {
		return false
	}
	return true
}

func resourceListEqual(a, b corev1.ResourceList) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || !v.Equal(bv) {
			return false
		}
	}
	return true
}
