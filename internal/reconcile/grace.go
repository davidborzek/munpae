package reconcile

import (
	"time"

	"github.com/davidborzek/munpae/internal/endpoint"
)

// graceState remembers which records munpae has recently managed so that a
// briefly-vanished endpoint — a container restart, a compose recreate, or
// munpae's own downtime — is not deleted the instant it drops out of the
// desired set. A vanished endpoint's record is kept (re-protected on each
// reconcile it is still absent) until the grace period has passed without the
// endpoint reappearing, after which it is eligible for deletion again.
//
// The grace set is seeded once from the provider's current owned records, so a
// fresh munpae process does not immediately delete records for containers that
// disappeared while munpae itself was down (e.g. mid-recreate); they get the
// same full grace window before any deletion is considered.
type graceState struct {
	period time.Duration
	// lastSeen maps an endpoint Key() to the last time it was present in the
	// desired set (or seeded from owned records at startup).
	lastSeen map[string]time.Time
	seeded   bool
}

func newGraceState(period time.Duration) *graceState {
	return &graceState{period: period, lastSeen: map[string]time.Time{}}
}

// seed records that munpae already owns (the provider's current records) as
// recently present. Only the first reconcile seeds, so this is exactly the
// "startup grace": owned records get a full grace window before deletion.
func (g *graceState) seed(now time.Time, eps []endpoint.Endpoint) {
	for _, e := range eps {
		k := e.Key()
		if _, ok := g.lastSeen[k]; !ok {
			g.lastSeen[k] = now
		}
	}
}

// observe marks the currently-desired endpoints as present, refreshing their
// protection so an endpoint that is still there never expires.
func (g *graceState) observe(now time.Time, eps []endpoint.Endpoint) {
	for _, e := range eps {
		g.lastSeen[e.Key()] = now
	}
}

// prune drops endpoints that have been absent for longer than the grace period.
func (g *graceState) prune(now time.Time) {
	for k, last := range g.lastSeen {
		if now.Sub(last) > g.period {
			delete(g.lastSeen, k)
		}
	}
}

// protected reports whether the endpoint key is still within its grace window.
func (g *graceState) protected(key string) bool {
	_, ok := g.lastSeen[key]
	return ok
}

// filterDeletes drops any pending deletion whose endpoint is still under grace.
func (g *graceState) filterDeletes(del []endpoint.Endpoint) []endpoint.Endpoint {
	if len(del) == 0 {
		return del
	}
	out := del[:0]
	for _, e := range del {
		if !g.protected(e.Key()) {
			out = append(out, e)
		}
	}
	return out
}
