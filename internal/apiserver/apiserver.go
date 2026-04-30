package apiserver

//go:generate go tool oapi-codegen -config cfg.yaml openapi.yaml
// To generate code, install openapi-codegen from https://github.com/oapi-codegen/oapi-codegen)
// Then use: go generate ./internal/apiserver

import (
	"context"
	"net/http"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/gnmic/operator/internal/controller/discovery/registry"
	"k8s.io/apimachinery/pkg/types"
)

type APIServer struct {
	Server            *http.Server
	router            *gin.Engine
	clusterReconciler *controller.ClusterReconciler
	DiscoveryRegistry *registry.Registry[types.NamespacedName, []core.DiscoveryMessage]
	chunkSize         int
}

type urlStruct struct {
	namespace           string `uri:"namespace" binding:"required"`
	gNMIcControllerName string `uri:"gNMIcControllerName" binding:"required"`
}

func New(addr string, clusterReconciler *controller.ClusterReconciler, chunkSize int) (*APIServer, error) {
	router := gin.Default()
	a := &APIServer{
		Server: &http.Server{
			Addr:    addr,
			Handler: router,
		},
		router:            router,
		clusterReconciler: clusterReconciler,
		chunkSize:         chunkSize,
	}
	apiBaseURL := "/api/v1/:namespace/:gNMIcControllerName"
	RegisterHandlersWithOptions(router, a, GinServerOptions{BaseURL: apiBaseURL})
	return a, nil
}

// kubectl port-forward -n gnmic-system svc/gnmic-controller-manager-api 8082:8082 --address=0.0.0.0

// GetClusterPlan returns cluster plan
func (a *APIServer) GetClusterPlan(c *gin.Context) {
	url := parseURI(c)
	plan, err := a.clusterReconciler.GetClusterPlan(url.namespace, url.gNMIcControllerName)
	if err != nil {
		c.String(404, err.Error())
		return
	}
	c.JSON(200, plan)
}

// CreateTargets binds payload to payloadTargets struct defined in openapi contract. Creates a []core.DiscoveryEvent sends it to the core package.
func (a *APIServer) CreateTargets(c *gin.Context) {
	logger.Info("received POST request for CreateTargets.")

	var payloadTargets Targets
	if err := c.ShouldBind(&payloadTargets); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ch, ok := a.DiscoveryRegistry.Get(getKey(payloadTargets))
	if !ok {
		logger.Error("TargetSource ", payloadTargets.TargetSourceNameSpace, "/", payloadTargets.TargetSourceName, "does not exist.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "TargetSource " + payloadTargets.TargetSourceNameSpace + " / " + payloadTargets.TargetSourceName + " does not exist"})
		return
	}

	targets := createDiscoveryEvent(payloadTargets)
	// fmt.Printf("core.DiscoveryEvent was created: %v", targets)
	core.SendEvents(context.Background(), ch, targets, a.chunkSize)
	c.JSON(http.StatusOK, payloadTargets)
}

// createDiscoveryEvent creates object of type core.DiscoveryEvent
func createDiscoveryEvent(payloadTargets Targets) []core.DiscoveryEvent {
	targets := []core.DiscoveryEvent{}
	if len(payloadTargets.TargetList) > 0 {
		for i, target := range payloadTargets.TargetList {
			if target.Address == "" || target.Name == "" || target.Operation == "" {
				// no REST API return here as not all targets might
				logger.Warn("Target receieved at index", i, " by pull interface does not contain Address, Name or Operation and is skipped.")
				break
			}
			if target.Operation.Valid() != true {
				logger.Warn("Target receieved at index", i, " by pull interface has invalid Operation.")
				break
			}

			targets = append(targets, core.DiscoveryEvent{
				Target: core.DiscoveredTarget{
					Name:    target.Name,
					Address: target.Address,
					Labels:  convertTargetLabelsToMap(target),
				},
				Event: getEvent(target),
			})
		}
	}
	return targets
}

// getKey returns key for used to identify correct channel in DiscoveryRegistry
func getKey(payloadTargets Targets) types.NamespacedName {
	key := types.NamespacedName{
		Namespace: payloadTargets.TargetSourceNameSpace,
		Name:      payloadTargets.TargetSourceName,
	}
	return key
}

// convertTargetLabelsToMap converts target.Labels to map.
func convertTargetLabelsToMap(target Target) map[string]string {
	labelToMap := make(map[string]string)
	if target.Labels != nil {
		for _, tag := range *target.Labels {
			if tag.Key == nil || tag.Value == nil || *tag.Key == "" {
				continue
			}
			labelToMap[*tag.Key] = *tag.Value
		}
	}
	return labelToMap
}

// getEvent converts target.Operation to core.Operation.
func getEvent(target Target) core.EventAction {
	event := core.CREATE
	switch target.Operation {
	case Created:
		event = core.UPDATE
	case Updated:
		event = core.UPDATE
	case Deleted:
		event = core.DELETE
	default:
		logger.Warn("Received invalid Operation flag")
	}
	return event
}

// parseURI parses URI to urlStruct.
func parseURI(c *gin.Context) (url urlStruct) {
	var u urlStruct
	if err := c.ShouldBindUri(&u); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	return u
}
