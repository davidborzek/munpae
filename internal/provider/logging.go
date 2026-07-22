package provider

import (
	"context"
	"log/slog"

	"github.com/davidborzek/munpae/internal/endpoint"
	"github.com/davidborzek/munpae/internal/plan"
)

// Logging is the `--dry-run` backend: it observes nothing and only logs the
// changes it would make, touching no DNS server.
type Logging struct{ log *slog.Logger }

// NewLogging returns the dry-run provider.
func NewLogging(log *slog.Logger) *Logging { return &Logging{log: log} }

// Records reports an empty backend, so a dry run shows every desired record as
// a create.
func (l *Logging) Records(context.Context) ([]endpoint.Endpoint, error) { return nil, nil }

// ApplyChanges logs the plan instead of applying it.
func (l *Logging) ApplyChanges(_ context.Context, ch *plan.Changes) error {
	log := func(op string, eps []endpoint.Endpoint) {
		for _, e := range eps {
			l.log.Info("dry-run "+op, "name", e.DNSName, "type", string(e.RecordType), "targets", e.Targets, "ttl", e.TTL)
		}
	}
	log("CREATE", ch.Create)
	log("UPDATE", ch.Update)
	log("DELETE", ch.Delete)
	return nil
}
