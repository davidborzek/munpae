package provider

import (
	"context"
	"testing"

	"github.com/cloudflare/cloudflare-go"

	"github.com/davidborzek/munpae/internal/endpoint"
	"github.com/davidborzek/munpae/internal/plan"
)

// fakeCF implements cfAPI. ListDNSRecords pages by params.Page over f.pages.
type fakeCF struct {
	zones   []cloudflare.Zone
	pages   [][]cloudflare.DNSRecord
	created []cloudflare.CreateDNSRecordParams
	updated []cloudflare.UpdateDNSRecordParams
	deleted []string
}

func (f *fakeCF) ListZones(context.Context, ...string) ([]cloudflare.Zone, error) {
	return f.zones, nil
}

func (f *fakeCF) ListDNSRecords(_ context.Context, _ *cloudflare.ResourceContainer, params cloudflare.ListDNSRecordsParams) ([]cloudflare.DNSRecord, *cloudflare.ResultInfo, error) {
	page := params.Page
	if page == 0 {
		page = 1
	}
	info := &cloudflare.ResultInfo{Page: page, PerPage: 1, TotalPages: len(f.pages)}
	if page-1 >= len(f.pages) {
		return nil, info, nil
	}
	return f.pages[page-1], info, nil
}

func (f *fakeCF) CreateDNSRecord(_ context.Context, _ *cloudflare.ResourceContainer, params cloudflare.CreateDNSRecordParams) (cloudflare.DNSRecord, error) {
	f.created = append(f.created, params)
	return cloudflare.DNSRecord{}, nil
}

func (f *fakeCF) UpdateDNSRecord(_ context.Context, _ *cloudflare.ResourceContainer, params cloudflare.UpdateDNSRecordParams) (cloudflare.DNSRecord, error) {
	f.updated = append(f.updated, params)
	return cloudflare.DNSRecord{}, nil
}

func (f *fakeCF) DeleteDNSRecord(_ context.Context, _ *cloudflare.ResourceContainer, recordID string) error {
	f.deleted = append(f.deleted, recordID)
	return nil
}

func TestRecordsPaginationAndMerge(t *testing.T) {
	f := &fakeCF{
		zones: []cloudflare.Zone{{ID: "z1", Name: "example.com"}},
		pages: [][]cloudflare.DNSRecord{
			{{Type: "A", Name: "rr.example.com", Content: "10.0.0.1", TTL: 300, Proxied: new(false)}},
			{
				{Type: "A", Name: "rr.example.com", Content: "10.0.0.2", TTL: 300, Proxied: new(false)},
				{Type: "CNAME", Name: "c.example.com", Content: "anchor.example.com", TTL: 1, Proxied: new(true)},
			},
		},
	}
	c := &Cloudflare{api: f}
	eps, err := c.Records(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]endpoint.Endpoint{}
	for _, e := range eps {
		byKey[e.Key()] = e
	}
	// Page 2 must have been fetched — proves pagination.
	cn, ok := byKey["CNAME/c.example.com"]
	if !ok {
		t.Fatal("second page not fetched — pagination missing")
	}
	// Two A records for one name merge into one endpoint with two targets.
	if a := byKey["A/rr.example.com"]; len(a.Targets) != 2 {
		t.Fatalf("multi-value A must merge into one endpoint: %+v", a)
	}
	// Proxied flag round-trips into the label.
	if cn.Labels["cloudflare-proxied"] != "true" {
		t.Fatalf("proxied CNAME label = %q, want true", cn.Labels["cloudflare-proxied"])
	}
	if byKey["A/rr.example.com"].Labels["cloudflare-proxied"] != "false" {
		t.Fatalf("unproxied A label = %q, want false", byKey["A/rr.example.com"].Labels["cloudflare-proxied"])
	}
}

func TestProxiedResolution(t *testing.T) {
	labeled := func(v string) endpoint.Endpoint {
		e := endpoint.New("x.example.com", []string{"1.2.3.4"}, endpoint.TypeA, 0)
		e.Labels = map[string]string{"cloudflare-proxied": v}
		return e
	}
	if !(&Cloudflare{proxied: false}).proxiedFor(labeled("true")) {
		t.Error("label true must override global false")
	}
	if (&Cloudflare{proxied: true}).proxiedFor(labeled("false")) {
		t.Error("label false must override global true")
	}
	plainA := endpoint.New("x.example.com", []string{"1.2.3.4"}, endpoint.TypeA, 0)
	if !(&Cloudflare{proxied: true}).proxiedFor(plainA) {
		t.Error("no label must fall back to the global default")
	}
	txt := endpoint.New("x.example.com", []string{"v"}, endpoint.TypeTXT, 0)
	txt.Labels = map[string]string{"cloudflare-proxied": "true"}
	if (&Cloudflare{proxied: true}).proxiedFor(txt) {
		t.Error("TXT must never be proxied")
	}
}

