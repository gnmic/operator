package gnmic

import (
	gnmicv1alpha1 "github.com/karimra/gnmic-operator/api/v1alpha1"
	"github.com/karimra/gnmic-operator/internal/utils"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PlanBuilder builds an ApplyPlan from pipeline data
type PlanBuilder struct {
	// all currently active pipelines
	pipelines map[string]PipelineData
	// an impl to get credentials from a secret
	credsFetcher CredentialsFetcher
	// client TLS paths for target connections
	clientTLS *ClientTLSPaths
}

// NewPlanBuilder creates a new PlanBuilder
func NewPlanBuilder(credsFetcher CredentialsFetcher) *PlanBuilder {
	return &PlanBuilder{
		pipelines:    make(map[string]PipelineData),
		credsFetcher: credsFetcher,
	}
}

// WithClientTLS sets the client TLS paths for target connections
func (b *PlanBuilder) WithClientTLS(clientTLS *ClientTLSPaths) *PlanBuilder {
	b.clientTLS = clientTLS
	return b
}

// AddPipeline adds pipeline data to the builder
func (b *PlanBuilder) AddPipeline(name string, data PipelineData) *PlanBuilder {
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
	}

	// 1) collect relationships across all pipelines
	subscriptionOutputs := b.collectSubscriptionOutputs()
	targetSubscriptions := b.collectTargetSubscriptions()
	inputOutputs := b.collectInputOutputs()
	outputProcessors := b.collectOutputProcessors()
	inputProcessors := b.collectInputProcessors()

	// 2) build the configs (TODO: these are not needed the plan has the exact same maps)
	processedTargets := make(map[string]struct{})
	processedSubscriptions := make(map[string]struct{})
	processedOutputs := make(map[string]struct{})
	processedInputs := make(map[string]struct{})
	processedProcessors := make(map[string]struct{})
	processedTunnelPolicies := make(map[string]struct{})

	for _, pipelineData := range b.pipelines {
		// 2.1) build target configs
		if err := b.buildTargets(plan, pipelineData, targetSubscriptions, processedTargets); err != nil {
			return nil, err
		}

		// 2.2) build subscription configs
		b.buildSubscriptions(plan, pipelineData, subscriptionOutputs, processedSubscriptions)

		// 2.3) build output configs
		if err := b.buildOutputs(plan, pipelineData, outputProcessors, processedOutputs); err != nil {
			return nil, err
		}

		// 2.4) build input configs
		if err := b.buildInputs(plan, pipelineData, inputOutputs, inputProcessors, processedInputs); err != nil {
			return nil, err
		}

		// 2.5) build processor configs (merged from output and input processors)
		if err := b.buildProcessors(plan, pipelineData, processedProcessors); err != nil {
			return nil, err
		}

		// 2.6) build tunnel target match configs
		if err := b.buildTunnelTargetMatches(plan, pipelineData, processedTunnelPolicies); err != nil {
			return nil, err
		}
	}

	return plan, nil
}

func (b *PlanBuilder) collectSubscriptionOutputs() map[string]map[string]struct{} {
	subscriptionOutputs := make(map[string]map[string]struct{})

	for _, pipelineData := range b.pipelines {
		outputNames := make([]string, 0, len(pipelineData.Outputs))
		for outputNN := range pipelineData.Outputs {
			outputNames = append(outputNames, outputNN)
		}

		for subNN := range pipelineData.Subscriptions {
			if _, ok := subscriptionOutputs[subNN]; !ok {
				subscriptionOutputs[subNN] = make(map[string]struct{})
			}
			for _, outputName := range outputNames {
				subscriptionOutputs[subNN][outputName] = struct{}{}
			}
		}
	}

	return subscriptionOutputs
}

