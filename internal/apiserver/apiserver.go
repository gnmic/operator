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
}

func New(addr string, clusterReconciler *controller.ClusterReconciler) (*APIServer, *gin.Engine) {
	router := gin.Default()
	a := &APIServer{
		Server: &http.Server{
			Addr:    addr,
			Handler: router,
		},
		router:            router,
		clusterReconciler: clusterReconciler,
	}
	return a, router
}

func (a *APIServer) GetHealthz(c *gin.Context) {
	c.Status(http.StatusOK)
}

func (a *APIServer) GetReadyz(c *gin.Context) {
	c.Status(http.StatusOK)
}

func (a *APIServer) GetClusterPlan(c *gin.Context) {
	log.Printf("received GET request: path=%s method=%s remote=%s", c.Request.URL.Path, c.Request.Method, c.Request.RemoteAddr)
	namespace, name := c.Param("namespace"), c.Param("name")
	plan, err := a.clusterReconciler.GetClusterPlan(namespace, name)
	if err != nil {
		c.String(404, err.Error())
		return
	}
	c.JSON(200, plan)
}

// kubectl port-forward -n gnmic-system svc/gnmic-controller-manager-api 8082:8082 --address=0.0.0.0
// curl -X POST http://localhost:8082/clusters/gnmic-system/gnmic-controller-manager/createTarget

func (a *APIServer) CreateTargets(c *gin.Context) {
	var payload []Target
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// testing
	for _, target := range payload {
		fmt.Printf("name: %s, address: %s, profile: %s, tags: %s\n", *target.Name, *target.Address, *target.Profile, *target.Tags)
	}

	c.JSON(http.StatusOK, payload)
}
