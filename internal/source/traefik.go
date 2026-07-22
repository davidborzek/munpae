package source

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/davidborzek/munpae/internal/endpoint"
)

// Traefik builds endpoints from Traefik router labels. Hostnames come from the
// `traefik.http.routers.<r>.rule` matcher (Host()/HostSNI()); the record target
// is the anchor mapped from the router's entrypoint via labels on the traefik
// container (`<prefix>.dns/traefik.entrypoint.<ep>.target`), unless the
// container overrides it with `<prefix>.dns/target`.
type Traefik struct {
	cli         client.APIClient
	prefix      string
	entrypoints []string // instance filter; empty = publish all entrypoints
	log         *slog.Logger
}

// NewTraefik returns a Traefik source. entrypoints scopes which entrypoints
// this instance publishes (nil/empty = all).
func NewTraefik(cli client.APIClient, prefix string, entrypoints []string, log *slog.Logger) *Traefik {
	return &Traefik{cli: cli, prefix: prefix, entrypoints: entrypoints, log: log}
}

// Endpoints implements Source.
func (s *Traefik) Endpoints(ctx context.Context) ([]endpoint.Endpoint, error) {
	summaries, err := s.cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, err
	}
	targets := s.entrypointTargets(summaries)

	seen := map[string]bool{} // dedup by hostname; first matching entrypoint wins
	var out []endpoint.Endpoint
	for _, c := range summaries {
		if strings.EqualFold(c.Labels[s.prefix+".dns/exclude"], "true") {
			continue
		}
		override := strings.TrimSpace(c.Labels[s.prefix+".dns/target"])
		for _, r := range parseRouters(c.Labels) {
			hosts := parseHosts(r.rule)
			if len(hosts) == 0 {
				continue
			}
			for _, ep := range r.entrypoints {
				if !s.publishes(ep) {
					continue
				}
				target := override
				if target == "" {
					target = targets[ep]
				}
				if target == "" {
					continue // no anchor for this entrypoint; nothing to point at
				}
				for _, h := range hosts {
					if seen[h] {
						continue
					}
					seen[h] = true
					out = append(out, endpoint.New(h, []string{target}, "", 0))
				}
			}
		}
	}
	return out, nil
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

// entrypointTargets collects the `<prefix>.dns/traefik.entrypoint.<ep>.target`
// map from container labels (conventionally the traefik container's).
func (s *Traefik) entrypointTargets(summaries []container.Summary) map[string]string {
	key := s.prefix + ".dns/traefik.entrypoint."
	m := map[string]string{}
	for _, c := range summaries {
		for k, v := range c.Labels {
			if v == "" || !strings.HasPrefix(k, key) || !strings.HasSuffix(k, ".target") {
				continue
			}
			if ep := strings.TrimSuffix(strings.TrimPrefix(k, key), ".target"); ep != "" {
				m[ep] = v
			}
		}
	}
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
	hostFunc = regexp.MustCompile("(?i)Host(?:SNI)?\\(([^)]*)\\)")
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
