package apiserver

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.6.0 -config cfg.yaml openapi.yaml

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller"
	"github.com/gnmic/operator/internal/discovery"
)

const (
	// maxWebhookBody bounds what a refresh call may send; the body is only a
	// signature input.
	maxWebhookBody  = 1 << 20
	defaultDebounce = 5 * time.Second
)

// APIServer serves the operator's REST endpoints. It runs on every replica,
// leader or not: nothing it does needs the controllers, only a client.
type APIServer struct {
	Server            *http.Server
	router            *gin.Engine
	clusterReconciler *controller.ClusterReconciler
	client            client.Client
	logger            logr.Logger

	// now is overridable for tests.
	now func() time.Time
}

type urlStruct struct {
	Namespace string `uri:"namespace" binding:"required"`
	Name      string `uri:"name" binding:"required"`
}

// New builds the server. clusterReconciler serves the plan endpoint; c is
// used by the refresh endpoint to read TargetSources and Secrets and to
// annotate.
func New(addr string, clusterReconciler *controller.ClusterReconciler, c client.Client) *APIServer {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	logger := log.Log.WithValues("component", "api-server")

	a := &APIServer{
		Server:            &http.Server{Addr: addr, Handler: router, ReadHeaderTimeout: 10 * time.Second},
		router:            router,
		clusterReconciler: clusterReconciler,
		client:            c,
		logger:            logger,
		now:               time.Now,
	}
	RegisterHandlers(router, a)
	logger.Info("API server initialized", "addr", addr)
	return a
}

// Router exposes the gin engine, for tests.
func (a *APIServer) Router() *gin.Engine { return a.router }

// Start runs the server until ctx is cancelled. It satisfies manager.Runnable.
func (a *APIServer) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		err := a.Server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return a.Server.Shutdown(shutdownCtx)
	}
}

// NeedLeaderElection opts out of leader election so followers serve too. A
// Service in front of several replicas would otherwise refuse requests that
// land on a follower.
func (a *APIServer) NeedLeaderElection() bool { return false }

// GetClusterPlan returns the cached apply plan for a Cluster, with credentials
// masked.
//
// The plan carries each target's password and token straight from its
// credentials Secret, and this endpoint has no authentication of its own, so
// the raw plan must never be what goes on the wire. The reconciler hands out a
// redacted copy; the real one stays inside the process.
func (a *APIServer) GetClusterPlan(c *gin.Context) {
	uri, ok := parseURI(c)
	if !ok {
		return
	}
	logger := a.logger.WithValues("namespace", uri.Namespace, "cluster", uri.Name)
	plan, err := a.clusterReconciler.RedactedClusterPlan(uri.Namespace, uri.Name)
	if err != nil {
		logger.Info("no plan for cluster")
		c.String(http.StatusNotFound, err.Error())
		return
	}
	c.JSON(http.StatusOK, plan)
}

// RefreshTargetSource asks for an immediate discovery run. It authenticates,
// checks the debounce window, and annotates the TargetSource; the annotation
// change is what the controller reacts to. No target data is read from the
// body, so there is one desired set and it always comes from the source.
func (a *APIServer) RefreshTargetSource(c *gin.Context) {
	uri, ok := parseURI(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	logger := a.logger.WithValues("namespace", uri.Namespace, "targetsource", uri.Name)

	var ts gnmicv1alpha1.TargetSource
	if err := a.client.Get(ctx, types.NamespacedName{Namespace: uri.Namespace, Name: uri.Name}, &ts); err != nil {
		if apierrors.IsNotFound(err) {
			c.String(http.StatusNotFound, "not found")
			return
		}
		logger.Error(err, "failed to read TargetSource")
		c.String(http.StatusInternalServerError, "failed to read TargetSource")
		return
	}
	// Unknown and disabled look the same from outside, on purpose.
	if ts.Spec.Webhook == nil || !ts.Spec.Webhook.Enabled {
		c.String(http.StatusNotFound, "not found")
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBody+1))
	if err != nil || len(body) > maxWebhookBody {
		c.String(http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	if err := a.authenticate(ctx, c.Request, &ts, body); err != nil {
		logger.Info("refresh rejected", "reason", err.Error())
		c.String(http.StatusUnauthorized, "authentication failed")
		return
	}

	now := a.now()
	debounce := defaultDebounce
	if d := ts.Spec.Webhook.Debounce; d != nil && d.Duration > 0 {
		debounce = d.Duration
	}
	if last, ok := ts.Annotations[discovery.AnnotationRequestedAt]; ok {
		if t, err := time.Parse(time.RFC3339Nano, last); err == nil && now.Sub(t) < debounce {
			c.JSON(http.StatusOK, RefreshResponse{RequestedAt: t, Debounced: true})
			return
		}
	}

	base := ts.DeepCopy()
	if ts.Annotations == nil {
		ts.Annotations = map[string]string{}
	}
	ts.Annotations[discovery.AnnotationRequestedAt] = now.UTC().Format(time.RFC3339Nano)
	if err := a.client.Patch(ctx, &ts, client.MergeFrom(base)); err != nil {
		logger.Error(err, "failed to annotate TargetSource")
		c.String(http.StatusInternalServerError, "failed to request a run")
		return
	}
	logger.Info("refresh requested")
	c.JSON(http.StatusAccepted, RefreshResponse{RequestedAt: now.UTC(), Debounced: false})
}

// parseURI binds namespace and name from the path. It writes the 400 itself
// and reports false so the handler returns without a second response.
func parseURI(c *gin.Context) (urlStruct, bool) {
	var u urlStruct
	if err := c.ShouldBindUri(&u); err != nil {
		c.String(http.StatusBadRequest, "namespace and name are required")
		return u, false
	}
	return u, true
}
