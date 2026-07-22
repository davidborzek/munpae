package source

import (
	"context"
	"log/slog"
	"testing"

	"github.com/docker/docker/api/types/container"
)

func TestParseHosts(t *testing.T) {
	cases := []struct {
		rule string
		want []string
	}{
		{"Host(`a.example.com`)", []string{"a.example.com"}},
		{"Host(`a.example.com`, `b.example.com`)", []string{"a.example.com", "b.example.com"}},
		{"Host(`a.example.com`) && PathPrefix(`/x`)", []string{"a.example.com"}},
		{"Host(`a.example.com`) || Host(`b.example.com`)", []string{"a.example.com", "b.example.com"}},
		{"HostSNI(`a.example.com`)", []string{"a.example.com"}},
		{"PathPrefix(`/x`)", nil},
	}
	for _, c := range cases {
		got := parseHosts(c.rule)
		if len(got) != len(c.want) {
			t.Errorf("%q → %v, want %v", c.rule, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q → %v, want %v", c.rule, got, c.want)
				break
			}
		}
	}
}

func TestTraefikEndpoints(t *testing.T) {
	summaries := []container.Summary{
		{Labels: map[string]string{ // traefik container carries the anchor map
			"munpae.dns/traefik.entrypoint.internal-secure.target": "internal.example.com",
			"munpae.dns/traefik.entrypoint.external-secure.target": "external.example.com",
		}},
		{Labels: map[string]string{ // internal-only app
			"traefik.http.routers.sonarr.rule":        "Host(`sonarr.example.com`)",
			"traefik.http.routers.sonarr.entrypoints": "internal-secure",
		}},
		{Labels: map[string]string{ // public app
			"traefik.http.routers.plex.rule":        "Host(`plex.example.com`)",
			"traefik.http.routers.plex.entrypoints": "external-secure",
		}},
		{Labels: map[string]string{ // per-app target override wins over the anchor
			"traefik.http.routers.db.rule":        "Host(`db.example.com`)",
			"traefik.http.routers.db.entrypoints": "internal-secure",
			"munpae.dns/target":                   "192.0.2.2",
		}},
	}

	collect := func(entrypoints []string) map[string]string {
		s := NewTraefik(fakeDocker{summaries: summaries}, "munpae", entrypoints, slog.Default())
		eps, err := s.Endpoints(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for _, e := range eps {
			if len(e.Targets) > 0 {
				m[e.DNSName] = e.Targets[0]
			}
		}
		return m
	}

	// bind instance: no filter → all entrypoints.
	bind := collect(nil)
	if bind["sonarr.example.com"] != "internal.example.com" {
		t.Errorf("internal app → internal anchor: %v", bind)
	}
	if bind["plex.example.com"] != "external.example.com" {
		t.Errorf("public app → external anchor: %v", bind)
	}
	if bind["db.example.com"] != "192.0.2.2" {
		t.Errorf("per-app target must override the anchor: %v", bind)
	}

	// cf instance: only external-secure → internal apps excluded.
	cf := collect([]string{"external-secure"})
	if _, ok := cf["sonarr.example.com"]; ok {
		t.Errorf("cf instance must not publish an internal-only app: %v", cf)
	}
	if cf["plex.example.com"] != "external.example.com" {
		t.Errorf("cf must publish the external app: %v", cf)
	}
}