func (b *PlanBuilder) collectTargetSubscriptions() map[string]map[string]struct{} {
	targetSubscriptions := make(map[string]map[string]struct{})

	for _, pipelineData := range b.pipelines {
		subNames := make([]string, 0, len(pipelineData.Subscriptions))
		for subNN := range pipelineData.Subscriptions {
			subNames = append(subNames, subNN)
		}

		for targetNN := range pipelineData.Targets {
			if _, ok := targetSubscriptions[targetNN]; !ok {
				targetSubscriptions[targetNN] = make(map[string]struct{})
			}
			for _, subName := range subNames {
				targetSubscriptions[targetNN][subName] = struct{}{}
			}
		}
	}

	return targetSubscriptions
}

func (b *PlanBuilder) collectInputOutputs() map[string]map[string]struct{} {
	inputOutputs := make(map[string]map[string]struct{})

	for _, pipelineData := range b.pipelines {
		outputNames := make([]string, 0, len(pipelineData.Outputs))
		for outputNN := range pipelineData.Outputs {
			outputNames = append(outputNames, outputNN)
		}

		for inputNN := range pipelineData.Inputs {
			if _, ok := inputOutputs[inputNN]; !ok {
				inputOutputs[inputNN] = make(map[string]struct{})
			}
			for _, outputName := range outputNames {
				inputOutputs[inputNN][outputName] = struct{}{}
			}
		}
	}

	return inputOutputs
}

// collectOutputProcessors collects the ordered relationship between outputs and their processors.
// Returns map[outputNN][]processorNN where the slice maintains the order from the pipeline.
func (b *PlanBuilder) collectOutputProcessors() map[string][]string {
	outputProcessors := make(map[string][]string)

	for _, pipelineData := range b.pipelines {
		// use the ordered list from PipelineData
		processorOrder := pipelineData.OutputProcessorOrder

		for outputNN := range pipelineData.Outputs {
			if _, ok := outputProcessors[outputNN]; !ok {
				outputProcessors[outputNN] = make([]string, 0)
			}
			// append processors in order, avoiding duplicates
			seen := make(map[string]struct{})
			for _, existing := range outputProcessors[outputNN] {
				seen[existing] = struct{}{}
			}
			for _, processorNN := range processorOrder {
				if _, ok := seen[processorNN]; !ok {
					outputProcessors[outputNN] = append(outputProcessors[outputNN], processorNN)
					seen[processorNN] = struct{}{}
				}
			}
		}
	}

	return outputProcessors
}

// collectInputProcessors collects the ordered relationship between inputs and their processors.
// returns map[inputNN][]processorNN where the slice maintains the order from the pipeline.
func (b *PlanBuilder) collectInputProcessors() map[string][]string {
	inputProcessors := make(map[string][]string)

	for _, pipelineData := range b.pipelines {
		// use the ordered list from PipelineData
		processorOrder := pipelineData.InputProcessorOrder

		for inputNN := range pipelineData.Inputs {
			if _, ok := inputProcessors[inputNN]; !ok {
				inputProcessors[inputNN] = make([]string, 0)
			}
			// append processors in order, avoiding duplicates
			seen := make(map[string]struct{})
			for _, existing := range inputProcessors[inputNN] {
				seen[existing] = struct{}{}
			}
			for _, processorNN := range processorOrder {
				if _, ok := seen[processorNN]; !ok {
					inputProcessors[inputNN] = append(inputProcessors[inputNN], processorNN)
					seen[processorNN] = struct{}{}
				}
			}
		}
	}

	return inputProcessors
}

func (b *PlanBuilder) buildTargets(
	plan *ApplyPlan,
	pipelineData PipelineData,
	targetSubscriptions map[string]map[string]struct{},
	processed map[string]struct{},
) error {
	for targetNN, targetSpec := range pipelineData.Targets {
		if _, ok := processed[targetNN]; ok {
			continue
		}
		processed[targetNN] = struct{}{}

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

		targetConfig := buildTargetConfig(target, &profileSpec, creds, b.clientTLS)

		// add subscriptions
		if subs, ok := targetSubscriptions[targetNN]; ok {
			subNames := make([]string, 0, len(subs))
			for subName := range subs {
				subNames = append(subNames, subName)
			}
			targetConfig.Subscriptions = subNames
		}

		plan.Targets[targetNN] = targetConfig
	}

	return nil
}

