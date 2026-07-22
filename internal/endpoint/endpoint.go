// Package endpoint is the source-agnostic unit of desired/observed DNS state.
package endpoint

import "net"

// RecordType is a DNS record type munpae manages.
type RecordType string

const (
	TypeA     RecordType = "A"
	TypeAAAA  RecordType = "AAAA"
	TypeCNAME RecordType = "CNAME"
	TypeTXT   RecordType = "TXT"
)

// Endpoint is one desired or observed DNS record: a name, its RDATA targets,
// type, TTL, and free-form labels (ownership / source metadata). Sources emit
// Endpoints; providers read and write them. Nothing here knows about Docker,
// Traefik, or any specific backend.
type Endpoint struct {
	DNSName    string
	Targets    []string
	RecordType RecordType
	TTL        int64
	Labels     map[string]string
}

// New builds an Endpoint, inferring the record type from the first target when
// rtype is empty (IPv4 → A, IPv6 → AAAA, otherwise CNAME).
func New(name string, targets []string, rtype RecordType, ttl int64) Endpoint {
	if rtype == "" {
		rtype = inferType(targets)
	}
	return Endpoint{DNSName: name, Targets: targets, RecordType: rtype, TTL: ttl}
}

func inferType(targets []string) RecordType {
	if len(targets) == 0 {
		return TypeCNAME
	}
	ip := net.ParseIP(targets[0])
	switch {
	case ip == nil:
		return TypeCNAME
	case ip.To4() != nil:
		return TypeA
	default:
		return TypeAAAA
	}
}

// Key uniquely identifies a record for diffing: name + type.
func (e Endpoint) Key() string { return string(e.RecordType) + "/" + e.DNSName }
