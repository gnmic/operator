package apiserver

import (
	"encoding/json"
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
