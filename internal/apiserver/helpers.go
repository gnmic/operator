package apiserver

import (
	"fmt"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// createDiscoveryEvent creates object of type core.DiscoveryEvent
func createDiscoveryEvent(payloadTargets []Target) ([]core.DiscoveryEvent, error) {
	targets := []core.DiscoveryEvent{}

	if len(payloadTargets) > 0 {
		for i, target := range payloadTargets {
			if target.Name == "" {
				return nil, fmt.Errorf("Target receieved at index %d by pull interface has no Name.", i)
			}
			if target.Address == "" {
				return nil, fmt.Errorf("Target receieved at index %d by pull interface has no Ip.", i)
			}
			event, err := getEvent(target, i)
			if err != nil {
				return nil, err
			}

			targets = append(targets, core.DiscoveryEvent{
				Target: core.DiscoveredTarget{
					Name:          target.Name,
					Address:       target.Address,
					Port:          int32(*target.Port),
					Labels:        convertTargetLabelsToMap(target),
					TargetProfile: *target.TargetProfile,
				},
				Event: event,
			})
		}
	}
	return targets, nil
}

// validateAddress
func validateAddress(address string) (string, error) {
	address, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	if port == "" {
		port = "57400"
	}
	return address + ":" + port, nil
}

// getKey returns key for used to identify correct channel in DiscoveryRegistry
func getKey(u urlStruct) types.NamespacedName {
	key := types.NamespacedName{
		Namespace: u.Namespace,
		Name:      u.Name,
	}
	// or kubectl get secret -n gnmic-system gnmic-api-auth -o jsonpath="{.data.bearer-token}" | base64 --decode
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
func getEvent(target Target, index int) (core.EventAction, error) {
	event := core.EventApply
	switch target.Operation {
	case Created:
		event = core.EventApply
	case Updated:
		event = core.EventApply
	case Deleted:
		event = core.EventDelete
	default:
		return event, fmt.Errorf("Target receieved at index %d by pull interface has no valid Operation", index)
	}
	return event, nil
}

// parseURI parses URI to urlStruct.
func parseURI(c *gin.Context) (url urlStruct) {
	logger := log.FromContext(c.Request.Context()).WithValues("component", "apiserver", "action", "parse-uri")
	var u urlStruct
	if err := c.ShouldBindUri(&u); err != nil {
		logger.Error(err, "Failed to bind request URI")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	return u
}
