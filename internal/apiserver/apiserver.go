package apiserver

import (
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

func New(addr string, clusterReconciler *controller.ClusterReconciler) *APIServer {
	router := gin.Default()
	a := &APIServer{
		Server: &http.Server{
			Addr:    addr,
			Handler: router,
		},
		router:            router,
		clusterReconciler: clusterReconciler,
	}

	a.router.POST("/clusters/:namespace/:name/createTarget", a.postCreateTarget)
	a.router.GET("/clusters/:namespace/:name/plan", a.getClusterPlan)
	a.router.GET("/healthz", a.getHealthz)
	a.router.GET("/readyz", a.getReadyz)
	return a
}

func (a *APIServer) getHealthz(c *gin.Context) {
	c.Status(http.StatusOK)
}

func (a *APIServer) getReadyz(c *gin.Context) {
	c.Status(http.StatusOK)
}

func (a *APIServer) getClusterPlan(c *gin.Context) {
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

func (a *APIServer) postCreateTarget(c *gin.Context) {
	log.Printf("received POST request: path=%s method=%s remote=%s", c.Request.URL.Path, c.Request.Method, c.Request.RemoteAddr)
	c.Status(202)
}
