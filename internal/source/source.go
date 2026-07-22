// Package source discovers desired DNS endpoints from the environment (Docker
// labels, Traefik router rules, …) and watches for changes.
package source

import (
	"context"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"github.com/davidborzek/munpae/internal/endpoint"
)

// Source produces the desired DNS endpoints from one part of the environment.
// Adding a source is one implementation; the rest of the pipeline is unchanged.
type Source interface {
	Endpoints(ctx context.Context) ([]endpoint.Endpoint, error)
}

// Multi fans out to several sources and concatenates their endpoints.
type Multi []Source

// Endpoints implements Source.
func (m Multi) Endpoints(ctx context.Context) ([]endpoint.Endpoint, error) {
	var out []endpoint.Endpoint
	for _, s := range m {
		eps, err := s.Endpoints(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, eps...)
	}
	return out, nil
}

// Watch emits a signal whenever a relevant container event occurs. It
// resubscribes on stream errors and stops when ctx is cancelled. The Docker
// trigger is source-independent, so all Docker-backed sources share it.
func Watch(ctx context.Context, cli client.APIClient, log *slog.Logger, onRestart func()) <-chan struct{} {
	out := make(chan struct{}, 1)
	go func() {
		defer close(out)
		f := filters.NewArgs(
			filters.Arg("type", "container"),
			filters.Arg("event", "start"),
			filters.Arg("event", "die"),
			filters.Arg("event", "destroy"),
			filters.Arg("event", "update"),
		)
		for ctx.Err() == nil {
			msgs, errs := cli.Events(ctx, events.ListOptions{Filters: f})
		stream:
			for {
				select {
				case <-ctx.Done():
					return
				case <-msgs:
					signal(out)
				case err := <-errs:
					if ctx.Err() == nil && err != nil {
						log.Warn("docker event stream interrupted, resubscribing", "error", err)
						if onRestart != nil {
							onRestart()
						}
						time.Sleep(time.Second)
					}
					break stream
				}
			}
		}
	}()
	return out
}

func signal(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	default: // a reconcile is already pending
	}
}
