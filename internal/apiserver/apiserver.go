package apiserver

//go:generate go tool oapi-codegen -config cfg.yaml openapi.yaml
// or use go generate ./internal/apiserver in the console (install from https://github.com/oapi-codegen/oapi-codegen)

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller"
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

// CreateTargets binds payload to Target struct defined in openapi.yaml and TBD...
func (a *APIServer) CreateTargets(c *gin.Context) {
	var payload []Target
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// For testing, to see the payload that is being sent
	for _, target := range payload {
		if target.Name != nil {
			fmt.Printf("name: %s, ", *target.Name)
		}
		if target.Address != nil {
			fmt.Printf("address: %s, ", *target.Address)
		}
		if target.Profile != nil {
			fmt.Printf("profile: %s, ", *target.Profile)
		}
		if target.Tags != nil {
			fmt.Printf("tags: %s", *target.Tags)
		}
		fmt.Printf("\n")
	}

	// TODO: send target received from interface to autodiscover logic via channel.

	c.JSON(http.StatusOK, payload)
}
