package apiserver

//go:generate go tool oapi-codegen -config cfg.yaml openapi.yaml
// or use go generate ./internal/apiserver in the console (install from https://github.com/oapi-codegen/oapi-codegen)

import (
	"context"
	"fmt"
	"net/http"

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

// CreateTargets binds payload to Target struct defined in openapi.yaml and sends it to pull loader
func (a *APIServer) CreateTargets(c *gin.Context) {
	// logger.Info("Create Targets called")

	var payloadTarget []Target
	var payloadTargetSource TargetSource
	fmt.Println("Binding Target to PayloadTarget")
	// https://gin-gonic.com/en/docs/binding/bind-body-into-different-structs/
	if err := c.ShouldBindBodyWithJSON(&payloadTarget); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		fmt.Printf("err: %s", err.Error)
		return
	}
	fmt.Printf("payloadTarget: %s", payloadTarget)
	if err := c.ShouldBindBodyWithJSON(&payloadTargetSource); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// error {"error":"json: cannot unmarshal object into Go   value of type []apiserver.Target"}

	targets := []core.DiscoveryEvent{}
	for _, target := range payloadTarget {
		event := core.CREATE
		switch *target.Operation {
		case Create:
			event = core.CREATE
		case Delete:
			event = core.DELETE
		}
		targets = append(targets, core.DiscoveryEvent{
			Target: core.DiscoveredTarget{
				Name:    *target.Name,
				Address: *target.Address,
				Labels:  map[string]string{"key": "Is this a tag?"},
			},
			Event: event,
		})
	}

	key := types.NamespacedName{
		Namespace: *payloadTargetSource.Namespace,
		Name:      *payloadTargetSource.Name,
	}
	ch, ok := a.DiscoveryRegistry.Get(key)
	if !ok {
		// Error message to be udpated!!
		c.JSON(http.StatusBadRequest, gin.H{"error": "Target Source doesn't exist"})
		return
	}

	core.SendEvents(context.Background(), ch, targets, 10) // make number constant
	c.JSON(http.StatusOK, payloadTarget)
}
