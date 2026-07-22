package provider

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/davidborzek/munpae/internal/endpoint"
	"github.com/davidborzek/munpae/internal/plan"
)

// RFC2136 programs a DNS zone via dynamic UPDATE (RFC 2136) with TSIG, using
// miekg/dns directly. Reads the current records via AXFR.
type RFC2136 struct {
	addr    string // host:port
	zone    string // fqdn
	keyName string // fqdn
	secret  string // base64 TSIG secret
	alg     string // dns.Hmac* constant
}

// NewRFC2136 builds the provider from connection settings.
func NewRFC2136(host, port, zone, keyName, secret, algorithm string) (*RFC2136, error) {
	if host == "" || zone == "" || keyName == "" || secret == "" {
		return nil, fmt.Errorf("rfc2136: host, zone, TSIG keyname and secret are required")
	}
	return &RFC2136{
		addr:    net.JoinHostPort(host, port),
		zone:    dns.Fqdn(zone),
		keyName: dns.Fqdn(keyName),
		secret:  secret,
		alg:     tsigAlg(algorithm),
	}, nil
}

// Records lists the zone via AXFR and maps A/AAAA/CNAME/TXT into endpoints.
func (p *RFC2136) Records(_ context.Context) ([]endpoint.Endpoint, error) {
	t := &dns.Transfer{TsigSecret: map[string]string{p.keyName: p.secret}}
	m := new(dns.Msg)
	m.SetAxfr(p.zone)
	m.SetTsig(p.keyName, p.alg, 300, time.Now().Unix())
	ch, err := t.In(m, p.addr)
	if err != nil {
		return nil, fmt.Errorf("rfc2136 AXFR: %w", err)
	}
	byKey := map[string]*endpoint.Endpoint{}
	var order []string
	for env := range ch {
		if env.Error != nil {
			return nil, fmt.Errorf("rfc2136 AXFR: %w", env.Error)
		}
		for _, rr := range env.RR {
			e, ok := toEndpoint(rr)
			if !ok {
				continue
			}
			if cur := byKey[e.Key()]; cur != nil {
				cur.Targets = append(cur.Targets, e.Targets...)
				continue
			}
			ne := e
			byKey[e.Key()] = &ne
			order = append(order, e.Key())
		}
	}
	out := make([]endpoint.Endpoint, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out, nil
}

// ApplyChanges sends one dynamic UPDATE with all creates/updates/deletes.
func (p *RFC2136) ApplyChanges(ctx context.Context, ch *plan.Changes) error {
	m := new(dns.Msg)
	m.SetUpdate(p.zone)
	for _, e := range ch.Create {
		m.Insert(rrs(e))
	}
	for _, e := range ch.Update {
		m.RemoveRRset(rrs(e))
		m.Insert(rrs(e))
	}
	for _, e := range ch.Delete {
		m.RemoveRRset(rrs(e))
	}
	if len(m.Ns) == 0 {
		return nil
	}
	m.SetTsig(p.keyName, p.alg, 300, time.Now().Unix())
	c := &dns.Client{Net: "tcp", TsigSecret: map[string]string{p.keyName: p.secret}}
	resp, _, err := c.ExchangeContext(ctx, m, p.addr)
	if err != nil {
		return fmt.Errorf("rfc2136 update: %w", err)
	}
	if resp.Rcode != dns.RcodeSuccess {
		return fmt.Errorf("rfc2136 update rejected: %s", dns.RcodeToString[resp.Rcode])
	}
	return nil
}

func rrs(e endpoint.Endpoint) []dns.RR {
	name := dns.Fqdn(e.DNSName)
	ttl := uint32(e.TTL)
	if ttl == 0 {
		ttl = 300
	}
	var out []dns.RR
	for _, t := range e.Targets {
		hdr := dns.RR_Header{Name: name, Class: dns.ClassINET, Ttl: ttl}
		switch e.RecordType {
		case endpoint.TypeA:
			hdr.Rrtype = dns.TypeA
			out = append(out, &dns.A{Hdr: hdr, A: net.ParseIP(t)})
		case endpoint.TypeAAAA:
			hdr.Rrtype = dns.TypeAAAA
			out = append(out, &dns.AAAA{Hdr: hdr, AAAA: net.ParseIP(t)})
		case endpoint.TypeCNAME:
			hdr.Rrtype = dns.TypeCNAME
			out = append(out, &dns.CNAME{Hdr: hdr, Target: dns.Fqdn(t)})
		case endpoint.TypeTXT:
			hdr.Rrtype = dns.TypeTXT
			out = append(out, &dns.TXT{Hdr: hdr, Txt: []string{t}})
		}
	}
	return out
}

func toEndpoint(rr dns.RR) (endpoint.Endpoint, bool) {
	h := rr.Header()
	name := strings.TrimSuffix(h.Name, ".")
	ttl := int64(h.Ttl)
	switch v := rr.(type) {
	case *dns.A:
		return endpoint.New(name, []string{v.A.String()}, endpoint.TypeA, ttl), true
	case *dns.AAAA:
		return endpoint.New(name, []string{v.AAAA.String()}, endpoint.TypeAAAA, ttl), true
	case *dns.CNAME:
		return endpoint.New(name, []string{strings.TrimSuffix(v.Target, ".")}, endpoint.TypeCNAME, ttl), true
	case *dns.TXT:
		return endpoint.New(name, []string{strings.Join(v.Txt, "")}, endpoint.TypeTXT, ttl), true
	default:
		return endpoint.Endpoint{}, false
	}
}

func tsigAlg(s string) string {
	switch strings.ToLower(strings.TrimSuffix(s, ".")) {
	case "hmac-sha1":
		return dns.HmacSHA1
	case "hmac-sha224":
		return dns.HmacSHA224
	case "hmac-sha384":
		return dns.HmacSHA384
	case "hmac-sha512":
		return dns.HmacSHA512
	default:
		return dns.HmacSHA256
	}
}
