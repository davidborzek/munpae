package source

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/davidborzek/munpae/internal/endpoint"
)

// DockerLabel builds endpoints from explicit `<prefix>.dns.*` container labels:
//
//	<prefix>.dns.hostname   comma-separated names (required)
//	<prefix>.dns.target     RDATA override (optional; else DefaultTarget)
//	<prefix>.dns.ttl        TTL seconds (optional)
//	<prefix>.dns.cloudflare-proxied  per-record CF proxied override (optional)
//	<prefix>.dns.exclude    "true" opts the container out
//
// The legacy `<prefix>.dns/<field>` (slash) form is still read for backwards
// compatibility but is deprecated; see LABEL-SPEC.md.
type DockerLabel struct {
	cli            client.APIClient
	includeStopped bool
	r              *labelReader
}

// NewDockerLabel returns a DockerLabel source. includeStopped makes the source
// treat stopped-but-existing containers as still desired (their records are
// kept until the container is removed) instead of listing only running ones.
func NewDockerLabel(cli client.APIClient, prefix string, includeStopped bool, log *slog.Logger, onDeprecated func(string)) *DockerLabel {
	return &DockerLabel{cli: cli, includeStopped: includeStopped, r: newLabelReader(prefix, log, onDeprecated)}
}

// Endpoints implements Source.
func (s *DockerLabel) Endpoints(ctx context.Context) ([]endpoint.Endpoint, error) {
	summaries, err := s.cli.ContainerList(ctx, container.ListOptions{All: s.includeStopped})
	if err != nil {
		return nil, err
	}
	var out []endpoint.Endpoint
	for _, c := range summaries {
		host := strings.TrimSpace(s.r.field(c.Labels, "hostname"))
		if host == "" || strings.EqualFold(s.r.field(c.Labels, "exclude"), "true") {
			continue
		}
		var ttl int64
		if v := s.r.field(c.Labels, "ttl"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				ttl = n
			}
		}
		var targets []string
		if t := strings.TrimSpace(s.r.field(c.Labels, "target")); t != "" {
			targets = []string{t}
		}
		var labels map[string]string
		if v := strings.TrimSpace(s.r.field(c.Labels, "cloudflare-proxied")); v != "" {
			labels = map[string]string{"cloudflare-proxied": strconv.FormatBool(strings.EqualFold(v, "true"))}
		}
		for _, name := range strings.Split(host, ",") {
			if name = strings.TrimSpace(name); name != "" {
				e := endpoint.New(name, targets, "", ttl)
				e.Labels = labels
				out = append(out, e)
			}
		}
	}
	return out, nil
}
