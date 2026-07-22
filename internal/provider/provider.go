// Package provider reads and writes records in a DNS backend.
package provider

import (
	"context"

	"github.com/davidborzek/munpae/internal/endpoint"
	"github.com/davidborzek/munpae/internal/plan"
)

// Provider reads the current records and applies a set of changes. Adding a
// backend is one implementation.
type Provider interface {
	// Records returns the records currently in the backend (within munpae's
	// managed scope).
	Records(ctx context.Context) ([]endpoint.Endpoint, error)
	// ApplyChanges creates/updates/deletes records per the plan.
	ApplyChanges(ctx context.Context, changes *plan.Changes) error
}

// EndpointAdjuster is an optional Provider capability mirroring external-dns'
// AdjustEndpoints: normalize desired endpoints before planning. Providers that
// don't implement it are treated as identity.
type EndpointAdjuster interface {
	AdjustEndpoints(ctx context.Context, eps []endpoint.Endpoint) ([]endpoint.Endpoint, error)
}
