// Package plan diffs desired endpoints against the current (owned) records.
package plan

import (
	"sort"

	"github.com/davidborzek/munpae/internal/endpoint"
)

// Changes is the diff between desired and current records.
type Changes struct {
	Create    []endpoint.Endpoint
	Update    []endpoint.Endpoint // desired new state for keys present in both but differing
	UpdateOld []endpoint.Endpoint // current state being replaced, parallel to Update
	Delete    []endpoint.Endpoint
}

// Empty reports whether there is nothing to apply.
func (c *Changes) Empty() bool {
	return len(c.Create)+len(c.Update)+len(c.Delete) == 0
}

// Calculate diffs desired against current by Endpoint.Key(). `current` is
// assumed to already be the owned set (ownership filtering is the registry's
// job). Deletes are only produced when policy is "sync"; "upsert-only" never
// deletes.
func Calculate(desired, current []endpoint.Endpoint, policy string) *Changes {
	desiredByKey := index(desired)
	currentByKey := index(current)
	ch := &Changes{}
	for k, d := range desiredByKey {
		cur, ok := currentByKey[k]
		switch {
		case !ok:
			ch.Create = append(ch.Create, d)
		case !equal(d, cur):
			ch.Update = append(ch.Update, d)
			ch.UpdateOld = append(ch.UpdateOld, cur)
		}
	}
	if policy == "sync" {
		for k, cur := range currentByKey {
			if _, ok := desiredByKey[k]; !ok {
				ch.Delete = append(ch.Delete, cur)
			}
		}
	}
	return ch
}

func index(eps []endpoint.Endpoint) map[string]endpoint.Endpoint {
	m := make(map[string]endpoint.Endpoint, len(eps))
	for _, e := range eps {
		m[e.Key()] = e // last wins on duplicate name+type
	}
	return m
}

// equal compares a desired endpoint (a) against a current one (b). A desired
// TTL of 0 means "unspecified" and never triggers an update on its own.
func equal(a, b endpoint.Endpoint) bool {
	if (a.TTL != 0 && a.TTL != b.TTL) || len(a.Targets) != len(b.Targets) {
		return false
	}
	// Provider-specific proxied flag: only diff when the desired side declares it.
	if dp, ok := a.Labels["cloudflare-proxied"]; ok && dp != b.Labels["cloudflare-proxied"] {
		return false
	}
	x := append([]string(nil), a.Targets...)
	y := append([]string(nil), b.Targets...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
