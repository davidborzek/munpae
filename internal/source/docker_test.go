package source

import (
	"context"
	"log/slog"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// fakeDocker satisfies client.APIClient via embedding; only ContainerList is
// implemented (the only method DockerLabel calls).
type fakeDocker struct {
	client.APIClient
	summaries []container.Summary
}

func (f fakeDocker) ContainerList(context.Context, container.ListOptions) ([]container.Summary, error) {
	return f.summaries, nil
}

func TestDockerLabelEndpoints(t *testing.T) {
	s := NewDockerLabel(fakeDocker{summaries: []container.Summary{
		{Labels: map[string]string{"munpae.dns/hostname": "a.example.com", "munpae.dns/target": "10.0.0.1"}},
		{Labels: map[string]string{"munpae.dns/hostname": "b.example.com, c.example.com", "munpae.dns/target": "10.0.0.2", "munpae.dns/ttl": "300"}},
		{Labels: map[string]string{"munpae.dns/hostname": "x.example.com", "munpae.dns/exclude": "true"}},
		{Labels: map[string]string{"unrelated": "1"}},
	}}, "munpae", slog.Default())

	eps, err := s.Endpoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]struct {
		targets []string
		ttl     int64
	}{}
	for _, e := range eps {
		byName[e.DNSName] = struct {
			targets []string
			ttl     int64
		}{e.Targets, e.TTL}
	}
	if len(eps) != 3 {
		t.Fatalf("want 3 endpoints (a, b, c), got %d: %+v", len(eps), eps)
	}
	if _, ok := byName["x.example.com"]; ok {
		t.Error("excluded container must be skipped")
	}
	for _, n := range []string{"a.example.com", "b.example.com", "c.example.com"} {
		if _, ok := byName[n]; !ok {
			t.Errorf("missing endpoint %s", n)
		}
	}
	if b := byName["b.example.com"]; b.ttl != 300 || len(b.targets) != 1 || b.targets[0] != "10.0.0.2" {
		t.Errorf("comma-split entry b: ttl=%d targets=%v", b.ttl, b.targets)
	}
}

func TestDockerLabelProxied(t *testing.T) {
	s := NewDockerLabel(fakeDocker{summaries: []container.Summary{
		{Labels: map[string]string{"munpae.dns/hostname": "p.example.com", "munpae.dns/target": "anchor.example.com", "munpae.dns/cloudflare-proxied": "True"}},
	}}, "munpae", slog.Default())

	eps, err := s.Endpoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The label is normalized to canonical "true"/"false".
	if len(eps) != 1 || eps[0].Labels["cloudflare-proxied"] != "true" {
		t.Fatalf("proxied label must normalize to \"true\": %+v", eps)
	}
}
