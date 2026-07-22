//go:build e2e

// Package e2e drives the whole munpae binary end to end: a labelled demo
// container flows through the docker source and the TXT registry into a real
// bind server via the rfc2136 provider. Everything runs against a real Docker
// daemon (testcontainers). Run with `go test -tags e2e ./test/e2e/`.
package e2e

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/davidborzek/munpae/internal/provider"
)

const (
	zone   = "example.com"
	tsigID = "munpae"
)

func TestEndToEnd(t *testing.T) {
	tc.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()
	secret := base64.StdEncoding.EncodeToString(randBytes(t, 32))

	bindAddr := startBind(t, ctx, secret)
	startDemo(t, ctx, "app.example.com", "192.0.2.42")
	runMunpae(t, bindAddr, secret)

	// The demo container's label must flow all the way into bind.
	assertRecord(t, bindAddr, secret, "app.example.com", "192.0.2.42", 30*time.Second)
}

// startBind starts a bind server preloaded with the TSIG key + zone and returns
// its reachable "host:port".
func startBind(t *testing.T, ctx context.Context, secret string) string {
	t.Helper()
	// %[1]s = key name, %[2]s = TSIG secret, %[3]s = zone.
	namedConf := fmt.Sprintf(`options {
	directory "/var/cache/bind";
	recursion no;
	listen-on { any; };
	listen-on-v6 { none; };
	allow-query { any; };
	dnssec-validation no;
};
key "%[1]s" {
	algorithm hmac-sha256;
	secret "%[2]s";
};
zone "%[3]s" {
	type master;
	file "/var/cache/bind/db.%[3]s";
	allow-update { key "%[1]s"; };
	allow-transfer { key "%[1]s"; };
};
`, tsigID, secret, zone)
	zoneFile := `$TTL 300
@ IN SOA ns.example.com. admin.example.com. ( 1 3600 600 86400 300 )
@ IN NS ns.example.com.
ns IN A 127.0.0.1
`

	ctr, err := tc.GenericContainer(ctx, tc.GenericContainerRequest{
		ContainerRequest: tc.ContainerRequest{
			Image:        "internetsystemsconsortium/bind9:9.18",
			ExposedPorts: []string{"53/tcp", "53/udp"},
			User:         "root",
			Entrypoint:   []string{"sh", "-c", "chmod 777 /var/cache/bind && exec named -c /etc/bind/named.conf -g"},
			Files: []tc.ContainerFile{
				{Reader: strings.NewReader(namedConf), ContainerFilePath: "/etc/bind/named.conf", FileMode: 0o644},
				{Reader: strings.NewReader(zoneFile), ContainerFilePath: "/var/cache/bind/db." + zone, FileMode: 0o644},
			},
			WaitingFor: wait.ForAll(
				wait.ForLog("all zones loaded"),
				wait.ForListeningPort("53/tcp"),
			).WithStartupTimeoutDefault(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start bind: %v", err)
	}
	tc.CleanupContainer(t, ctr)

	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := ctr.MappedPort(ctx, "53/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return host + ":" + port.Port()
}

// startDemo starts a long-running container carrying munpae DNS labels, as a
// user's workload would.
func startDemo(t *testing.T, ctx context.Context, host, target string) {
	t.Helper()
	ctr, err := tc.GenericContainer(ctx, tc.GenericContainerRequest{
		ContainerRequest: tc.ContainerRequest{
			Image:      "alpine:3",
			Entrypoint: []string{"sleep", "600"},
			Labels: map[string]string{
				"munpae.dns/hostname": host,
				"munpae.dns/target":   target,
			},
			WaitingFor: wait.ForExec([]string{"true"}),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start demo container: %v", err)
	}
	tc.CleanupContainer(t, ctr)
}

// runMunpae builds and runs the real munpae binary against the bind server and
// the host Docker daemon (where the demo container lives). It is stopped when
// the test ends.
func runMunpae(t *testing.T, bindAddr, secret string) {
	t.Helper()
	host, port, ok := strings.Cut(bindAddr, ":")
	if !ok {
		t.Fatalf("bad bind addr %q", bindAddr)
	}

	bin := buildMunpae(t)

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"MUNPAE_SOURCES=docker",
		"MUNPAE_PROVIDER=rfc2136",
		"MUNPAE_REGISTRY=txt",
		"MUNPAE_OWNER_ID=munpae-e2e",
		"MUNPAE_POLICY=sync",
		"MUNPAE_DOMAIN_FILTER="+zone,
		"MUNPAE_METRICS_ADDR=", // no HTTP server needed
		"MUNPAE_RESYNC_INTERVAL=2s",
		"MUNPAE_DEBOUNCE_DELAY=200ms",
		"MUNPAE_RFC2136_HOST="+host,
		"MUNPAE_RFC2136_PORT="+port,
		"MUNPAE_RFC2136_ZONE="+zone,
		"MUNPAE_RFC2136_TSIG_KEYNAME="+tsigID,
		"MUNPAE_RFC2136_TSIG_SECRET="+secret,
	)
	cmd.Stdout = os.Stderr // surface munpae logs under `go test -v`
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start munpae: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
	})
}

// buildMunpae compiles the munpae binary under test. Passing the full import
// path lets `go build` resolve it from any working directory — no module-root
// juggling.
func buildMunpae(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "munpae")
	if out, err := exec.Command("go", "build", "-o", bin, "github.com/davidborzek/munpae/cmd/munpae").CombinedOutput(); err != nil {
		t.Fatalf("build munpae: %v: %s", err, out)
	}
	return bin
}

// assertRecord polls bind until `name` resolves to `target` (via a second
// rfc2136 client), failing after the timeout.
func assertRecord(t *testing.T, bindAddr, secret, name, target string, timeout time.Duration) {
	t.Helper()
	host, port, _ := strings.Cut(bindAddr, ":")
	p, err := provider.NewRFC2136(host, port, zone, tsigID, secret, "hmac-sha256")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	deadline := time.Now().Add(timeout)
	for {
		recs, err := p.Records(ctx)
		if err == nil {
			for _, e := range recs {
				if e.DNSName == name {
					for _, tg := range e.Targets {
						if tg == target {
							return // record made it end to end
						}
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("record %s -> %s not found in bind within %s (last err: %v)", name, target, timeout, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}
