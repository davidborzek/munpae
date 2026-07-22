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

// DockerLabel builds endpoints from explicit `<prefix>.dns/*` container labels:
//
//	<prefix>.dns/hostname   comma-separated names (required)
//	<prefix>.dns/target     RDATA override (optional; else DefaultTarget)
//	<prefix>.dns/ttl        TTL seconds (optional)
//	<prefix>.dns/cloudflare-proxied  per-record CF proxied override (optional)
//	<prefix>.dns/exclude    "true" opts the container out
type DockerLabel struct {
	cli    client.APIClient
	prefix string
	log    *slog.Logger
}

// NewDockerLabel returns a DockerLabel source.
func NewDockerLabel(cli client.APIClient, prefix string, log *slog.Logger) *DockerLabel {
	return &DockerLabel{cli: cli, prefix: prefix, log: log}
}

// Endpoints implements Source.
func (s *DockerLabel) Endpoints(ctx context.Context) ([]endpoint.Endpoint, error) {
	summaries, err := s.cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, err
	}
	var (
		hostKey    = s.prefix + ".dns/hostname"
		targetKey  = s.prefix + ".dns/target"
		ttlKey     = s.prefix + ".dns/ttl"
		excludeKey = s.prefix + ".dns/exclude"
		proxiedKey = s.prefix + ".dns/cloudflare-proxied"
	)
	var out []endpoint.Endpoint
	for _, c := range summaries {
		host := strings.TrimSpace(c.Labels[hostKey])
		if host == "" || strings.EqualFold(c.Labels[excludeKey], "true") {
			continue
		}
		var ttl int64
		if v := c.Labels[ttlKey]; v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				ttl = n
			}
		}
		var targets []string
		if t := strings.TrimSpace(c.Labels[targetKey]); t != "" {
			targets = []string{t}
		}
		var labels map[string]string
		if v := strings.TrimSpace(c.Labels[proxiedKey]); v != "" {
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
