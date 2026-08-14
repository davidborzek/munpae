package source

import (
	"context"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/davidborzek/munpae/internal/endpoint"
)

// Traefik builds endpoints from Traefik router labels. Hostnames come from the
// `traefik.http.routers.<r>.rule` matcher (Host()/HostSNI()); the record target
// is the anchor mapped from the router's entrypoint via labels on the traefik
// container (`<prefix>.dns.traefik.entrypoint.<ep>.target`), unless the
// container overrides it with `<prefix>.dns.target`.
//
// The legacy slash form (`<prefix>.dns/...`) is still read but deprecated; see
// LABEL-SPEC.md.
type Traefik struct {
	cli            client.APIClient
	entrypoints    []string // instance filter; empty = publish all entrypoints
	includeStopped bool
	log            *slog.Logger
	onConflict     func(host string)
	r              *labelReader
}

// NewTraefik returns a Traefik source. entrypoints scopes which entrypoints
// this instance publishes (nil/empty = all); includeStopped makes the source
// treat stopped-but-existing containers as still desired. onConflict (may be
// nil) is called with a hostname skipped because its routers/entrypoints
// resolved to conflicting targets.
func NewTraefik(cli client.APIClient, prefix string, entrypoints []string, includeStopped bool, log *slog.Logger, onDeprecated func(string), onConflict func(string)) *Traefik {
	return &Traefik{cli: cli, entrypoints: entrypoints, includeStopped: includeStopped, log: log, onConflict: onConflict, r: newLabelReader(prefix, log, onDeprecated)}
}

// Endpoints implements Source. For each hostname it gathers every candidate
// target — each router's entrypoint anchor, or the container's per-app
// `<prefix>.dns.target` override — then resolves ONE winner deterministically:
//
//  1. a per-app override wins outright;
//  2. otherwise the highest entrypoint priority wins
//     (`<prefix>.dns.traefik.entrypoint.<ep>.priority`, default 0);
//  3. a tie between DISTINCT targets is a conflict: the host is skipped (never
//     guessed), logged, and reported via onConflict.
//
// So a host on several routers/entrypoints (split-horizon) resolves the same
// way every reconcile, instead of depending on Go map iteration order.
func (s *Traefik) Endpoints(ctx context.Context) ([]endpoint.Endpoint, error) {
	summaries, err := s.cli.ContainerList(ctx, container.ListOptions{All: s.includeStopped})
	if err != nil {
		return nil, err
	}
	anchors := s.entrypointAnchors(summaries)

	cands := map[string][]candidate{}
	for _, c := range summaries {
		if strings.EqualFold(s.r.field(c.Labels, "exclude"), "true") {
			continue
		}
		override := strings.TrimSpace(s.r.field(c.Labels, "target"))
		for name, r := range parseRouters(c.Labels) {
			if strings.EqualFold(s.r.field(c.Labels, "traefik.router."+name+".exclude"), "true") {
				continue // per-router opt-out
			}
			hosts := parseHosts(r.rule)
			if len(hosts) == 0 {
				continue
			}
			for _, ep := range r.entrypoints {
				if !s.publishes(ep) {
					continue
				}
				var cand candidate
				if override != "" {
					cand.target, cand.override = override, true
				} else {
					a, ok := anchors[ep]
					if !ok || a.target == "" {
						continue // no anchor for this entrypoint; nothing to point at
					}
					cand.target, cand.priority = a.target, a.priority
				}
				for _, h := range hosts {
					cands[h] = append(cands[h], cand)
				}
			}
		}
	}

	hosts := make([]string, 0, len(cands))
	for h := range cands {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)

	var out []endpoint.Endpoint
	for _, h := range hosts {
		target, conflict := resolveCandidates(cands[h])
		if conflict {
			if s.log != nil {
				s.log.Warn("conflicting DNS targets for host; skipping",
					"host", h, "targets", distinctTargets(cands[h]))
			}
			if s.onConflict != nil {
				s.onConflict(h)
			}
			continue
		}
		out = append(out, endpoint.New(h, []string{target}, "", 0))
	}
	return out, nil
}

// candidate is one possible target for a hostname, from one router/entrypoint.
type candidate struct {
	target   string
	priority int
	override bool // from a per-app <prefix>.dns.target (wins over entrypoint anchors)
}

