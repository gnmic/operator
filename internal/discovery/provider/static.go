package provider

import (
	"context"
	"maps"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// staticProvider takes the inventory from the spec itself. It exists so the
// naming, pruning and conflict logic can be tested and demonstrated without
// any external system, and so a small fixed lab needs no second resource.
type staticProvider struct{}

func init() { Register(staticProvider{}) }

func (staticProvider) Type() gnmicv1alpha1.SourceType { return gnmicv1alpha1.SourceTypeStatic }

func (staticProvider) Fetch(_ context.Context, req Request) (Result, error) {
	src := req.Source.Static
	if src == nil {
		return Result{}, Specf("source.static is not set")
	}
	devices := make([]Device, 0, len(src.Devices))
	for _, d := range src.Devices {
		devices = append(devices, Device{
			Name:    d.Name,
			Address: d.Address,
			Port:    d.Port,
			Profile: d.Profile,
			Labels:  maps.Clone(d.Labels),
		})
	}
	return Result{Devices: devices}, nil
}
