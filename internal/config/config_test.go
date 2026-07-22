package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "rfc2136" || cfg.Registry != "txt" || cfg.Policy != "upsert-only" {
		t.Errorf("core defaults wrong: %+v", cfg)
	}
	if len(cfg.Sources) != 1 || cfg.Sources[0] != "docker" {
		t.Errorf("Sources default: %v", cfg.Sources)
	}
	if cfg.TXTPrefix != "munpae." || cfg.OwnerID != "munpae" || cfg.LabelPrefix != "munpae" {
		t.Errorf("ownership defaults: %+v", cfg)
	}
	if cfg.ResyncInterval != time.Minute || cfg.DebounceDelay != time.Second || cfg.MetricsAddr != ":9333" {
		t.Errorf("timing/metrics defaults: %+v", cfg)
	}
	if cfg.RFC2136.Port != "53" || cfg.RFC2136.TSIGAlgorithm != "hmac-sha256" {
		t.Errorf("rfc2136 defaults: %+v", cfg.RFC2136)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("MUNPAE_PROVIDER", "cloudflare")
	t.Setenv("MUNPAE_CF_API_TOKEN", "tok")
	t.Setenv("MUNPAE_CF_PROXIED", "true")
	t.Setenv("MUNPAE_RFC2136_HOST", "192.0.2.1")
	t.Setenv("MUNPAE_RFC2136_TSIG_KEYNAME", "k")
	t.Setenv("MUNPAE_TRAEFIK_ENTRYPOINTS", "web-internal, web-external")
	t.Setenv("MUNPAE_RESYNC_INTERVAL", "30s")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "cloudflare" || cfg.Cloudflare.APIToken != "tok" || !cfg.Cloudflare.Proxied {
		t.Errorf("cloudflare override: %+v", cfg.Cloudflare)
	}
	if cfg.RFC2136.Host != "192.0.2.1" || cfg.RFC2136.TSIGKeyName != "k" {
		t.Errorf("rfc2136 override: %+v", cfg.RFC2136)
	}
	if len(cfg.Traefik.Entrypoints) != 2 || cfg.Traefik.Entrypoints[1] != "web-external" {
		t.Errorf("traefik entrypoints (nested + trim): %v", cfg.Traefik.Entrypoints)
	}
	if cfg.ResyncInterval != 30*time.Second {
		t.Errorf("resync override: %s", cfg.ResyncInterval)
	}
}

func TestLoadListTrimAndDropEmpty(t *testing.T) {
	t.Setenv("MUNPAE_DOMAIN_FILTER", " a.com , b.com ,, c.com ")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.DomainFilter) != 3 || cfg.DomainFilter[0] != "a.com" || cfg.DomainFilter[2] != "c.com" {
		t.Fatalf("list must trim and drop empties: got %v", cfg.DomainFilter)
	}
}

func TestLoadClampsNonPositiveDurations(t *testing.T) {
	t.Setenv("MUNPAE_RESYNC_INTERVAL", "0s")
	t.Setenv("MUNPAE_DEBOUNCE_DELAY", "-5s")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ResyncInterval != time.Minute || cfg.DebounceDelay != time.Second {
		t.Fatalf("non-positive durations must clamp to defaults: resync=%s debounce=%s", cfg.ResyncInterval, cfg.DebounceDelay)
	}
}

func TestLoadInvalidDurationErrors(t *testing.T) {
	t.Setenv("MUNPAE_RESYNC_INTERVAL", "nonsense")
	if _, err := Load(); err == nil {
		t.Fatal("invalid duration must return an error")
	}
}

func TestLoadMetricsAddrDisable(t *testing.T) {
	// An explicitly empty value must disable the server, not fall back to the
	// default (regression against caarlos0/env's empty-with-default behaviour).
	t.Setenv("MUNPAE_METRICS_ADDR", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetricsAddr != "" {
		t.Fatalf("empty MUNPAE_METRICS_ADDR must disable, got %q", cfg.MetricsAddr)
	}
}
