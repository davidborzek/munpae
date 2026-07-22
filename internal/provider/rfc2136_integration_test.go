//go:build integration

package provider

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/davidborzek/munpae/internal/endpoint"
	"github.com/davidborzek/munpae/internal/plan"
)

// TestRFC2136Integration exercises the rfc2136 provider against a real bind
// server (started via testcontainers): create (single + multi-target), AXFR
// read with merge, update, and delete. Run with `go test -tags integration`.
func TestRFC2136Integration(t *testing.T) {
	tc.SkipIfProviderIsNotHealthy(t)

	const zone, key = "example.com", "munpae"
	secret := base64.StdEncoding.EncodeToString(mustRand(t, 32))

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
`, key, secret, zone)
	zoneFile := `$TTL 300
@ IN SOA ns.example.com. admin.example.com. ( 1 3600 600 86400 300 )
@ IN NS ns.example.com.
ns IN A 127.0.0.1
`

	ctx := context.Background()
	ctr, err := tc.GenericContainer(ctx, tc.GenericContainerRequest{
		ContainerRequest: tc.ContainerRequest{
			Image:        "internetsystemsconsortium/bind9:9.18",
			ExposedPorts: []string{"53/tcp", "53/udp"},
			User:         "root",
			// The ISC image's named runs as the bind user, which can't write the
			// root-owned /var/cache/bind (journal + managed-keys). Open it up first.
			Entrypoint: []string{"sh", "-c", "chmod 777 /var/cache/bind && exec named -c /etc/bind/named.conf -g"},
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

	p, err := NewRFC2136(host, port.Port(), zone, key, secret, "hmac-sha256")
	if err != nil {
		t.Fatal(err)
	}

	// Create a single A and a multi-target A.
	single := endpoint.New("a.example.com", []string{"192.0.2.1"}, endpoint.TypeA, 0)
	multi := endpoint.New("m.example.com", []string{"192.0.2.10", "192.0.2.11"}, endpoint.TypeA, 0)
	if err := p.ApplyChanges(ctx, &plan.Changes{Create: []endpoint.Endpoint{single, multi}}); err != nil {
		t.Fatalf("create: %v", err)
	}

	recs := records(t, p, ctx)
	if a := recs["A/a.example.com"]; a == nil || len(a.Targets) != 1 || a.Targets[0] != "192.0.2.1" {
		t.Fatalf("single A not created: %+v", a)
	}
	// Multi-value records must merge into one endpoint on read.
	if m := recs["A/m.example.com"]; m == nil || len(m.Targets) != 2 {
		t.Fatalf("multi-target A must merge to 2 targets: %+v", m)
	}

	// Update the single A's value.
	updated := endpoint.New("a.example.com", []string{"192.0.2.9"}, endpoint.TypeA, 0)
	if err := p.ApplyChanges(ctx, &plan.Changes{Update: []endpoint.Endpoint{updated}}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if a := records(t, p, ctx)["A/a.example.com"]; a == nil || a.Targets[0] != "192.0.2.9" {
		t.Fatalf("update not applied: %+v", a)
	}

	// Delete it.
	if err := p.ApplyChanges(ctx, &plan.Changes{Delete: []endpoint.Endpoint{updated}}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if a := records(t, p, ctx)["A/a.example.com"]; a != nil {
		t.Fatalf("record not deleted: %+v", a)
	}
}

func records(t *testing.T, p *RFC2136, ctx context.Context) map[string]*endpoint.Endpoint {
	t.Helper()
	eps, err := p.Records(ctx)
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	m := map[string]*endpoint.Endpoint{}
	for i := range eps {
		m[eps[i].Key()] = &eps[i]
	}
	return m
}

func mustRand(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}