func (b *PlanBuilder) buildSubscriptions(
	plan *ApplyPlan,
	pipelineData PipelineData,
	subscriptionOutputs map[string]map[string]struct{},
	processed map[string]struct{},
) {
	for subNN, subSpec := range pipelineData.Subscriptions {
		if _, ok := processed[subNN]; ok {
			continue
		}
		processed[subNN] = struct{}{}

		subConfig := buildSubscriptionConfig(subNN, &subSpec)

		// add outputs
		if outputs, ok := subscriptionOutputs[subNN]; ok {
			outputNames := make([]string, 0, len(outputs))
			for outputName := range outputs {
				outputNames = append(outputNames, outputName)
			}
			subConfig.Outputs = outputNames
		}

		plan.Subscriptions[subNN] = subConfig
	}
}

func (b *PlanBuilder) buildOutputs(
	plan *ApplyPlan,
	pipelineData PipelineData,
	outputProcessors map[string][]string,
	processed map[string]struct{},
) error {
	for outputNN, outputSpec := range pipelineData.Outputs {
		if _, ok := processed[outputNN]; ok {
			continue
		}
		processed[outputNN] = struct{}{}

		outputConfig, err := buildOutputConfig(&outputSpec)
		if err != nil {
			return err
		}

		// add event-processors if any (already ordered)
		if processors, ok := outputProcessors[outputNN]; ok && len(processors) > 0 {
			outputConfig["event-processors"] = processors
		}

		plan.Outputs[outputNN] = outputConfig
	}

	return nil
}

func (b *PlanBuilder) buildInputs(
	plan *ApplyPlan,
	pipelineData PipelineData,
	inputOutputs map[string]map[string]struct{},
	inputProcessors map[string][]string,
	processed map[string]struct{},
) error {
	for inputNN, inputSpec := range pipelineData.Inputs {
		if _, ok := processed[inputNN]; ok {
			continue
		}
		processed[inputNN] = struct{}{}

		// collect outputs for this input
		var outputs []string
		if outputSet, ok := inputOutputs[inputNN]; ok {
			outputs = make([]string, 0, len(outputSet))
			for outputName := range outputSet {
				outputs = append(outputs, outputName)
			}
		}

		inputConfig, err := buildInputConfig(&inputSpec, outputs)
		if err != nil {
			return err
		}

		// add event-processors if any (already ordered)
		if processors, ok := inputProcessors[inputNN]; ok && len(processors) > 0 {
			inputConfig["event-processors"] = processors
		}

		plan.Inputs[inputNN] = inputConfig
	}

	return nil
}

func (b *PlanBuilder) buildProcessors(
	plan *ApplyPlan,
	pipelineData PipelineData,
	processed map[string]struct{},
) error {
	// process output processors
	for processorNN, processorSpec := range pipelineData.OutputProcessors {
		if _, ok := processed[processorNN]; ok {
			continue
		}
		processed[processorNN] = struct{}{}

		processorConfig, err := buildProcessorConfig(&processorSpec)
		if err != nil {
			return err
		}
		plan.Processors[processorNN] = processorConfig
	}

	// process input processors
	for processorNN, processorSpec := range pipelineData.InputProcessors {
		if _, ok := processed[processorNN]; ok {
			continue
		}
		processed[processorNN] = struct{}{}

		processorConfig, err := buildProcessorConfig(&processorSpec)
		if err != nil {
			return err
		}
		plan.Processors[processorNN] = processorConfig
	}

	return nil
}

func (b *PlanBuilder) buildTunnelTargetMatches(
	plan *ApplyPlan,
	pipelineData PipelineData,
	processed map[string]struct{},
) error {
	for policyNN, policySpec := range pipelineData.TunnelTargetPolicies {
		if _, ok := processed[policyNN]; ok {
			continue
		}
		processed[policyNN] = struct{}{}

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
