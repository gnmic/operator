package apiserver

import (
	"context"
	"net/http"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/gnmic/operator/internal/controller/discovery/loaders/utils"
	"k8s.io/apimachinery/pkg/types"
)

// CreateTargets binds payload to payloadTargets struct defined in openapi contract.
// Creates a []core.DiscoveryEvent sends it to the core package.
func (a *APIServer) CreateTargets(c *gin.Context) {
	namespace := c.Param("namespace")
	name := c.Param("name")
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	registry, ok := a.DiscoveryRegistry.Get(key)
	if !ok || (registry.LoaderConfig.AcceptPush != true) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "TargetSource not active or does not exist",
		})
		return
	}

	var payloadTargets Targets
	if err := c.ShouldBind(&payloadTargets); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	targets := []core.DiscoveryEvent{}
	if len(payloadTargets.TargetList) > 0 {
		for i, target := range payloadTargets.TargetList {
			if target.Address == "" || target.Name == "" || target.Operation == "" {
				logger.Warn("Target receieved at index", i, " by pull interface does not contain Address, Name or Operation and is skipped.")
				break
			}
			if target.Operation.Valid() != true {
				logger.Warn("Target receieved at index", i, " by pull interface has invalid Operation.")
				break
			}

			event := core.EventApply
			switch target.Operation {
			case Create:
				event = core.EventApply
			case Delete:
				event = core.EventDelete
			}

			targets = append(targets, core.DiscoveryEvent{
				Target: core.DiscoveredTarget{
					Name:    target.Name,
					Address: target.Address,
					Labels:  map[string]string{"key": "Is this a tag?"},
				},
				Event: event,
			})
		}
	}

	utils.SendEvents(context.Background(), registry.Channel, targets, a.ChunkSize)
	c.JSON(http.StatusOK, payloadTargets)
}
