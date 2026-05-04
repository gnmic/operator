package apiserver

//go:generate go tool oapi-codegen -config cfg.yaml openapi.yaml
// To generate code, install openapi-codegen from https://github.com/oapi-codegen/oapi-codegen)
// Then use: go generate ./internal/apiserver

// kubectl port-forward -n gnmic-system svc/gnmic-controller-manager-api 8082:8082 --address=0.0.0.0

import (
	"context"
	"net/http"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller"
	"github.com/gnmic/operator/internal/controller/discovery"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/gnmic/operator/internal/controller/discovery/loaders/utils"
	"k8s.io/apimachinery/pkg/types"
)

type APIServer struct {
	Server            *http.Server
	router            *gin.Engine
	clusterReconciler *controller.ClusterReconciler
	DiscoveryRegistry *discovery.Registry[
		types.NamespacedName,
		core.DiscoveryRegistryValue,
	]
	chunzSize int
}

type urlStruct struct {
	namespace           string `uri:"namespace" binding:"required"`
	gNMIcControllerName string `uri:"gNMIcControllerName" binding:"required"`
}

func New(
	addr string,
	clusterReconciler *controller.ClusterReconciler,
	discoveryRegistry *discovery.Registry[
		types.NamespacedName,
		core.DiscoveryRegistryValue,
	],
	discoveryChunksize int,
) (*APIServer, error) {
	router := gin.Default()
	a := &APIServer{
		Server: &http.Server{
			Addr:    addr,
			Handler: router,
		},
		router:            router,
		clusterReconciler: clusterReconciler,
		DiscoveryRegistry: discoveryRegistry,
		chunzSize:         discoveryChunksize,
	}
	// apiBaseURL := "/api/v1/:namespace/:gNMIcClusterName"
	// RegisterHandlersWithOptions(router, a, GinServerOptions{BaseURL: apiBaseURL})
	a.routes()
	return a, nil
}

// kubectl port-forward -n gnmic-system svc/gnmic-controller-manager-api 8082:8082 --address=0.0.0.0

func (a *APIServer) Router() *gin.Engine {
	return a.router
}

func (a *APIServer) routes() {
	a.router.GET("/clusters/:namespace/:name/plan", a.GetClusterPlan)
	a.router.POST("/api/v1/:namespace/target-source/:name/createTargets", a.CreateTargets)
}

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

	registry, ok := a.DiscoveryRegistry.Get(getKey(payloadTargets))
	if !ok {
		logger.Error("TargetSource ", payloadTargets.TargetSourceNameSpace, "/", payloadTargets.TargetSourceName, "does not exist.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "TargetSource " + payloadTargets.TargetSourceNameSpace + " / " + payloadTargets.TargetSourceName + " does not exist"})
		return
	}
	// make sure channel is not closed if targetsource in registry is deleted
	// timeout for sending to the channel
	targets := createDiscoveryEvent(payloadTargets)
	// fmt.Printf("core.DiscoveryEvent was created: %v", targets)
	utils.SendEvents(context.Background(), registry.Channel, targets, a.chunzSize)
	c.JSON(http.StatusOK, payloadTargets)
}
