package apiserver

//go:generate go tool oapi-codegen -config cfg.yaml openapi.yaml
// To generate code, install openapi-codegen from https://github.com/oapi-codegen/oapi-codegen)
// Then use: go generate ./internal/apiserver

import (
	"context"
	"fmt"
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
}

func New(addr string, clusterReconciler *controller.ClusterReconciler) (*APIServer, error) {
	router := gin.Default()
	a := &APIServer{
		Server: &http.Server{
			Addr:    addr,
			Handler: router,
		},
		router:            router,
		clusterReconciler: clusterReconciler,
	}

	apiBaseURL := "/api/v1/namespaceCluster/namegNMIcCluster"
	RegisterHandlersWithOptions(router, a, GinServerOptions{BaseURL: apiBaseURL})
	return a, nil
}

// kubectl port-forward -n gnmic-system svc/gnmic-controller-manager-api 8082:8082 --address=0.0.0.0

// GetClusterPlan returns cluster plan
func (a *APIServer) GetClusterPlan(c *gin.Context) {
	plan, err := a.clusterReconciler.GetClusterPlan("temp", "temp")
	if err != nil {
		c.String(404, err.Error())
		return
	}
	c.JSON(200, plan)
}

// CreateTargets binds payload to payloadTargets struct defined in openapi contract. Passes
func (a *APIServer) CreateTargets(c *gin.Context) {
	var payloadTargets Targets
	fmt.Println("Binding Target to PayloadTarget")
	if err := c.ShouldBind(&payloadTargets); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// the openapi.yaml contract has required fields, but these are not enforced... To enforce them, a middleware
	// needs to be used: https://deepwiki.com/oapi-codegen/oapi-codegen/7-middleware-and-validation
	// The one for gin-gonic is not actively maintained, so for v1 I'll do validation manually. To be improved.
	if payloadTargets.TargetSourceNameSpace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "targetSourceNameSpace is required"})
		return
	}
	if payloadTargets.TargetSourceName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "targetSourceName is required"})
		return
	}


	targets := []core.DiscoveryEvent{}
	if len(payloadTargets.TargetList) > 0 {
		for i, target := range payloadTargets.TargetList {
			if target.Address == "" || target.Name == "" || target.Operation == "" {
				logger.Warn("Target receieved at index %s by pull interface does not contain Address, Name or Operation and is skipped.", i)
				break
			}
			event := core.CREATE
			switch target.Operation {
			case Create:
				event = core.CREATE
			case Delete:
				event = core.DELETE
			}
			targets = append(targets, core.DiscoveryEvent{
				Target: core.DiscoveredTarget{
					Name:    target.Name,
					Address: target.Address,
					Labels:  map[string]string{"key": "Is this a tag?"},
				},
				Event: event,
			})
		}
	}

	key := types.NamespacedName{
		Namespace: payloadTargets.TargetSourceNameSpace,
		Name:      payloadTargets.TargetSourceName,
	}
	ch, ok := a.DiscoveryRegistry.Get(key)
	if !ok {
		// Error message to be udpated!!
		c.JSON(http.StatusBadRequest, gin.H{"error": "Target Source doesn't exist"})
		return
	}
	core.SendEvents(context.Background(), ch, targets, 10) // make number constant
	c.JSON(http.StatusOK, payloadTargets)
}
