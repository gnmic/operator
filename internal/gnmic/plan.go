package gnmic

import (
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"sort"
	"strconv"

	"github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/utils"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
)

var logger *slog.Logger

func init() {
	logLevel := slog.LevelInfo
	v, ok := os.LookupEnv("DEBUG")
	if ok && v == "true" {
		logLevel = slog.LevelDebug
	}
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
}

// PlanBuilder builds an ApplyPlan from pipeline data
type PlanBuilder struct {
	clusterName string
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
	// target distribution capacity
	targetDistributionCapacity int
	// credsCache memoises credential lookups for the duration of a single Build().
	// Thousands of targets typically share a handful of secrets, so without this the
	// same secret is fetched once per target. Deliberately not retained across builds:
	// a longer-lived cache would stop credential rotation from taking effect.
	credsCache map[string]*Credentials
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
func NewPlanBuilder(clusterName string, credsFetcher CredentialsFetcher) *PlanBuilder {
	return &PlanBuilder{
		clusterName:  clusterName,
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

func (b *PlanBuilder) WithTargetDistributionCapacity(capacity int) *PlanBuilder {
	b.targetDistributionCapacity = capacity
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
		Targets:                 make(map[string]*gapi.TargetConfig),
		CurrentTargetAssignment: make(map[int]map[string]struct{}),
		Subscriptions:           make(map[string]*gapi.SubscriptionConfig),
		Outputs:                 make(map[string]map[string]any),
		Inputs:                  make(map[string]map[string]any),
		Processors:              make(map[string]map[string]any),
		TunnelTargetMatches:     make(map[string]*TunnelTargetMatch),
		PrometheusPorts:         make(map[string]int32),
	}
	// Credentials are memoised per build only — see credsCache.
	b.credsCache = make(map[string]*Credentials)

	// 1) collect relationships across all pipelines
	b.collectRelationships(plan)

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
	}

	// Assign Prometheus listen ports across the whole plan so two pipelines
	// sharing (or each having) a prometheus output cannot both land on :9804.
	if err := b.assignPrometheusOutputPorts(plan); err != nil {
		return nil, err
	}

	return plan, nil
}

func (b *PlanBuilder) collectRelationships(plan *ApplyPlan) {
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

		for targetNN, targetCR := range pipelineData.Targets {
			// target --> subscriptions
			if _, ok := b.relationships.targetSubscriptions[targetNN]; !ok {
				b.relationships.targetSubscriptions[targetNN] = make(map[string]struct{})
			}
			for _, subName := range subNames {
				b.relationships.targetSubscriptions[targetNN][subName] = struct{}{}
			}

			// pods --> targets
			podIdx := b.findTargetCurrentAssignment(targetCR)
			if podIdx != nil {
				logger.Debug("target assigned to pod", "target", targetNN, "pod", *podIdx)
			} else {
				logger.Debug("target not assigned to any pod", "target", targetNN)
			}
			if podIdx != nil && *podIdx >= 0 {
				if plan.CurrentTargetAssignment == nil {
					plan.CurrentTargetAssignment = make(map[int]map[string]struct{})
				}
				if _, ok := plan.CurrentTargetAssignment[*podIdx]; !ok {
					plan.CurrentTargetAssignment[*podIdx] = make(map[string]struct{})
				}
				plan.CurrentTargetAssignment[*podIdx][targetNN] = struct{}{}
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
		// Prefer OutputProcessorOrder (refs then sorted selectors). Fall back to
		// sorted map keys for callers that only populate the map.
		processorNames := pipelineData.OutputProcessorOrder
		if len(processorNames) == 0 {
			processorNames = make([]string, 0, len(pipelineData.OutputProcessors))
			for processorNN := range pipelineData.OutputProcessors {
				processorNames = append(processorNames, processorNN)
			}
			sort.Strings(processorNames)
		}

		for outputNN := range pipelineData.Outputs {
			if _, ok := b.relationships.outputProcessors[outputNN]; !ok {
				b.relationships.outputProcessors[outputNN] = make([]string, 0)
			}
			b.relationships.outputProcessors[outputNN] = append(b.relationships.outputProcessors[outputNN], processorNames...)
		}
		// input -> processors
		inputProcessorNames := pipelineData.InputProcessorOrder
		if len(inputProcessorNames) == 0 {
			inputProcessorNames = make([]string, 0, len(pipelineData.InputProcessors))
			for processorNN := range pipelineData.InputProcessors {
				inputProcessorNames = append(inputProcessorNames, processorNN)
			}
			sort.Strings(inputProcessorNames)
		}

		for inputNN := range pipelineData.Inputs {
			if _, ok := b.relationships.inputProcessors[inputNN]; !ok {
				b.relationships.inputProcessors[inputNN] = make([]string, 0)
			}
			b.relationships.inputProcessors[inputNN] = append(b.relationships.inputProcessors[inputNN], inputProcessorNames...)
		}
	}
}

func (b *PlanBuilder) findTargetCurrentAssignment(targetCR v1alpha1.Target) *int {
	// TODO(KR): add other strategies here
	return b.findTargetCurrentAssignmentBoundedLoadHashing(targetCR)
}

func (b *PlanBuilder) findTargetCurrentAssignmentBoundedLoadHashing(targetCR v1alpha1.Target) *int {
	podID := targetCR.Status.ClusterStates[b.clusterName].Pod
	if podID == "" {
		return nil
	}
	// get pod index from pod ID
	podSuffix := ""
	for i := len(podID) - 1; i >= 0; i-- {
		if podID[i] == '-' {
			podSuffix = podID[i+1:]
			break
		}
	}
	podIdx, err := strconv.Atoi(podSuffix)
	if err != nil {
		return nil // KR: TODO warn ?
	}
	return &podIdx
}

// fetchCredentialsCached resolves a secret reference, reusing any value already fetched
// during the current Build(). The cache is cleared at the start of every Build() so that a
// rotated secret is picked up on the next reconcile.
func (b *PlanBuilder) fetchCredentialsCached(namespace, secretRef string) (*Credentials, error) {
	key := namespace + Delimiter + secretRef
	if creds, ok := b.credsCache[key]; ok {
		return creds, nil
	}
	creds, err := b.credsFetcher.FetchCredentials(namespace, secretRef)
	if err != nil {
		return nil, err
	}
	if b.credsCache == nil {
		b.credsCache = make(map[string]*Credentials)
	}
	b.credsCache[key] = creds
	return creds, nil
}

func (b *PlanBuilder) buildTargets(plan *ApplyPlan, pipelineData *PipelineData) error {
	for targetNN, target := range pipelineData.Targets {
		if _, ok := plan.Targets[targetNN]; ok {
			continue
		}

		namespace, _ := utils.SplitNN(targetNN)

		// find the target profile: TODO: cannot happen once the data is collected ?
		profileSpec, ok := pipelineData.TargetProfiles[namespace+Delimiter+target.Spec.Profile]
		if !ok {
			continue
		}

		// fetch credentials if needed
		var creds *Credentials
		if profileSpec.CredentialsRef != "" && b.credsFetcher != nil {
			var err error
			creds, err = b.fetchCredentialsCached(namespace, profileSpec.CredentialsRef)
			if err != nil {
				return err
			}
		}

		subscriptions := sortedKeys(b.relationships.targetSubscriptions[targetNN])

		targetConfig := buildTargetConfig(&target, &profileSpec, creds, b.clientTLS)
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

		outputs := sortedKeys(b.relationships.subscriptionOutputs[subNN])
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

		outputs := sortedKeys(b.relationships.inputOutputs[inputNN])
		processors := b.relationships.inputProcessors[inputNN]
		inputConfig, err := buildInputConfig(&inputSpec, outputs, processors)
		if err != nil {
			return err
		}

		plan.Inputs[inputNN] = inputConfig
	}

	return nil
}

// sortedKeys returns the keys of m in lexicographic order so set-like
// relationship slices (outputs, subscriptions) in the apply plan are stable
// across reconciles.
func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
			creds, err = b.fetchCredentialsCached(namespace, profileSpec.CredentialsRef)
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

// assignPrometheusOutputPorts decides which port each Prometheus output binds and
// records it in plan.PrometheusPorts, which is the single source of truth for anything
// downstream that needs to know -- in particular the Service built by
// reconcilePrometheusServices.
//
// An output whose config already carries a "listen" address keeps it. Overwriting it
// left the collector bound to a hash-derived port while the Service was built from the
// CR's listen value, so the Service pointed at a port nothing was listening on and
// scraping silently returned nothing. Outputs with no listen address are assigned from
// the hash pool, which now also skips every pinned port so two Prometheus outputs in
// one collector process cannot be handed the same one.
func (b *PlanBuilder) assignPrometheusOutputPorts(plan *ApplyPlan) error {
	pinned := make(map[string]int32)
	unpinned := make([]string, 0)

	for outputNN, cfg := range plan.Outputs {
		if t, _ := cfg["type"].(string); t != PrometheusOutputType {
			continue
		}
		raw, ok := cfg["listen"]
		if !ok || raw == nil {
			unpinned = append(unpinned, outputNN)
			continue
		}
		listen, ok := raw.(string)
		if !ok {
			return fmt.Errorf("output %q: listen must be a string, got %T", outputNN, raw)
		}
		port, err := ParseListenPort(listen)
		if err != nil {
			return fmt.Errorf("output %q: %w", outputNN, err)
		}
		if port == 0 {
			// listen present but empty -- treat as unset rather than binding :0
			unpinned = append(unpinned, outputNN)
			continue
		}
		pinned[outputNN] = port
	}

	if len(pinned) == 0 && len(unpinned) == 0 {
		return nil
	}

	pinnedNames := make([]string, 0, len(pinned))
	for outputNN := range pinned {
		pinnedNames = append(pinnedNames, outputNN)
	}
	sort.Strings(pinnedNames)

	reserved := make(map[int32]struct{}, len(pinned))
	byPort := make(map[int32]string, len(pinned))
	for _, outputNN := range pinnedNames {
		port := pinned[outputNN]
		if other, clash := byPort[port]; clash {
			// Both keep the port they asked for; the collector will fail to bind the
			// second. Say so here rather than let it surface only in gNMIc's logs.
			logger.Warn("prometheus outputs pin the same listen port",
				"port", port, "outputs", []string{other, outputNN})
		}
		byPort[port] = outputNN
		reserved[port] = struct{}{}
	}

	assigned, err := assignPorts(unpinned, PrometheusDefaultPort, PrmetheusPortPoolSize, reserved)
	if err != nil {
		return err
	}

	plan.PrometheusPorts = make(map[string]int32, len(pinned)+len(assigned))
	for outputNN, port := range pinned {
		plan.PrometheusPorts[outputNN] = port
	}
	for outputNN, port := range assigned {
		plan.PrometheusPorts[outputNN] = port
		plan.Outputs[outputNN]["listen"] = fmt.Sprintf(":%d", port)
	}
	return nil
}

// assignPorts gives each name a distinct port in [base, base+rangeSize), chosen by
// double hashing so the assignment is stable across reconciles. Ports in reserved are
// treated as already taken: they belong to outputs that pinned their own listen
// address and are not ours to hand out.
func assignPorts(names []string, base int32, rangeSize int32, reserved map[int32]struct{}) (map[string]int32, error) {
	// rangeSize must exceed 1: the probe step is taken modulo rangeSize-1, which is a
	// division by zero at 1.
	if rangeSize <= 1 {
		return nil, fmt.Errorf("rangeSize must be > 1")
	}

	sort.Strings(names)

	used := make(map[int32]struct{}, len(names)+len(reserved))
	for port := range reserved {
		if slot := port - base; slot >= 0 && slot < rangeSize {
			used[slot] = struct{}{}
		}
	}
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
