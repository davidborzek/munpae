// Package registry adds record ownership on top of a Provider: munpae only
// manages records it created, tracked via companion TXT records.
package registry

import (
	"context"
	"strings"

	"github.com/davidborzek/munpae/internal/endpoint"
	"github.com/davidborzek/munpae/internal/plan"
	"github.com/davidborzek/munpae/internal/provider"
)

const heritage = "heritage=munpae"

// TXT is a Provider decorator that tracks ownership in companion TXT records.
// A managed record N of type T gets a TXT at `<prefix><t>-N` carrying
// `heritage=munpae,munpae/owner=<id>`; only records with a matching ownership
// TXT are considered munpae's, so foreign records are never modified or deleted.
type TXT struct {
	inner   provider.Provider
	ownerID string
	prefix  string
}

// NewTXT wraps p with TXT-based ownership for ownerID.
func NewTXT(p provider.Provider, ownerID, prefix string) *TXT {
	return &TXT{inner: p, ownerID: ownerID, prefix: prefix}
}

// Records returns only the records munpae owns (those with a matching ownership
// TXT), excluding the TXT records themselves.
func (r *TXT) Records(ctx context.Context) ([]endpoint.Endpoint, error) {
	all, err := r.inner.Records(ctx)
	if err != nil {
		return nil, err
	}
	owned := map[string]bool{}
	for _, e := range all {
		if e.RecordType == endpoint.TypeTXT && r.owns(e) {
			if key, ok := r.realKey(e.DNSName); ok {
				owned[key] = true
			}
		}
	}
	out := make([]endpoint.Endpoint, 0, len(all))
	for _, e := range all {
		if e.RecordType == endpoint.TypeTXT {
			continue
		}
		if owned[e.Key()] {
			out = append(out, e)
		}
	}
	return out, nil
}

// ApplyChanges applies the record changes plus their ownership TXT records
// (created alongside creates, removed alongside deletes; updates keep the same
// owner, so their TXT is untouched).
func (r *TXT) ApplyChanges(ctx context.Context, ch *plan.Changes) error {
	out := &plan.Changes{
		Create:    append([]endpoint.Endpoint(nil), ch.Create...),
		Update:    append([]endpoint.Endpoint(nil), ch.Update...),
		UpdateOld: append([]endpoint.Endpoint(nil), ch.UpdateOld...),
		Delete:    append([]endpoint.Endpoint(nil), ch.Delete...),
	}
	for _, e := range ch.Create {
		out.Create = append(out.Create, r.ownership(e))
	}
	for _, e := range ch.Delete {
		out.Delete = append(out.Delete, r.ownership(e))
	}
	return r.inner.ApplyChanges(ctx, out)
}

// AdjustEndpoints forwards to the wrapped provider when it supports the
// external-dns AdjustEndpoints hook; otherwise it is identity.
func (r *TXT) AdjustEndpoints(ctx context.Context, eps []endpoint.Endpoint) ([]endpoint.Endpoint, error) {
	if a, ok := r.inner.(provider.EndpointAdjuster); ok {
		return a.AdjustEndpoints(ctx, eps)
	}
	return eps, nil
}

func (r *TXT) ownership(e endpoint.Endpoint) endpoint.Endpoint {
	return endpoint.New(r.txtName(e), []string{heritage + ",munpae/owner=" + r.ownerID}, endpoint.TypeTXT, 0)
}

func (r *TXT) txtName(e endpoint.Endpoint) string {
	return r.prefix + strings.ToLower(string(e.RecordType)) + "-" + e.DNSName
}

func (r *TXT) owns(e endpoint.Endpoint) bool {
	if len(e.Targets) == 0 {
		return false
	}
	v := e.Targets[0]
	return strings.Contains(v, heritage) && strings.Contains(v, "munpae/owner="+r.ownerID)
}

// realKey reverses txtName back to the managed record's Key() (`TYPE/name`),
// or false if the TXT name is not one of ours. The record type never contains
// `-`, so the first `-` after the prefix separates type from name.
func (r *TXT) realKey(txtName string) (string, bool) {
	s := txtName
	if r.prefix != "" {
		t := strings.TrimPrefix(s, r.prefix)
		if t == s {
			return "", false
		}
		s = t
	}
	i := strings.Index(s, "-")
	if i <= 0 || i == len(s)-1 {
		return "", false
	}
	return strings.ToUpper(s[:i]) + "/" + s[i+1:], true
}
