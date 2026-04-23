package apiserver

//go:generate go tool oapi-codegen -config cfg.yaml openapi.yaml
// or use go generate ./internal/apiserver in the console (install from https://github.com/oapi-codegen/oapi-codegen)

import (
	"errors"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller"
	"github.com/gnmic/operator/internal/controller/discovery/core"
)

type APIServer struct {
	Server            *http.Server
	router            *gin.Engine
	clusterReconciler *controller.ClusterReconciler
	namespace         string
	clusterName       string
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
		namespace:         os.Getenv("POD_NAMESPACE"),
		clusterName:       os.Getenv("CLUSTER_NAME"),
	}

	if a.namespace == "" || a.clusterName == "" {
		return nil, errors.New("POD_NAMESPACE and CLUSTER_NAME must be set")
	}
	apiBaseURL := "/api/v1/" + a.namespace + "/" + a.clusterName
	RegisterHandlersWithOptions(router, a, GinServerOptions{BaseURL: apiBaseURL})
	return a, nil
}

// kubectl port-forward -n gnmic-system svc/gnmic-controller-manager-api 8082:8082 --address=0.0.0.0

// GetClusterPlan returns cluster plan
func (a *APIServer) GetClusterPlan(c *gin.Context) {
	plan, err := a.clusterReconciler.GetClusterPlan(a.namespace, a.clusterName)
	if err != nil {
		c.String(404, err.Error())
		return
	}
	c.JSON(200, plan)
}

// CreateTargets binds payload to Target struct defined in openapi.yaml and sends it to pull loader
func (a *APIServer) CreateTargets(c *gin.Context) {
	var payload []Target
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	targets := []core.DiscoveredTarget{}
	for _, target := range payload {
		targets = append(targets, core.DiscoveredTarget{
			Name:    *target.Name,
			Address: *target.Address,
			Labels:  map[string]string{"key": "Is this a tag?"},
		})
	}
	
	// discovery / core / helpers / sendEvents to send received udpates to TagetManager
	// loader push not needed
	c.JSON(http.StatusOK, payload)
}
