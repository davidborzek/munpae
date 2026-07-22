// Command munpae publishes DNS records for Docker workloads — an external-dns
// for plain Docker / Compose. See the docs/ directory.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/docker/docker/client"
	"github.com/urfave/cli/v2"

	"github.com/davidborzek/munpae/internal/config"
	"github.com/davidborzek/munpae/internal/metrics"
	"github.com/davidborzek/munpae/internal/provider"
	"github.com/davidborzek/munpae/internal/reconcile"
	"github.com/davidborzek/munpae/internal/registry"
	"github.com/davidborzek/munpae/internal/source"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	app := &cli.App{
		Name:    "munpae",
		Usage:   "publish DNS records for Docker workloads — an external-dns for plain Docker/Compose",
		Version: version,
		Description: "Configured via MUNPAE_* environment variables (see the docs). munpae derives\n" +
			"records from container labels and Traefik rules and reconciles them into a DNS\n" +
			"provider (rfc2136, cloudflare, or a webhook backend).",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "dry-run",
				Usage:   "log the plan without changing any DNS records",
				EnvVars: []string{"MUNPAE_DRY_RUN"},
			},
		},
		Action: runApp,
	}
	if err := app.Run(os.Args); err != nil {
		os.Exit(1)
	}
}

func runApp(c *cli.Context) error {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "error", err)
		return cli.Exit("", 1)
	}
	cfg.DryRun = c.Bool("dry-run") // flag (and its MUNPAE_DRY_RUN env) is authoritative
	logger := newLogger(cfg.LogLevel)
	if err := run(cfg, logger); err != nil {
		logger.Error("fatal", "error", err)
		return cli.Exit("", 1)
	}
	return nil
}

func run(cfg config.Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer func() { _ = cli.Close() }()

	src, err := buildSources(cfg, cli, logger)
	if err != nil {
		return err
	}
	base, err := buildProvider(cfg, logger)
	if err != nil {
		return err
	}
	prov, err := wrapRegistry(cfg, base)
	if err != nil {
		return err
	}
	rec := reconcile.New(src, prov, cfg.DomainFilter, cfg.DefaultTarget, cfg.Policy, logger)
	m := metrics.New(version)
	m.Serve(ctx, cfg.MetricsAddr, logger)

	logger.Info("munpae started",
		"version", version, "provider", cfg.Provider, "registry", cfg.Registry,
		"owner_id", cfg.OwnerID, "sources", cfg.Sources, "domain_filter", cfg.DomainFilter,
		"policy", cfg.Policy, "dry_run", cfg.DryRun)

	reconcileOnce(ctx, rec, m, logger)

	changes := source.Watch(ctx, cli, logger, m.ObserveWatchRestart)
	ticker := time.NewTicker(cfg.ResyncInterval)
	defer ticker.Stop()

	var debounce <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down")
			return nil
		case _, ok := <-changes:
			if !ok {
				changes = nil
				continue
			}
			if debounce == nil {
				debounce = time.After(cfg.DebounceDelay)
			}
		case <-debounce:
			debounce = nil
			reconcileOnce(ctx, rec, m, logger)
		case <-ticker.C:
			reconcileOnce(ctx, rec, m, logger)
		}
	}
}

func buildSources(cfg config.Config, cli client.APIClient, logger *slog.Logger) (source.Source, error) {
	var multi source.Multi
	for _, name := range cfg.Sources {
		switch name {
		case "docker":
			multi = append(multi, source.NewDockerLabel(cli, cfg.LabelPrefix, logger))
		case "traefik":
			multi = append(multi, source.NewTraefik(cli, cfg.LabelPrefix, cfg.Traefik.Entrypoints, logger))
		default:
			return nil, fmt.Errorf("unknown source %q", name)
		}
	}
	if len(multi) == 0 {
		return nil, fmt.Errorf("no sources configured (MUNPAE_SOURCES)")
	}
	return multi, nil
}

func buildProvider(cfg config.Config, logger *slog.Logger) (provider.Provider, error) {
	if cfg.DryRun {
		return provider.NewLogging(logger), nil
	}
	switch cfg.Provider {
	case "rfc2136":
		return provider.NewRFC2136(cfg.RFC2136.Host, cfg.RFC2136.Port, cfg.RFC2136.Zone,
			cfg.RFC2136.TSIGKeyName, cfg.RFC2136.TSIGSecret, cfg.RFC2136.TSIGAlgorithm)
	case "cloudflare":
		return provider.NewCloudflare(cfg.Cloudflare.APIToken, cfg.Cloudflare.Proxied)
	case "webhook":
		return provider.NewWebhook(cfg.Webhook.URL, cfg.Webhook.Timeout)
	default:
		return nil, fmt.Errorf("unknown provider %q", cfg.Provider)
	}
}

func wrapRegistry(cfg config.Config, p provider.Provider) (provider.Provider, error) {
	switch cfg.Registry {
	case "txt":
		return registry.NewTXT(p, cfg.OwnerID, cfg.TXTPrefix), nil
	case "noop":
		return p, nil
	default:
		return nil, fmt.Errorf("unknown registry %q", cfg.Registry)
	}
}

func reconcileOnce(ctx context.Context, rec *reconcile.Reconciler, m *metrics.Metrics, logger *slog.Logger) {
	start := time.Now()
	res, err := rec.Run(ctx)
	m.ObserveReconcile(err == nil, time.Since(start))
	if err != nil {
		logger.Error("reconcile failed", "error", err)
		return
	}
	m.SetManaged(res.Managed)
	m.ObserveChanges(res.Create, res.Update, res.Delete)
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}
