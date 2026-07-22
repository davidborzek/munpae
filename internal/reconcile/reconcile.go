// Package reconcile runs one desired→diff→apply cycle: collect endpoints from
// the sources, resolve/scope them, diff against the provider, and apply.
package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/davidborzek/munpae/internal/endpoint"
	"github.com/davidborzek/munpae/internal/plan"
	"github.com/davidborzek/munpae/internal/provider"
	"github.com/davidborzek/munpae/internal/source"
)

// Reconciler owns one source→provider pipeline.
type Reconciler struct {
	src           source.Source
	prov          provider.Provider
	domainFilter  []string
	defaultTarget string
	policy        string
	log           *slog.Logger
}

// New returns a Reconciler.
func New(src source.Source, prov provider.Provider, domainFilter []string, defaultTarget, policy string, log *slog.Logger) *Reconciler {
	return &Reconciler{src: src, prov: prov, domainFilter: domainFilter, defaultTarget: defaultTarget, policy: policy, log: log}
}

// Result summarizes one reconcile run for instrumentation.
type Result struct {
	Managed int // desired records munpae manages after scoping
	Create  int
	Update  int
	Delete  int
}

// Run performs one reconcile and reports the resulting counts.
func (r *Reconciler) Run(ctx context.Context) (Result, error) {
	desired, err := r.src.Endpoints(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("listing sources: %w", err)
	}
	desired = r.prepare(desired)
	if adj, ok := r.prov.(provider.EndpointAdjuster); ok {
		if desired, err = adj.AdjustEndpoints(ctx, desired); err != nil {
			return Result{}, fmt.Errorf("adjusting endpoints: %w", err)
		}
	}

	current, err := r.prov.Records(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("reading provider records: %w", err)
	}
	current = r.scope(current)

	changes := plan.Calculate(desired, current, r.policy)
	res := Result{Managed: len(desired), Create: len(changes.Create), Update: len(changes.Update), Delete: len(changes.Delete)}
	if changes.Empty() {
		r.log.Debug("no changes")
		return res, nil
	}
	r.log.Info("applying", "create", res.Create, "update", res.Update, "delete", res.Delete)
	if err := r.prov.ApplyChanges(ctx, changes); err != nil {
		return res, err
	}
	return res, nil
}

// prepare fills the default target when a source yielded none, drops endpoints
// still without a target, and scopes to the managed domains.
func (r *Reconciler) prepare(eps []endpoint.Endpoint) []endpoint.Endpoint {
	out := make([]endpoint.Endpoint, 0, len(eps))
	for _, e := range eps {
		if len(e.Targets) == 0 {
			if r.defaultTarget == "" {
				r.log.Warn("endpoint has no target and no default; skipping", "name", e.DNSName)
				continue
			}
			e = endpoint.New(e.DNSName, []string{r.defaultTarget}, "", e.TTL)
		}
		if r.inDomain(e.DNSName) {
			out = append(out, e)
		}
	}
	return out
}

func (r *Reconciler) scope(eps []endpoint.Endpoint) []endpoint.Endpoint {
	out := make([]endpoint.Endpoint, 0, len(eps))
	for _, e := range eps {
		if r.inDomain(e.DNSName) {
			out = append(out, e)
		}
	}
	return out
}

func (r *Reconciler) inDomain(name string) bool {
	if len(r.domainFilter) == 0 {
		return true
	}
	n := strings.TrimSuffix(name, ".")
	for _, z := range r.domainFilter {
		z = strings.TrimSuffix(z, ".")
		if n == z || strings.HasSuffix(n, "."+z) {
			return true
		}
	}
	return false
}
