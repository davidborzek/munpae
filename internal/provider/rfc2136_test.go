package provider

import (
	"net"
	"testing"

	"github.com/miekg/dns"

	"github.com/davidborzek/munpae/internal/endpoint"
)

func TestRRs(t *testing.T) {
	a := rrs(endpoint.New("a.example.com", []string{"10.0.0.1"}, endpoint.TypeA, 300))
	if len(a) != 1 {
		t.Fatalf("want 1 RR, got %d", len(a))
	}
	rec, ok := a[0].(*dns.A)
	if !ok || rec.Hdr.Name != "a.example.com." || rec.Hdr.Ttl != 300 || rec.A.String() != "10.0.0.1" {
		t.Fatalf("A record wrong: %v", a[0])
	}

	// CNAME target must be FQDN; TTL 0 defaults to 300.
	c := rrs(endpoint.New("x.example.com", []string{"anchor.example.com"}, endpoint.TypeCNAME, 0))
	cn, ok := c[0].(*dns.CNAME)
	if !ok || cn.Target != "anchor.example.com." || cn.Hdr.Ttl != 300 {
		t.Fatalf("CNAME record wrong: %v", c[0])
	}
}

func TestToEndpoint(t *testing.T) {
	a := &dns.A{
		Hdr: dns.RR_Header{Name: "a.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("10.0.0.1"),
	}
	e, ok := toEndpoint(a)
	if !ok || e.DNSName != "a.example.com" || e.RecordType != endpoint.TypeA || len(e.Targets) != 1 || e.Targets[0] != "10.0.0.1" {
		t.Fatalf("A → endpoint wrong: %+v ok=%v", e, ok)
	}

	if _, ok := toEndpoint(&dns.SOA{Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA}}); ok {
		t.Fatal("SOA must be skipped")
	}
}

func TestTSIGAlg(t *testing.T) {
	if tsigAlg("hmac-sha256") != dns.HmacSHA256 || tsigAlg("") != dns.HmacSHA256 {
		t.Fatal("hmac-sha256 / default")
	}
	if tsigAlg("hmac-sha512") != dns.HmacSHA512 {
		t.Fatal("hmac-sha512")
	}
}
