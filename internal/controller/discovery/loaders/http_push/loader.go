package http_push

import (
	"context"
	"fmt"

	"github.com/bytedance/gopkg/util/logger"
	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Loader struct{}

// New instantiates the http_pull loader
func New() core.Loader {
	return &Loader{}
}

func (l *Loader) Name() string {
	return "push"
}

func (l *Loader) Start(
	ctx context.Context,
	targetsourceName string,
	spec gnmicv1alpha1.TargetSourceSpec,
	out chan<- []core.DiscoveryMessage,
) error {
	logger := log.FromContext(ctx).WithValues("loader", l.Name())
	logger.Info("Push loader started")

	return nil
}

func SendTargetToLoader(dm []core.DiscoveryMessage) {
	logger.Info("SendTargetToLoader %s", dm)
	// for _, target := range payload {
	// 	if target.Name != nil {
	// 		fmt.Printf("name: %s, ", *target.Name)
	// 	}
	// 	if target.Address != nil {
	// 		fmt.Printf("address: %s, ", *target.Address)
	// 	}
	// 	if target.Profile != nil {
	// 		fmt.Printf("profile: %s, ", *target.Profile)
	// 	}
	// 	if target.Tags != nil {
	// 		fmt.Printf("tags: %s", *target.Tags)
	// 	}
	fmt.Printf("SentTargetToLoader called")
	//}
}
