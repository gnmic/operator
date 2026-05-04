package apiserver

import (
	"fmt"
	"net/http"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"k8s.io/apimachinery/pkg/types"
)

// createDiscoveryEvent creates object of type core.DiscoveryEvent
func createDiscoveryEvent(payloadTargets []Target) []core.DiscoveryEvent {
	targets := []core.DiscoveryEvent{}
	if len(payloadTargets) > 0 {
		for i, target := range payloadTargets {
			if target.Name == "" {
				// no REST API return here as not all targets might be incomplete
				err := fmt.Errorf("Target receieved at index %d by pull interface has no Name and is skipped.", i)
				logger.Error(err, "Failed creating DiscoveryEvent")
				break
			}
			if target.Address == "" {
				err := fmt.Errorf("Target receieved at index %d by pull interface has no Address and is skipped.", i)
				logger.Error(err, "Failed creating DiscoveryEvent")
				break
			}

			targets = append(targets, core.DiscoveryEvent{
				Target: core.DiscoveredTarget{
					Name:    target.Name,
					Address: target.Address,
					Labels:  convertTargetLabelsToMap(target),
				},
				Event: getEvent(target, i),
			})
		}
	}
	return targets
}

// getKey returns key for used to identify correct channel in DiscoveryRegistry
func getKey(u urlStruct) types.NamespacedName {
	key := types.NamespacedName{
		Namespace: u.Namespace,
		Name:      u.Name,
	}
	return key
}

// convertTargetLabelsToMap converts target.Labels to map.
func convertTargetLabelsToMap(target Target) map[string]string {
	labelToMap := make(map[string]string)
	if target.Labels != nil {
		for _, tag := range *target.Labels {
			if tag.Key == nil || tag.Value == nil || *tag.Key == "" {
				continue
			}
			labelToMap[*tag.Key] = *tag.Value
		}
	}
	return labelToMap
}

// getEvent converts target.Operation to core.Operation.
func getEvent(target Target, index int) core.EventAction {
	event := core.EventApply
	switch target.Operation {
	case Created:
		event = core.EventApply
	case Updated:
		event = core.EventApply
	case Deleted:
		event = core.EventDelete
	default:
		err := fmt.Errorf("Target receieved at index %d by pull interface has no valid Operation and is skipped.", index)
		logger.Error(err, "Failed creating DiscoveryEvent")
	}
	return event
}

// parseURI parses URI to urlStruct.
func parseURI(c *gin.Context) (url urlStruct) {
	var u urlStruct
	if err := c.ShouldBindUri(&u); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	return u
}
