package plan

import (
	"testing"

	"github.com/davidborzek/munpae/internal/endpoint"
)

func ep(name, target string) endpoint.Endpoint {
	return endpoint.New(name, []string{target}, "", 0)
}

func TestCalculate(t *testing.T) {
	desired := []endpoint.Endpoint{ep("a.example.com", "10.0.0.1"), ep("b.example.com", "10.0.0.2")}
	current := []endpoint.Endpoint{ep("b.example.com", "10.0.0.9"), ep("c.example.com", "10.0.0.3")}

	// upsert-only: create the new, update the changed, never delete the stale.
	c := Calculate(desired, current, "upsert-only")
	if len(c.Create) != 1 || c.Create[0].DNSName != "a.example.com" {
		t.Fatalf("create = %+v", c.Create)
	}
	if len(c.Update) != 1 || c.Update[0].DNSName != "b.example.com" {
		t.Fatalf("update = %+v", c.Update)
	}
	if len(c.Delete) != 0 {
		t.Fatalf("upsert-only must not delete, got %+v", c.Delete)
	}

	// sync: additionally delete the stale record.
	s := Calculate(desired, current, "sync")
	if len(s.Delete) != 1 || s.Delete[0].DNSName != "c.example.com" {
		t.Fatalf("delete = %+v", s.Delete)
	}
}

func TestCalculateNoChange(t *testing.T) {
	same := []endpoint.Endpoint{ep("a.example.com", "10.0.0.1")}
	if c := Calculate(same, same, "sync"); !c.Empty() {
		t.Fatalf("identical desired/current must be empty, got %+v", c)
	}
}

func TestCalculateTTLSemantics(t *testing.T) {
	// Desired TTL 0 (unspecified) must not diff against a stored TTL.
	desired := []endpoint.Endpoint{endpoint.New("a.example.com", []string{"10.0.0.1"}, endpoint.TypeA, 0)}
	current := []endpoint.Endpoint{endpoint.New("a.example.com", []string{"10.0.0.1"}, endpoint.TypeA, 300)}
	if c := Calculate(desired, current, "sync"); !c.Empty() {
		t.Fatalf("unset desired TTL must not trigger update, got %+v", c)
	}

	// An explicit desired TTL that differs does trigger an update.
	explicit := []endpoint.Endpoint{endpoint.New("a.example.com", []string{"10.0.0.1"}, endpoint.TypeA, 60)}
	if c := Calculate(explicit, current, "sync"); len(c.Update) != 1 {
		t.Fatalf("explicit differing TTL must update, got %+v", c)
	}
}
