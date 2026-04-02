package apiserver

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gnmic/operator/internal/controller"
)

type APIServer struct {
	Server            *http.Server
	clusterReconciler *controller.ClusterReconciler
}

func New(addr string, clusterReconciler *controller.ClusterReconciler) *APIServer {
	mux := http.NewServeMux()
	a := &APIServer{
		Server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		clusterReconciler: clusterReconciler,
	}
	a.routes(mux)
	return a
}

func (a *APIServer) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /clusters/{namespace}/{name}/plan", a.getClusterPlan)
	mux.HandleFunc("POST /clusters/{namespace}/{name}/createTarget", a.postCreateTarget)
}

func (a *APIServer) getClusterPlan(w http.ResponseWriter, r *http.Request) {
	namespace, name := r.PathValue("namespace"), r.PathValue("name")
	plan, err := a.clusterReconciler.GetClusterPlan(namespace, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(plan)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// kubectl port-forward -n gnmic-system svc/gnmic-controller-manager-api 8082:8082 --address=0.0.0.0
// curl -X POST http://localhost:8082/clusters/gnmic-system/gnmic-controller-manager/createTarget

func (a *APIServer) postCreateTarget(w http.ResponseWriter, r *http.Request) {
	log.Printf("received POST request: path=%s method=%s remote=%s", r.URL.Path, r.Method, r.RemoteAddr)
	w.WriteHeader(http.StatusAccepted)
}
