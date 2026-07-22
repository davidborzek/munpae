// Package config loads munpae settings from MUNPAE_* environment variables into
// a grouped struct, using caarlos0/env for tag-driven parsing.
package config

import (
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds all runtime settings. Core fields are source-agnostic; the
// nested blocks group source/provider-specific settings and inherit the parent
// env prefix (e.g. RFC2136.Host -> MUNPAE_RFC2136_HOST).
type Config struct {
	Sources        []string      `env:"SOURCES" envDefault:"docker"`      // enabled sources, e.g. docker,traefik
	Provider       string        `env:"PROVIDER" envDefault:"rfc2136"`    // rfc2136 | cloudflare
	Registry       string        `env:"REGISTRY" envDefault:"txt"`        // txt | noop
	LabelPrefix    string        `env:"LABEL_PREFIX" envDefault:"munpae"` // Docker label namespace -> <prefix>.dns/*
	DomainFilter   []string      `env:"DOMAIN_FILTER"`                    // only manage names under these zones
	DefaultTarget  string        `env:"DEFAULT_TARGET"`                   // fallback RDATA when a source yields none
	Policy         string        `env:"POLICY" envDefault:"upsert-only"`  // sync | upsert-only
	OwnerID        string        `env:"OWNER_ID" envDefault:"munpae"`     // ownership id written into TXT records
	TXTPrefix      string        `env:"TXT_PREFIX" envDefault:"munpae."`  // ownership TXT name prefix; keep stable once set
	ResyncInterval time.Duration `env:"RESYNC_INTERVAL" envDefault:"60s"` // periodic full resync (clamped > 0)
	DebounceDelay  time.Duration `env:"DEBOUNCE_DELAY" envDefault:"1s"`   // event coalescing window (clamped > 0)
	MetricsAddr    string        `env:"METRICS_ADDR" envDefault:":9333"`  // /metrics + /healthz listen address; blank disables
	LogLevel       string        `env:"LOG_LEVEL" envDefault:"info"`
	DryRun         bool          `env:"DRY_RUN"`

	Traefik    TraefikConfig    `envPrefix:"TRAEFIK_"`
	RFC2136    RFC2136Config    `envPrefix:"RFC2136_"`
	Cloudflare CloudflareConfig `envPrefix:"CF_"`
	Webhook    WebhookConfig    `envPrefix:"WEBHOOK_"`
}

// TraefikConfig scopes the traefik source.
type TraefikConfig struct {
	// Entrypoints publishes only these traefik entrypoints (empty = all) — the
	// per-instance internal/external split.
	Entrypoints []string `env:"ENTRYPOINTS"`
}

// RFC2136Config configures the bind/rfc2136 provider (dynamic UPDATE + AXFR).
type RFC2136Config struct {
	Host          string `env:"HOST"`
	Port          string `env:"PORT" envDefault:"53"`
	Zone          string `env:"ZONE"`
	TSIGKeyName   string `env:"TSIG_KEYNAME"`
	TSIGSecret    string `env:"TSIG_SECRET"`
	TSIGAlgorithm string `env:"TSIG_ALGORITHM" envDefault:"hmac-sha256"`
}

// CloudflareConfig configures the cloudflare provider.
type CloudflareConfig struct {
	APIToken string `env:"API_TOKEN"`
	Proxied  bool   `env:"PROXIED"`
}

// WebhookConfig configures the webhook provider (external-dns webhook protocol).
type WebhookConfig struct {
	URL     string        `env:"URL"`
	Timeout time.Duration `env:"TIMEOUT" envDefault:"10s"`
}

// Load reads and validates the configuration from MUNPAE_* environment variables.
func Load() (Config, error) {
	var cfg Config
	opts := env.Options{
		Prefix: "MUNPAE_",
		// Override list parsing to trim spaces and drop empty entries, so
		// "a, b ,, c" yields [a b c] rather than the library's raw split.
		FuncMap: map[reflect.Type]env.ParserFunc{
			reflect.TypeOf([]string(nil)): parseList,
		},
	}
	if err := env.ParseWithOptions(&cfg, opts); err != nil {
		return Config{}, err
	}
	// caarlos0/env resolves an explicitly-empty var with a default back to that
	// default, so honour MUNPAE_METRICS_ADDR="" as an explicit "disable the server".
	if v, ok := os.LookupEnv("MUNPAE_METRICS_ADDR"); ok {
		cfg.MetricsAddr = v
	}
	// A non-positive interval would panic time.NewTicker / defeat debouncing.
	if cfg.ResyncInterval <= 0 {
		cfg.ResyncInterval = 60 * time.Second
	}
	if cfg.DebounceDelay <= 0 {
		cfg.DebounceDelay = time.Second
	}
	return cfg, nil
}

// parseList splits a comma-separated value, trimming spaces and dropping empties.
func parseList(v string) (any, error) {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}
