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
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
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
	"github.com/gnmic/operator/internal/utils"
)

// reconcilePrometheusServices creates/updates/deletes services for Prometheus outputs
func (r *ClusterReconciler) reconcilePrometheusServices(ctx context.Context, cluster *gnmicv1alpha1.Cluster, plan map[string]*gnmic.PipelineData, prometheusPorts map[string]int32) error {
	logger := log.FromContext(ctx)

	// collect all Prometheus outputs across all pipelines
	prometheusOutputs := make(map[string]map[string]gnmicv1alpha1.OutputSpec) // outputNN -> pipelineName -> spec
	for pipelineName, pipelineData := range plan {
		for outputNN, outputSpec := range pipelineData.Outputs {
			if outputSpec.Type != gnmic.PrometheusOutputType {
				continue
			}
			if _, ok := prometheusOutputs[outputNN]; !ok {
				prometheusOutputs[outputNN] = make(map[string]gnmicv1alpha1.OutputSpec)
			}
			prometheusOutputs[outputNN][pipelineName] = outputSpec
		}
	}

	// get existing Prometheus services managed by this cluster
	existingServices, err := r.listPrometheusServicesForCluster(ctx, cluster.Namespace, cluster.Name)
	if err != nil {
		return err
	}
	existingServiceNames := make(map[string]struct{})
	for _, svc := range existingServices {
		existingServiceNames[svc.Name] = struct{}{}
	}

	// create/update services for each Prometheus output
	desiredServiceNames := make(map[string]struct{})

	for outputNN, pipelineOutputSpecs := range prometheusOutputs {
		pipelines := make([]string, 0, len(pipelineOutputSpecs))
		for pipelineName := range pipelineOutputSpecs {
			pipelines = append(pipelines, pipelineName)
		}
		sort.Strings(pipelines)

		for _, pipelineName := range pipelines {
			outputSpec := pipelineOutputSpecs[pipelineName]
			// Only the path is read from the CR. The port comes from the plan, which
			// records the user's own listen value when they set one and the assigned
			// port when they did not -- so the Service and the collector cannot
			// disagree. Reading the port from the CR here is what let them drift: the
			// plan builder had already replaced an explicit listen with a pool port.
			_, urlPath, err := parseListenPortAndPath(outputSpec.Config.Raw)
			if err != nil {
				logger.Error(err, "failed to parse config for Prometheus output", "output", outputNN)
				continue
			}
			port, ok := prometheusPorts[outputNN]
			if !ok || port == 0 {
				logger.Error(nil, "no port recorded for Prometheus output, skipping its service", "output", outputNN)
				continue
			}

			if urlPath == "" {
				urlPath = gnmic.PrometheusDefaultPath
			}
			// generate service name from output name
			// we use metadata.generateName to ensure the service name is unique
			// we will label the prometheus services with the pipeline and output names
			var pipelineName string
			var outputName string
			_, outputName = utils.SplitNN(outputNN)              // messy
			pipelineName, outputName = utils.SplitNN(outputName) // more messy
			serviceName := fmt.Sprintf("%s%s-prom-%s-%s", resourcePrefix, cluster.Name, pipelineName, outputName)
			desiredServiceNames[serviceName] = struct{}{}

			if err := r.reconcilePrometheusService(ctx, cluster, serviceName, outputName, pipelineName, port, urlPath, &outputSpec); err != nil {
				logger.Error(err, "failed to reconcile Prometheus service", "service", serviceName)
				return err
			}
			logger.Info("reconciled Prometheus service", "service", serviceName, "port", port)
		}
	}

	// delete services that are no longer needed
	for _, svc := range existingServices {
		if _, ok := desiredServiceNames[svc.Name]; !ok {
			logger.Info("deleting unused Prometheus service", "service", svc.Name)
			if err := r.Delete(ctx, &svc); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	return nil
}

// listPrometheusServicesForCluster lists all Prometheus output services for a cluster
func (r *ClusterReconciler) listPrometheusServicesForCluster(ctx context.Context, clusterNamespace, clusterName string) ([]corev1.Service, error) {
	var serviceList corev1.ServiceList
	err := r.List(ctx, &serviceList,
		client.InNamespace(clusterNamespace),
		client.MatchingLabels{
			LabelClusterName: clusterName,
			LabelServiceType: LabelValueServiceTypePrometheusOutput,
		},
	)
	if err != nil {
		return nil, err
	}
	return serviceList.Items, nil
}

// cleanupPrometheusServices deletes all Prometheus output services for a cluster
func (r *ClusterReconciler) cleanupPrometheusServices(ctx context.Context, clusterNamespace, clusterName string) error {
	services, err := r.listPrometheusServicesForCluster(ctx, clusterNamespace, clusterName)
	if err != nil {
		return err
	}
	for _, svc := range services {
		if err := r.Delete(ctx, &svc); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// reconcilePrometheusService creates or updates a service for a Prometheus output
func (r *ClusterReconciler) reconcilePrometheusService(ctx context.Context, cluster *gnmicv1alpha1.Cluster, serviceName, outputName, pipelineName string, port int32, urlPath string, outputSpec *gnmicv1alpha1.OutputSpec) error {
	desired := r.buildPrometheusService(cluster, serviceName, outputName, pipelineName, port, urlPath, outputSpec)

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

	// check if update is needed
	needsUpdate := false
	if current.Spec.Type != desired.Spec.Type {
		current.Spec.Type = desired.Spec.Type
		needsUpdate = true
	}
	if !servicePortsEqual(current.Spec.Ports, desired.Spec.Ports) {
		current.Spec.Ports = desired.Spec.Ports
		needsUpdate = true
	}
	if !maps.Equal(current.Labels, desired.Labels) {
		current.Labels = desired.Labels
		needsUpdate = true
	}
	if !maps.Equal(current.Annotations, desired.Annotations) {
		current.Annotations = desired.Annotations
		needsUpdate = true
	}

	if needsUpdate {
		return r.Update(ctx, &current)
	}
	return nil
}

// buildPrometheusService builds a service for a Prometheus output
func (r *ClusterReconciler) buildPrometheusService(cluster *gnmicv1alpha1.Cluster, serviceName, outputName, pipelineName string, port int32, urlPath string, outputSpec *gnmicv1alpha1.OutputSpec) *corev1.Service {
	labels := map[string]string{}
	annotations := map[string]string{}
	// default to ClusterIP if no service config is provided
	serviceType := corev1.ServiceTypeClusterIP

	if outputSpec.Service != nil {
		if outputSpec.Service.Type != "" {
			serviceType = outputSpec.Service.Type
		}
		if len(outputSpec.Service.Annotations) > 0 {
			maps.Copy(annotations, outputSpec.Service.Annotations)
		}
		if len(outputSpec.Service.Labels) > 0 {
			maps.Copy(labels, outputSpec.Service.Labels)
		}
	}

	// Operator labels and scrape annotations win over user-supplied keys with
	// the same name so ServiceMonitors and GC ownership stay reliable.
	labels["app.kubernetes.io/name"] = LabelValueName
	labels["app.kubernetes.io/managed-by"] = LabelValueManagedBy
	labels[LabelClusterName] = cluster.Name
	labels[LabelServiceType] = LabelValueServiceTypePrometheusOutput
	labels[LabelOutputName] = outputName
	labels[LabelPipelineName] = pipelineName
	annotations["prometheus.io/scrape"] = "true"
	annotations["prometheus.io/port"] = strconv.Itoa(int(port))
	annotations["prometheus.io/path"] = urlPath

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			// TODO: conside using this for unique service names
			// It would change the reconcile logic (cleanup especially)
			// GenerateName: serviceName,
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
					Name:       "metrics",
					Port:       port,
					TargetPort: intstr.FromInt32(port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// servicePortsEqual compares two slices of ServicePort
func servicePortsEqual(a, b []corev1.ServicePort) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name ||
			a[i].Port != b[i].Port ||
			a[i].Protocol != b[i].Protocol ||
			a[i].TargetPort.String() != b[i].TargetPort.String() {
			return false
		}
	}
	return true
}

type listenPortAndPath struct {
	Listen string `yaml:"listen,omitempty" json:"listen,omitempty"`
	Path   string `yaml:"path,omitempty" json:"path,omitempty"`
}

// parseListenPortAndPath parses the "listen" and "path" fields from output config.
// Supports: ":9804", "0.0.0.0:9804", "localhost:9804", "[::1]:9804".
// When listen is unset, port is 0 so callers can use the plan-assigned port
// from assignPrometheusOutputPorts — do not invent PrometheusDefaultPort here
// or the Service will disagree with the collector's actual listen address.
func parseListenPortAndPath(configRaw []byte) (int32, string, error) {
	if len(configRaw) == 0 {
		return 0, "", nil
	}

	var config listenPortAndPath
	if err := yaml.Unmarshal(configRaw, &config); err != nil {
		return 0, "", err
	}

	config.Path = strings.TrimSpace(config.Path)
	config.Listen = strings.TrimSpace(config.Listen)
	if config.Listen == "" {
		return 0, config.Path, nil
	}

	port, err := gnmic.ParseListenPort(config.Listen)
	if err != nil {
		return 0, "", err
	}
	return port, config.Path, nil
}
