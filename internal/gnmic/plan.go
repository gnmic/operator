package gnmic

import (
	"fmt"
	"hash/fnv"
	"maps"
	"sort"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/utils"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PlanBuilder builds an ApplyPlan from pipeline data
type PlanBuilder struct {
	// all currently active pipelines
	pipelines map[string]*PipelineData
	// an impl to get credentials from a secret
	credsFetcher CredentialsFetcher
	// client TLS paths for target connections
	clientTLS *ClientTLSPaths
	// relationships between resources
	relationships resourceRelationship
	// prometheus output controller selected ports for each pipeline/output
	// pipelineName -> outputNN -> port
	prometheusOutputPorts map[string]map[string]int32
}

type resourceRelationship struct {
	// subscription -> outputs
	subscriptionOutputs map[string]map[string]struct{}
	// target -> subscriptions
	targetSubscriptions map[string]map[string]struct{}
	// input -> outputs
	inputOutputs map[string]map[string]struct{}
	// output -> processors
	outputProcessors map[string][]string
	// input -> processors
	inputProcessors map[string][]string
}

// NewPlanBuilder creates a new PlanBuilder
func NewPlanBuilder(credsFetcher CredentialsFetcher) *PlanBuilder {
	return &PlanBuilder{
		pipelines:    make(map[string]*PipelineData),
		credsFetcher: credsFetcher,
		relationships: resourceRelationship{
			subscriptionOutputs: make(map[string]map[string]struct{}),
			targetSubscriptions: make(map[string]map[string]struct{}),
			inputOutputs:        make(map[string]map[string]struct{}),
			outputProcessors:    make(map[string][]string),
			inputProcessors:     make(map[string][]string),
		},
		prometheusOutputPorts: make(map[string]map[string]int32),
	}
}

// WithClientTLS sets the client TLS paths for target connections
func (b *PlanBuilder) WithClientTLS(clientTLS *ClientTLSPaths) *PlanBuilder {
	b.clientTLS = clientTLS
	return b
}

// AddPipeline adds pipeline data to the builder
func (b *PlanBuilder) AddPipeline(name string, data *PipelineData) *PlanBuilder {
	b.pipelines[name] = data
	return b
}

// Build creates the ApplyPlan from all added pipelines
func (b *PlanBuilder) Build() (*ApplyPlan, error) {
	plan := &ApplyPlan{
		Targets:             make(map[string]*gapi.TargetConfig),
		Subscriptions:       make(map[string]*gapi.SubscriptionConfig),
		Outputs:             make(map[string]map[string]any),
		Inputs:              make(map[string]map[string]any),
		Processors:          make(map[string]map[string]any),
		TunnelTargetMatches: make(map[string]*TunnelTargetMatch),
		PrometheusPorts:     make(map[string]int32),
	}
	// 1) collect relationships across all pipelines
	b.collectRelationships()

	// 2) build the configs
	for _, pipelineData := range b.pipelines {
		// 2.1) build target configs
		if err := b.buildTargets(plan, pipelineData); err != nil {
			return nil, err
		}

		// 2.2) build subscription configs
		b.buildSubscriptions(plan, pipelineData)

		// 2.3) build output configs
		if err := b.buildOutputs(plan, pipelineData); err != nil {
			return nil, err
		}

		// 2.4) build input configs
		if err := b.buildInputs(plan, pipelineData); err != nil {
			return nil, err
		}

		// 2.5) build processor configs (merged from output and input processors)
		if err := b.buildProcessors(plan, pipelineData); err != nil {
			return nil, err
		}

		// 2.6) build tunnel target match configs
		if err := b.buildTunnelTargetMatches(plan, pipelineData); err != nil {
			return nil, err
		}
		// 2.7) assign prometheus output ports
		if err := b.assignPrometheusOutputPorts(plan, pipelineData); err != nil {
			return nil, err
		}
	}

	return plan, nil
}

func (b *PlanBuilder) collectRelationships() {
	for _, pipelineData := range b.pipelines {
		// subscription -> outputs
		outputNames := make([]string, 0, len(pipelineData.Outputs))
		for outputNN := range pipelineData.Outputs {
			outputNames = append(outputNames, outputNN)
		}

		for subNN := range pipelineData.Subscriptions {
			if _, ok := b.relationships.subscriptionOutputs[subNN]; !ok {
				b.relationships.subscriptionOutputs[subNN] = make(map[string]struct{})
			}
			for _, outputName := range outputNames {
				b.relationships.subscriptionOutputs[subNN][outputName] = struct{}{}
			}
		}
		// target -> subscriptions
		subNames := make([]string, 0, len(pipelineData.Subscriptions))
		for subNN := range pipelineData.Subscriptions {
			subNames = append(subNames, subNN)
		}

		for targetNN := range pipelineData.Targets {
			if _, ok := b.relationships.targetSubscriptions[targetNN]; !ok {
				b.relationships.targetSubscriptions[targetNN] = make(map[string]struct{})
			}
			for _, subName := range subNames {
				b.relationships.targetSubscriptions[targetNN][subName] = struct{}{}
			}
		}
		// input -> outputs
		inputOutputNames := make([]string, 0, len(pipelineData.Outputs))
		for outputNN := range pipelineData.Outputs {
			inputOutputNames = append(inputOutputNames, outputNN)
		}

		for inputNN := range pipelineData.Inputs {
			if _, ok := b.relationships.inputOutputs[inputNN]; !ok {
				b.relationships.inputOutputs[inputNN] = make(map[string]struct{})
			}
			for _, outputName := range inputOutputNames {
				b.relationships.inputOutputs[inputNN][outputName] = struct{}{}
			}
		}
		// output -> processors
		// ordered relationship between outputs and their processors.
		// builds map[outputNN][]processorNN where the slice maintains the order from the pipeline.
		processorNames := make([]string, 0, len(pipelineData.OutputProcessors))
		for processorNN := range pipelineData.OutputProcessors {
			processorNames = append(processorNames, processorNN)
		}

		for outputNN := range pipelineData.Outputs {
			if _, ok := b.relationships.outputProcessors[outputNN]; !ok {
				b.relationships.outputProcessors[outputNN] = make([]string, 0)
			}
			for _, processorName := range processorNames {
				b.relationships.outputProcessors[outputNN] = append(b.relationships.outputProcessors[outputNN], processorName)
			}
		}
		// input -> processors
		// ordered relationship between inputs and their processors.
		// builds map[inputNN][]processorNN where the slice maintains the order from the pipeline.
		inputProcessorNames := make([]string, 0, len(pipelineData.InputProcessors))
		for processorNN := range pipelineData.InputProcessors {
			inputProcessorNames = append(inputProcessorNames, processorNN)
		}

		for inputNN := range pipelineData.Inputs {
			if _, ok := b.relationships.inputProcessors[inputNN]; !ok {
				b.relationships.inputProcessors[inputNN] = make([]string, 0)
			}
			for _, processorName := range inputProcessorNames {
				b.relationships.inputProcessors[inputNN] = append(b.relationships.inputProcessors[inputNN], processorName)
			}
		}
	}
}

func (b *PlanBuilder) buildTargets(plan *ApplyPlan, pipelineData *PipelineData) error {
	for targetNN, targetSpec := range pipelineData.Targets {
		if _, ok := plan.Targets[targetNN]; ok {
			continue
		}

		namespace, name := utils.SplitNN(targetNN)

		// find the target profile: TODO: cannot happen once the data is collected ?
		profileSpec, ok := pipelineData.TargetProfiles[namespace+Delimiter+targetSpec.Profile]
		if !ok {
			continue
		}

		// build target config
		target := &gnmicv1alpha1.Target{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: targetSpec,
		}

		// fetch credentials if needed
		var creds *Credentials
		if profileSpec.CredentialsRef != "" && b.credsFetcher != nil {
			var err error
			creds, err = b.credsFetcher.FetchCredentials(namespace, profileSpec.CredentialsRef)
			if err != nil {
				return err
			}
		}

		var subscriptions []string
		if subscriptionsMap, ok := b.relationships.targetSubscriptions[targetNN]; ok {
			subscriptions = make([]string, 0, len(subscriptionsMap))
			for subscriptionName := range subscriptionsMap {
				subscriptions = append(subscriptions, subscriptionName)
			}
		}

		targetConfig := buildTargetConfig(target, &profileSpec, creds, b.clientTLS)
		targetConfig.Subscriptions = subscriptions

		plan.Targets[targetNN] = targetConfig
	}

	return nil
}

func (b *PlanBuilder) buildSubscriptions(plan *ApplyPlan, pipelineData *PipelineData) {
	for subNN, subSpec := range pipelineData.Subscriptions {
		if _, ok := plan.Subscriptions[subNN]; ok {
			continue
		}

		var outputs []string
		if outputsMap, ok := b.relationships.subscriptionOutputs[subNN]; ok {
			outputs = make([]string, 0, len(outputsMap))
			for outputName := range outputsMap {
				outputs = append(outputs, outputName)
			}
		}

		subConfig := buildSubscriptionConfig(subNN, &subSpec, outputs, pipelineData.Subscriptions)

		plan.Subscriptions[subNN] = subConfig
	}
}

func (b *PlanBuilder) buildOutputs(plan *ApplyPlan, pipelineData *PipelineData) error {
	for outputNN, outputSpec := range pipelineData.Outputs {
		if _, ok := plan.Outputs[outputNN]; ok {
			continue
		}

		options := &outputConfigOptions{
			Processors: b.relationships.outputProcessors[outputNN],
		}
		if pipelineData.ResolvedOutputAddresses != nil {
			options.ResolvedAddresses = pipelineData.ResolvedOutputAddresses[outputNN]
		}

		outputConfig, err := buildOutputConfig(&outputSpec, options)
		if err != nil {
			return err
		}

		plan.Outputs[outputNN] = outputConfig
	}

	return nil
}

func (b *PlanBuilder) buildInputs(plan *ApplyPlan, pipelineData *PipelineData) error {
	for inputNN, inputSpec := range pipelineData.Inputs {
		if _, ok := plan.Inputs[inputNN]; ok {
			continue
		}

		// collect outputs for this input
		var outputs []string
		if outputSet, ok := b.relationships.inputOutputs[inputNN]; ok {
			outputs = make([]string, 0, len(outputSet))
			for outputName := range outputSet {
				outputs = append(outputs, outputName)
			}
		}

		processors := b.relationships.inputProcessors[inputNN]
		inputConfig, err := buildInputConfig(&inputSpec, outputs, processors)
		if err != nil {
			return err
		}

		plan.Inputs[inputNN] = inputConfig
	}

	return nil
}

func (b *PlanBuilder) buildProcessors(plan *ApplyPlan, pipelineData *PipelineData) error {
	// process output processors
	for processorNN, processorSpec := range pipelineData.OutputProcessors {
		if _, ok := plan.Processors[processorNN]; ok {
			continue
		}

		processorConfig, err := buildProcessorConfig(&processorSpec)
		if err != nil {
			return err
		}
		plan.Processors[processorNN] = processorConfig
	}

	// process input processors
	for processorNN, processorSpec := range pipelineData.InputProcessors {
		if _, ok := plan.Processors[processorNN]; ok {
			continue
		}

		processorConfig, err := buildProcessorConfig(&processorSpec)
		if err != nil {
			return err
		}
		plan.Processors[processorNN] = processorConfig
	}

	return nil
}

func (b *PlanBuilder) buildTunnelTargetMatches(plan *ApplyPlan, pipelineData *PipelineData) error {
	for policyNN, policySpec := range pipelineData.TunnelTargetPolicies {
		if _, ok := plan.TunnelTargetMatches[policyNN]; ok {
			continue
		}

		namespace, _ := utils.SplitNN(policyNN)

		// find the target profile for this policy
		profileSpec, ok := pipelineData.TargetProfiles[namespace+Delimiter+policySpec.Profile]
		if !ok {
			// skip if profile not found - validation should catch this earlier
			continue
		}

		// fetch credentials if needed
		var creds *Credentials
		if profileSpec.CredentialsRef != "" && b.credsFetcher != nil {
			var err error
			creds, err = b.credsFetcher.FetchCredentials(namespace, profileSpec.CredentialsRef)
			if err != nil {
				return err
			}
		}

		// build the tunnel target match config
		tunnelMatch := buildTunnelTargetMatch(&policySpec, &profileSpec, creds, b.clientTLS)
		plan.TunnelTargetMatches[policyNN] = tunnelMatch
	}

	return nil
}

func (b *PlanBuilder) assignPrometheusOutputPorts(plan *ApplyPlan, pipelineData *PipelineData) error {
	promoutputs := make([]string, 0)
	for outputNN, outputSpec := range pipelineData.Outputs {
		if outputSpec.Type != PrometheusOutputType {
			continue
		}
		promoutputs = append(promoutputs, outputNN)
	}
	pipelinePortMap, err := assignPorts(promoutputs, PrometheusDefaultPort, PrmetheusPortPoolSize)
	if err != nil {
		return err
	}
	maps.Copy(plan.PrometheusPorts, pipelinePortMap)
	for outputPNN, port := range pipelinePortMap {
		plan.Outputs[outputPNN]["listen"] = fmt.Sprintf(":%d", port)
	}

	return nil
}

func assignPorts(names []string, base int32, rangeSize int32) (map[string]int32, error) {
	if rangeSize <= 0 {
		return nil, fmt.Errorf("rangeSize must be > 0")
	}

	sort.Strings(names)

	used := make(map[int32]struct{}, len(names))
	result := make(map[string]int32, len(names))

	for _, name := range names {
		h1 := hash32(name)
		h2 := hash32("step:" + name)
		start := int32(h1 % uint32(rangeSize))
		step := int32(h2%(uint32(rangeSize-1))) + 1 // should not be zero

		var slot int32
		found := false

		for i := range rangeSize {
			slot = (start + i*step) % rangeSize
			if _, ok := used[slot]; !ok {
				found = true
				break
			}
		}

		if !found {
			return nil, fmt.Errorf("no free ports available in range")
		}

		used[slot] = struct{}{}
		result[name] = base + slot
	}

	return result, nil
}

func hash32(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}
