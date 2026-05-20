package apiserver

//go:generate go tool oapi-codegen -config cfg.yaml openapi.yaml
// To generate code, install openapi-codegen from https://github.com/oapi-codegen/oapi-codegen)
// Then use: go generate ./internal/apiserver

// kubectl port-forward -n gnmic-system svc/gnmic-controller-manager-api 8082:8082 --address=0.0.0.0

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller"
	"github.com/gnmic/operator/internal/controller/discovery"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/gnmic/operator/internal/controller/discovery/loaders/utils"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type APIServer struct {
	Server            *http.Server
	router            *gin.Engine
	clusterReconciler *controller.ClusterReconciler
	DiscoveryRegistry *discovery.Registry[
		types.NamespacedName,
		core.DiscoveryRegistryValue,
	]
	chunzSize   int
	logger      logr.Logger
	bearerToken bool
}

type urlStruct struct {
	Namespace string `uri:"namespace" binding:"required"`
	Name      string `uri:"name" binding:"required"`
}

func New(
	addr string,
	clusterReconciler *controller.ClusterReconciler,
	discoveryRegistry *discovery.Registry[
		types.NamespacedName,
		core.DiscoveryRegistryValue,
	],
	discoveryChunksize int,
	bearerToken string,
) (*APIServer, error) {
	router := gin.New()
	router.Use(gin.Recovery())
	logger := log.Log.WithValues("component", "api-server")
	a := &APIServer{
		Server: &http.Server{
			Addr:    addr,
			Handler: router,
		},
		router:            router,
		clusterReconciler: clusterReconciler,
		DiscoveryRegistry: discoveryRegistry,
		chunzSize:         discoveryChunksize,
		logger:            logger,
	}
	a.routes()
	logger.Info("API server initialized", "addr", addr, "chunkSize", discoveryChunksize)
	return a, nil
}

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
	logger := log.FromContext(c.Request.Context()).WithValues(
		"component", "apiserver",
		"namespace", url.Namespace,
		"cluster", url.Name,
	)
	logger.Info("Received GET request for GetClusterPlan")

	plan, err := a.clusterReconciler.GetClusterPlan(url.Namespace, url.Name)
	if err != nil {
		logger.Error(err, "Failed to get cluster plan")
		c.String(404, err.Error())
		return
	}
	c.JSON(200, plan)
}

// CreateTargets binds payload to payloadTargets struct defined in openapi contract. Creates a []core.DiscoveryEvent sends it to the core package.
func (a *APIServer) CreateTargets(c *gin.Context) {
	url := parseURI(c)
	logger := log.FromContext(c.Request.Context()).WithValues(
		"component", "apiserver",
		"namespace", url.Namespace,
		"targetsource", url.Name,
	)
	logger.Info("Received POST request for CreateTargets")

	if !a.verifyBearerToken(c, a.clusterReconciler) {
		logger.Info("Unauthorized request for CreateTargets")
		return
	}

	var payloadTargets Targets
	if err := c.ShouldBind(&payloadTargets); err != nil {
		logger.Error(err, "Failed to bind request payload")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	registry, ok := a.DiscoveryRegistry.Get(getKey(url))
	if !ok {
		err := fmt.Errorf("targetSource %s/%s does not exist", url.Namespace, url.Name)
		logger.Error(err, "TargetSource lookup failed")
		c.JSON(http.StatusBadRequest, gin.H{"error": "TargetSource " + url.Namespace + " / " + url.Name + " does not exist"})
		return
	}
	// WROMA: both of these things are not relevant here, but instead in utils.send. TODO, check with Daniel if and how this can be implemented
	// make sure channel is not closed if targetsource in registry is deleted ->
	// timeout for sending to the channel
	targets, err := createDiscoveryEvent(payloadTargets)
	if err != nil {
		logger.Error(err, "failed creating discoveryEvent")
		c.JSON(http.StatusBadRequest, gin.H{"error": err})
	}
	utils.SendEvents(context.Background(), registry.Channel, targets, a.chunzSize)
	c.JSON(http.StatusOK, payloadTargets)
}