// resolveCandidates picks the winning target for a hostname, or reports a
// conflict. Precedence: any per-app override wins; else the highest entrypoint
// priority. Within the winning tier several routers pointing at the SAME target
// is fine; distinct targets are an unresolved conflict.
func resolveCandidates(cs []candidate) (target string, conflict bool) {
	var pool []candidate
	for _, c := range cs {
		if c.override {
			pool = append(pool, c)
		}
	}
	if len(pool) == 0 {
		maxP, have := 0, false
		for _, c := range cs {
			if !have || c.priority > maxP {
				maxP, have = c.priority, true
			}
		}
		for _, c := range cs {
			if c.priority == maxP {
				pool = append(pool, c)
			}
		}
	}
	first := ""
	for _, c := range pool {
		if first == "" {
			first = c.target
		} else if c.target != first {
			return "", true
		}
	}
	return first, false
}

// distinctTargets returns the unique targets among candidates, sorted — a
// stable value for the conflict log line.
func distinctTargets(cs []candidate) []string {
	seen := map[string]bool{}
	var ts []string
	for _, c := range cs {
		if !seen[c.target] {
			seen[c.target] = true
			ts = append(ts, c.target)
		}
	}
	sort.Strings(ts)
	return ts
}

func (s *Traefik) publishes(ep string) bool {
	if len(s.entrypoints) == 0 {
		return true
	}
	for _, e := range s.entrypoints {
		if e == ep {
			return true
		}
	}
	return false
}

// anchor is an entrypoint's DNS target plus its resolution priority.
type anchor struct {
	target   string
	priority int
}

// entrypointAnchors collects the entrypoint→anchor map from container labels
// (conventionally the traefik container's):
//
//	<prefix>.dns.traefik.entrypoint.<ep>.target    (the record target)
//	<prefix>.dns.traefik.entrypoint.<ep>.priority  (optional, default 0)
//
// A higher priority wins when one hostname maps to several entrypoints. The
// legacy `<prefix>.dns/...` (slash) form is still honoured but deprecated; the
// dotted form wins when both are set.
func (s *Traefik) entrypointAnchors(summaries []container.Summary) map[string]anchor {
	const infix = "traefik.entrypoint."
	newKey := s.r.prefix + dnsDot + infix
	oldKey := s.r.prefix + dnsSlash + infix
	m := map[string]anchor{}
	set := func(ep, field, v string) {
		a := m[ep]
		switch field {
		case "target":
			a.target = v
		case "priority":
			if p, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				a.priority = p
			}
		}
		m[ep] = a
	}
	parse := func(key string, deprecated bool) {
		for _, c := range summaries {
			for k, v := range c.Labels {
				if v == "" || !strings.HasPrefix(k, key) {
					continue
				}
				rest := strings.TrimPrefix(k, key) // "<ep>.<field>"
				dot := strings.LastIndex(rest, ".")
				if dot <= 0 {
					continue
				}
				ep, field := rest[:dot], rest[dot+1:]
				if field != "target" && field != "priority" {
					continue
				}
				if deprecated {
					s.r.deprecate(k)
				}
				set(ep, field, v)
			}
		}
	}
	parse(oldKey, true) // deprecated slash form first; dotted form overwrites it
	parse(newKey, false)
	return m
}

type router struct {
	rule        string
	entrypoints []string
}

// parseRouters extracts each `traefik.http.routers.<name>` router's rule and
// entrypoints from a container's labels.
func parseRouters(labels map[string]string) map[string]*router {
	const p = "traefik.http.routers."
	rs := map[string]*router{}
	get := func(name string) *router {
		if rs[name] == nil {
			rs[name] = &router{}
		}
		return rs[name]
	}
	for k, v := range labels {
		if !strings.HasPrefix(k, p) {
			continue
		}
		rest := k[len(p):]
		dot := strings.LastIndex(rest, ".")
		if dot < 0 {
			continue
		}
		switch name, field := rest[:dot], rest[dot+1:]; field {
		case "rule":
			get(name).rule = v
		case "entrypoints":
			for _, e := range strings.Split(v, ",") {
				if e = strings.TrimSpace(e); e != "" {
					get(name).entrypoints = append(get(name).entrypoints, e)
				}
			}
		}
	}
	return rs
}

var (
	hostFunc = regexp.MustCompile(`(?i)Host(?:SNI)?\(([^)]*)\)`)
	quoted   = regexp.MustCompile("`([^`]*)`")
)

// parseHosts extracts literal hostnames from Host()/HostSNI() matchers in a
// Traefik rule. HostRegexp and non-Host matchers are ignored.
func parseHosts(rule string) []string {
	var hosts []string
	for _, m := range hostFunc.FindAllStringSubmatch(rule, -1) {
		for _, q := range quoted.FindAllStringSubmatch(m[1], -1) {
			if h := strings.TrimSpace(q[1]); h != "" {
				hosts = append(hosts, h)
			}
		}
	}
	return hosts
}
