package apiserver

//go:generate go tool oapi-codegen -config cfg.yaml openapi.yaml
// To generate code, install openapi-codegen from https://github.com/oapi-codegen/oapi-codegen)
// Then use: go generate ./internal/apiserver
//
// kubectl port-forward -n gnmic-system svc/gnmic-controller-manager-api 8082:8082 --address=0.0.0.0

// docker run --rm -v ${PWD}:/local openapitools/openapi-generator-cli generate -i /local/internal/apiserver/openapi.yaml -g markdown -o /local/docs/content/docs/user-guide/rest-api

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
	gin.SetMode(gin.ReleaseMode)
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
	RegisterHandlers(router, a)
	logger.Info("API server initialized", "addr", addr, "chunkSize", discoveryChunksize)
	return a, nil
}

func (a *APIServer) Router() *gin.Engine {
	return a.router
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
func (a *APIServer) ApplyTargets(c *gin.Context) {
	url := parseURI(c)
	logger := log.FromContext(c.Request.Context()).WithValues(
		"component", "apiserver",
		"namespace", url.Namespace,
		"targetsource", url.Name,
	)
	logger.Info("Received POST request for CreateTargets")

	targetSource, err := a.fetchTargetSource(c.Request.Context(), getKey(url))
	if err != nil {
		logger.Error(err, "Failed to fetch TargetSource")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !a.verifyAuthentication(c, a.clusterReconciler, targetSource) {
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
	if registry.CommonLoaderConfig.Push == false {
		err := fmt.Errorf("targetSource %s/%s has the Push interface turned off", url.Namespace, url.Name)
		logger.Error(err, "POST request rejected")
		c.JSON(http.StatusBadRequest, gin.H{"error": "TargetSource " + url.Namespace + " / " + url.Name + " has the Push interface turned off"})
		return
	}

	if !ok {
		err := fmt.Errorf("targetSource %s/%s does not exist", url.Namespace, url.Name)
		logger.Error(err, "TargetSource lookup failed")
		c.JSON(http.StatusBadRequest, gin.H{"error": "TargetSource " + url.Namespace + " / " + url.Name + " does not exist"})
		return
	}

	targets, err := createDiscoveryEvent(payloadTargets)
	if err != nil {
		logger.Error(err, "failed creating discoveryEvent")
		c.JSON(http.StatusBadRequest, gin.H{"error": err})
		return
	}
	utils.SendEvents(context.Background(), registry.Channel, targets, a.chunzSize)
	c.JSON(http.StatusOK, payloadTargets)
}
