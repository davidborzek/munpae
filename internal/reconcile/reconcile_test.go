package reconcile

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/davidborzek/munpae/internal/endpoint"
	"github.com/davidborzek/munpae/internal/plan"
)

type fakeSource struct{ eps []endpoint.Endpoint }

func (f fakeSource) Endpoints(context.Context) ([]endpoint.Endpoint, error) { return f.eps, nil }

type fakeProvider struct {
	current []endpoint.Endpoint
	applied *plan.Changes
	adjust  func([]endpoint.Endpoint) []endpoint.Endpoint
}

func (f *fakeProvider) Records(context.Context) ([]endpoint.Endpoint, error) { return f.current, nil }
func (f *fakeProvider) ApplyChanges(_ context.Context, c *plan.Changes) error {
	f.applied = c
	return nil
}
func (f *fakeProvider) AdjustEndpoints(_ context.Context, eps []endpoint.Endpoint) ([]endpoint.Endpoint, error) {
	if f.adjust != nil {
		return f.adjust(eps), nil
	}
	return eps, nil
}

func run(t *testing.T, src fakeSource, prov *fakeProvider, domains []string, defaultTarget, policy string) {
	t.Helper()
	r := New(src, prov, domains, defaultTarget, policy, 0, slog.Default()) // grace disabled keeps semantics of existing tests
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunFillsDefaultTargetAndCreates(t *testing.T) {
	src := fakeSource{eps: []endpoint.Endpoint{endpoint.New("a.example.com", nil, "", 0)}} // no target
	prov := &fakeProvider{}
	run(t, src, prov, []string{"example.com"}, "internal.example.com", "upsert-only")

	if prov.applied == nil || len(prov.applied.Create) != 1 {
		t.Fatalf("want 1 create, got %+v", prov.applied)
	}
	c := prov.applied.Create[0]
	if len(c.Targets) != 1 || c.Targets[0] != "internal.example.com" || c.RecordType != endpoint.TypeCNAME {
		t.Fatalf("default target not applied / wrong type: %+v", c)
	}
}

func TestRunDropsOutOfDomain(t *testing.T) {
	src := fakeSource{eps: []endpoint.Endpoint{endpoint.New("a.example.net", []string{"10.0.0.1"}, "", 0)}}
	prov := &fakeProvider{}
	run(t, src, prov, []string{"example.com"}, "", "sync")

	if prov.applied != nil {
		t.Fatalf("out-of-domain name must be dropped, got %+v", prov.applied)
	}
}

func TestRunSkipsNoTargetWithoutDefault(t *testing.T) {
	src := fakeSource{eps: []endpoint.Endpoint{endpoint.New("a.example.com", nil, "", 0)}}
	prov := &fakeProvider{}
	run(t, src, prov, nil, "", "sync")

	if prov.applied != nil {
		t.Fatalf("endpoint with no target and no default must be skipped, got %+v", prov.applied)
	}
}

func TestRunSyncDeletesStale(t *testing.T) {
	src := fakeSource{eps: []endpoint.Endpoint{endpoint.New("keep.example.com", []string{"10.0.0.1"}, "", 0)}}
	prov := &fakeProvider{current: []endpoint.Endpoint{
		endpoint.New("keep.example.com", []string{"10.0.0.1"}, "", 0),  // unchanged
		endpoint.New("stale.example.com", []string{"10.0.0.9"}, "", 0), // absent from desired
	}}
	run(t, src, prov, []string{"example.com"}, "", "sync")

	if prov.applied == nil || len(prov.applied.Delete) != 1 || prov.applied.Delete[0].DNSName != "stale.example.com" {
		t.Fatalf("sync must delete the stale record, got %+v", prov.applied)
	}
	if len(prov.applied.Create) != 0 || len(prov.applied.Update) != 0 {
		t.Fatalf("unchanged record must not create/update: %+v", prov.applied)
	}
}

func TestRunAdjustEndpoints(t *testing.T) {
	src := fakeSource{eps: []endpoint.Endpoint{endpoint.New("a.example.com", []string{"10.0.0.1"}, "", 0)}}
	prov := &fakeProvider{adjust: func([]endpoint.Endpoint) []endpoint.Endpoint {
		return []endpoint.Endpoint{endpoint.New("a.example.com", []string{"10.9.9.9"}, endpoint.TypeA, 0)}
	}}
	run(t, src, prov, []string{"example.com"}, "", "sync")
	if prov.applied == nil || len(prov.applied.Create) != 1 || prov.applied.Create[0].Targets[0] != "10.9.9.9" {
		t.Fatalf("adjusted endpoints must feed the plan: %+v", prov.applied)
	}
}

func TestRunGraceSuppressesDeleteUntilPeriodElapses(t *testing.T) {
	// Container vanishes but comes back within the grace window → never deleted.
	period := 5 * time.Minute
	prov := &fakeProvider{}
	r := New(fakeSource{eps: []endpoint.Endpoint{endpoint.New("app.example.com", []string{"10.0.0.1"}, "", 0)}},
		prov, []string{"example.com"}, "", "sync", period, slog.Default())
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if prov.applied == nil || len(prov.applied.Create) != 1 {
		t.Fatalf("want 1 create on first run, got %+v", prov.applied)
	}

	// Container disappears (restart): the owned record must not be deleted.
	prov.current = []endpoint.Endpoint{endpoint.New("app.example.com", []string{"10.0.0.1"}, "", 0)}
	r.src = fakeSource{}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if prov.applied != nil && len(prov.applied.Delete) != 0 {
		t.Fatalf("grace must suppress the delete while the container is briefly gone, got %+v", prov.applied)
	}

	// Container comes back → record recreated/updated, still no delete.
	prov.applied = nil
	r.src = fakeSource{eps: []endpoint.Endpoint{endpoint.New("app.example.com", []string{"10.0.0.1"}, "", 0)}}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if prov.applied != nil && len(prov.applied.Delete) != 0 {
		t.Fatalf("record reappeared: must not delete, got %+v", prov.applied)
	}
}

func TestRunGraceDeletesAfterPeriodElapses(t *testing.T) {
	// Container stays gone longer than the grace period → deleted.
	period := time.Millisecond
	prov := &fakeProvider{}
	r := New(fakeSource{eps: []endpoint.Endpoint{endpoint.New("app.example.com", []string{"10.0.0.1"}, "", 0)}},
		prov, []string{"example.com"}, "", "sync", period, slog.Default())
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	prov.current = []endpoint.Endpoint{endpoint.New("app.example.com", []string{"10.0.0.1"}, "", 0)}
	r.src = fakeSource{}
	time.Sleep(2 * time.Millisecond) // let the grace window elapse
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if prov.applied == nil || len(prov.applied.Delete) != 1 {
		t.Fatalf("after the grace period a vanished record must be deleted, got %+v", prov.applied)
	}
}

func TestRunStartupGraceSeedsFromOwnedRecords(t *testing.T) {
	// munpae restarts while a container is mid-recreate: the owned record must
	// survive the first reconcile (seeded from current records), then be deleted
	// once the grace window passes if the container never comes back.
	period := 5 * time.Minute
	prov := &fakeProvider{current: []endpoint.Endpoint{endpoint.New("app.example.com", []string{"10.0.0.1"}, "", 0)}}
	r := New(fakeSource{}, prov, []string{"example.com"}, "", "sync", period, slog.Default())

	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if prov.applied != nil && len(prov.applied.Delete) != 0 {
		t.Fatalf("startup grace must protect owned records, got %+v", prov.applied)
	}
}
