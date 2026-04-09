package apiserver

//go:generate go tool oapi-codegen -config cfg.yaml openapi.yaml

import (
	"fmt"
	"log"
	"net/http"

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

func New(addr string, namespace string, clusterName string, clusterReconciler *controller.ClusterReconciler) (*APIServer, *gin.Engine) {
	router := gin.Default()
	a := &APIServer{
		Server: &http.Server{
			Addr:    addr,
			Handler: router,
		},
		router:            router,
		clusterReconciler: clusterReconciler,
		namespace:         namespace,
		clusterName:       clusterName,
	}
	return a, router
}

func (a *APIServer) GetClusterPlan(c *gin.Context) {
	log.Printf("received GET request: path=%s method=%s remote=%s", c.Request.URL.Path, c.Request.Method, c.Request.RemoteAddr)
	// plan, err := a.clusterReconciler.GetClusterPlan(a.namespace, a.clusterName)
	// if err != nil {
	// 	c.String(404, err.Error())
	// 	return
	// }
	// c.JSON(200, plan)
	c.JSON(http.StatusOK, "GetClusterPlan")
}

// kubectl port-forward -n gnmic-system svc/gnmic-controller-manager-api 8082:8082 --address=0.0.0.0
// curl -X POST http://localhost:8082/clusters/gnmic-system/gnmic-controller-manager/createTarget

func (a *APIServer) CreateTargets(c *gin.Context) {
	log.Printf("received POST request: path=%s method=%s remote=%s", c.Request.URL.Path, c.Request.Method, c.Request.RemoteAddr)
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