func TestParamsProxyAndTTL(t *testing.T) {
	proxied := &Cloudflare{proxied: true}
	cn := endpoint.New("app.example.com", []string{"anchor.example.com"}, endpoint.TypeCNAME, 300)
	p := proxied.createParams(cn, cn.Targets[0])
	if p.Proxied == nil || !*p.Proxied || p.TTL != 1 || p.Content != "anchor.example.com" {
		t.Fatalf("proxied CNAME: proxied=%v ttl=%d content=%s", p.Proxied, p.TTL, p.Content)
	}
	plain := &Cloudflare{proxied: false}
	pa := plain.createParams(endpoint.New("h.example.com", []string{"192.0.2.1"}, endpoint.TypeA, 120), "192.0.2.1")
	if pa.Proxied == nil || *pa.Proxied || pa.TTL != 120 {
		t.Fatalf("unproxied A: proxied=%v ttl=%d", pa.Proxied, pa.TTL)
	}
}

func TestApplyCreatePerTarget(t *testing.T) {
	f := &fakeCF{zones: []cloudflare.Zone{{ID: "z1", Name: "example.com"}}}
	c := &Cloudflare{api: f}
	e := endpoint.New("multi.example.com", []string{"10.0.0.1", "10.0.0.2"}, endpoint.TypeA, 0)
	if err := c.ApplyChanges(context.Background(), &plan.Changes{Create: []endpoint.Endpoint{e}}); err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 2 {
		t.Fatalf("multi-target create must emit one record per target, got %d", len(f.created))
	}
}

func TestApplyUpdateInPlaceAndConverge(t *testing.T) {
	// One existing record + one desired target → in-place update, no delete.
	f := &fakeCF{
		zones: []cloudflare.Zone{{ID: "z1", Name: "example.com"}},
		pages: [][]cloudflare.DNSRecord{{{Type: "A", Name: "s.example.com", Content: "10.0.0.1", ID: "r1"}}},
	}
	c := &Cloudflare{api: f}
	upd := endpoint.New("s.example.com", []string{"10.0.0.9"}, endpoint.TypeA, 0)
	if err := c.ApplyChanges(context.Background(), &plan.Changes{Update: []endpoint.Endpoint{upd}}); err != nil {
		t.Fatal(err)
	}
	if len(f.updated) != 1 || len(f.deleted) != 0 || f.updated[0].Content != "10.0.0.9" {
		t.Fatalf("1<->1 update must be in-place: updated=%d deleted=%d", len(f.updated), len(f.deleted))
	}

	// Two existing records + one desired target → converge (delete both, create one).
	f2 := &fakeCF{
		zones: []cloudflare.Zone{{ID: "z1", Name: "example.com"}},
		pages: [][]cloudflare.DNSRecord{{
			{Type: "A", Name: "m.example.com", Content: "10.0.0.1", ID: "r1"},
			{Type: "A", Name: "m.example.com", Content: "10.0.0.2", ID: "r2"},
		}},
	}
	c2 := &Cloudflare{api: f2}
	upd2 := endpoint.New("m.example.com", []string{"10.0.0.9"}, endpoint.TypeA, 0)
	if err := c2.ApplyChanges(context.Background(), &plan.Changes{Update: []endpoint.Endpoint{upd2}}); err != nil {
		t.Fatal(err)
	}
	if len(f2.deleted) != 2 || len(f2.created) != 1 {
		t.Fatalf("multi->single update must converge: deleted=%d created=%d", len(f2.deleted), len(f2.created))
	}
}

func TestApplyDeleteRemovesAll(t *testing.T) {
	f := &fakeCF{
		zones: []cloudflare.Zone{{ID: "z1", Name: "example.com"}},
		pages: [][]cloudflare.DNSRecord{{
			{Type: "A", Name: "d.example.com", Content: "10.0.0.1", ID: "r1"},
			{Type: "A", Name: "d.example.com", Content: "10.0.0.2", ID: "r2"},
		}},
	}
	c := &Cloudflare{api: f}
	e := endpoint.New("d.example.com", []string{"10.0.0.1"}, endpoint.TypeA, 0)
	if err := c.ApplyChanges(context.Background(), &plan.Changes{Delete: []endpoint.Endpoint{e}}); err != nil {
		t.Fatal(err)
	}
	if len(f.deleted) != 2 {
		t.Fatalf("delete must remove all records for the key, got %d", len(f.deleted))
	}
}

func TestLongestZone(t *testing.T) {
	zones := []string{"example.com", "sub.example.com"}
	cases := map[string]string{
		"x.sub.example.com": "sub.example.com",
		"y.example.com":     "example.com",
		"example.com":       "example.com",
		"a.example.net":     "",
	}
	for name, want := range cases {
		if got := longestZone(name, zones); got != want {
			t.Errorf("longestZone(%q) = %q, want %q", name, got, want)
		}
	}
}
