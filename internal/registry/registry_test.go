package registry

import (
	"context"
	"strings"
	"testing"

	"github.com/davidborzek/munpae/internal/endpoint"
	"github.com/davidborzek/munpae/internal/plan"
)

type fakeProvider struct {
	records []endpoint.Endpoint
	applied *plan.Changes
}

func (f *fakeProvider) Records(context.Context) ([]endpoint.Endpoint, error) { return f.records, nil }
func (f *fakeProvider) ApplyChanges(_ context.Context, c *plan.Changes) error {
	f.applied = c
	return nil
}

// ownTXT builds the ownership TXT a given owner would write for e.
func ownTXT(owner string, e endpoint.Endpoint) endpoint.Endpoint {
	return NewTXT(nil, owner, "").ownership(e)
}

func TestRecordsFiltersToOwned(t *testing.T) {
	app := endpoint.New("app.example.com", []string{"192.0.2.1"}, endpoint.TypeA, 0)
	foreign := endpoint.New("foreign.example.com", []string{"192.0.2.9"}, endpoint.TypeA, 0)
	other := endpoint.New("other.example.com", []string{"192.0.2.8"}, endpoint.TypeA, 0)
	inner := &fakeProvider{records: []endpoint.Endpoint{
		app, foreign, other,
		ownTXT("me", app),        // owned by us
		ownTXT("someone", other), // owned by a different id
	}}

	got, err := NewTXT(inner, "me", "").Records(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].DNSName != "app.example.com" {
		t.Fatalf("only our owned record should surface (foreign + other-owner excluded), got %+v", got)
	}
}

func TestApplyAddsAndRemovesOwnershipTXT(t *testing.T) {
	inner := &fakeProvider{}
	r := NewTXT(inner, "me", "")
	create := endpoint.New("app.example.com", []string{"192.0.2.1"}, endpoint.TypeA, 0)
	del := endpoint.New("gone.example.com", []string{"192.0.2.2"}, endpoint.TypeA, 0)

	if err := r.ApplyChanges(context.Background(), &plan.Changes{
		Create: []endpoint.Endpoint{create},
		Delete: []endpoint.Endpoint{del},
	}); err != nil {
		t.Fatal(err)
	}

	if len(inner.applied.Create) != 2 || len(inner.applied.Delete) != 2 {
		t.Fatalf("each create/delete must carry its ownership TXT: %+v", inner.applied)
	}
	var txt *endpoint.Endpoint
	for i := range inner.applied.Create {
		if inner.applied.Create[i].RecordType == endpoint.TypeTXT {
			txt = &inner.applied.Create[i]
		}
	}
	if txt == nil || txt.DNSName != "a-app.example.com" || !strings.Contains(txt.Targets[0], "munpae/owner=me") {
		t.Fatalf("ownership TXT wrong: %+v", txt)
	}
}

func TestRealKeyRoundTrip(t *testing.T) {
	r := NewTXT(nil, "me", "")
	// a name containing '-' exercises the "type has no dash" invariant.
	e := endpoint.New("my-app.example.com", []string{"anchor.example.com"}, endpoint.TypeCNAME, 0)
	name := r.txtName(e)
	if name != "cname-my-app.example.com" {
		t.Fatalf("txtName = %q", name)
	}
	key, ok := r.realKey(name)
	if !ok || key != e.Key() {
		t.Fatalf("realKey = %q ok=%v, want %q", key, ok, e.Key())
	}
}

func TestRealKeyWithPrefix(t *testing.T) {
	r := NewTXT(nil, "me", "munpae.")
	e := endpoint.New("app.example.com", []string{"192.0.2.1"}, endpoint.TypeA, 0)
	if name := r.txtName(e); name != "munpae.a-app.example.com" {
		t.Fatalf("txtName = %q", name)
	}
	key, ok := r.realKey("munpae.a-app.example.com")
	if !ok || key != e.Key() {
		t.Fatalf("realKey = %q ok=%v, want %q", key, ok, e.Key())
	}
	// A name missing the configured prefix is not one of ours.
	if _, ok := r.realKey("a-app.example.com"); ok {
		t.Fatal("unprefixed TXT name must not be recognized as ours")
	}
}

type fakeAdjuster struct {
	fakeProvider
	adjusted bool
}

func (f *fakeAdjuster) AdjustEndpoints(_ context.Context, eps []endpoint.Endpoint) ([]endpoint.Endpoint, error) {
	f.adjusted = true
	return eps, nil
}

func TestAdjustEndpointsDelegation(t *testing.T) {
	// Inner supports the hook → registry forwards to it.
	adj := &fakeAdjuster{}
	if _, err := NewTXT(adj, "me", "").AdjustEndpoints(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !adj.adjusted {
		t.Fatal("registry must forward AdjustEndpoints to an adjuster inner")
	}
	// Inner without the hook → identity.
	in := []endpoint.Endpoint{endpoint.New("a.example.com", []string{"10.0.0.1"}, endpoint.TypeA, 0)}
	out, err := NewTXT(&fakeProvider{}, "me", "").AdjustEndpoints(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].DNSName != "a.example.com" {
		t.Fatalf("non-adjuster inner must be identity: %+v", out)
	}
}
