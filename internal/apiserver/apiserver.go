package apiserver

//go:generate go tool oapi-codegen -config cfg.yaml openapi.yaml
// To generate code, install openapi-codegen from https://github.com/oapi-codegen/oapi-codegen)
// Then use: go generate ./internal/apiserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller"
	"github.com/gnmic/operator/internal/controller/discovery"
	"github.com/gnmic/operator/internal/controller/discovery/core"
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
	ChunkSize int
}

type urlStruct struct {
	namespace        string `uri:"namespace" binding:"required"`
	gNMIcClusterName string `uri:"gNMIcClusterName" binding:"required"`
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
		ChunkSize:         discoveryChunksize,
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
	plan, err := a.clusterReconciler.GetClusterPlan(url.namespace, url.gNMIcClusterName)
	if err != nil {
		c.String(404, err.Error())
		return
	}
	c.JSON(200, plan)
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
