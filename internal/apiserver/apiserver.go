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
	"github.com/gnmic/operator/internal/controller/discovery"
	"k8s.io/apimachinery/pkg/types"
)

type APIServer struct {
	Server            *http.Server
	router            *gin.Engine
	clusterReconciler *controller.ClusterReconciler

	DiscoveryRegistry *discovery.Registry[types.NamespacedName, core.DiscoveryRegistryValue]
}

type urlStruct struct {
	namespace string `uri:"namespace" binding:"required"`
	gNMIcClusterName string `uri:"gNMIcClusterName" binding:"required"`
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
	apiBaseURL := "/api/v1/:namespace/:gNMIcClusterName"
	RegisterHandlersWithOptions(router, a, GinServerOptions{BaseURL: apiBaseURL})
	return a, nil
}

// kubectl port-forward -n gnmic-system svc/gnmic-controller-manager-api 8082:8082 --address=0.0.0.0

// GetClusterPlan returns cluster plan
func (a *APIServer) GetClusterPlan(c *gin.Context) {
	url := parseURI(c)
	plan, err := a.clusterReconciler.GetClusterPlan(url.namespace, url.gNMIcClusterName)
	if err != nil {
		c.String(404, err.Error())
		return
	}
	c.JSON(200, plan)
}

// CreateTargets binds payload to payloadTargets struct defined in openapi contract. Creates a []core.DiscoveryEvent sends it to the core package.
func (a *APIServer) CreateTargets(c *gin.Context) {
	// Discussion with Daniel: this was input from Jan and Karim that the URI should be a template
	// But I don't think it is needed in the CreateTargets function
	// url := parseURI(c)
	// fmt.Printf("namespace: %s", url.namespace)
	// fmt.Printf("gNMIcClusterName: %s", url.gNMIcClusterName)

	var payloadTargets Targets
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
				logger.Warn("Target receieved at index", i , " by pull interface does not contain Address, Name or Operation and is skipped.")
				break
			}
			if target.Operation.Valid() != true {
				logger.Warn("Target receieved at index", i , " by pull interface has invalid Operation.")
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
		logger.Error("TargetSource " , payloadTargets.TargetSourceNameSpace, "/", payloadTargets.TargetSourceName,  "does not exist.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "TargetSource does not exist"})
		return
	}
	core.SendEvents(context.Background(), ch, targets, a.chunkSize)
	c.JSON(http.StatusOK, payloadTargets)
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
