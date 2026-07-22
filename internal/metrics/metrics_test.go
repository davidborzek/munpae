package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestObserve(t *testing.T) {
	m := New("test")
	m.ObserveReconcile(true, 10*time.Millisecond)
	m.ObserveReconcile(false, 5*time.Millisecond)
	m.SetManaged(3)
	m.ObserveChanges(2, 1, 0)

	if got := testutil.ToFloat64(m.reconciles.WithLabelValues("success")); got != 1 {
		t.Errorf("success reconciles = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.reconciles.WithLabelValues("error")); got != 1 {
		t.Errorf("error reconciles = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.managed); got != 3 {
		t.Errorf("managed = %v, want 3", got)
	}
	if got := testutil.ToFloat64(m.changes.WithLabelValues("create")); got != 2 {
		t.Errorf("create changes = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.changes.WithLabelValues("delete")); got != 0 {
		t.Errorf("delete changes = %v, want 0", got)
	}
}

func TestHandler(t *testing.T) {
	m := New("test")
	m.ObserveChanges(1, 0, 0)
	ts := httptest.NewServer(m.handler())
	defer ts.Close()

	// /healthz is a plain 200 liveness probe.
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("healthz = %d %q, want 200 \"ok\"", resp.StatusCode, body)
	}

	// /metrics exposes our counters.
	resp, err = http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), "munpae_changes_total") {
		t.Fatalf("metrics missing munpae_changes_total:\n%s", body)
	}
}

func TestReadyAndWatchRestarts(t *testing.T) {
	m := New("test")

	m.ObserveReconcile(true, time.Millisecond)
	if got := testutil.ToFloat64(m.ready); got != 1 {
		t.Errorf("ready after success = %v, want 1", got)
	}
	if testutil.ToFloat64(m.lastSuccess) == 0 {
		t.Error("last-success timestamp must be set after a success")
	}

	m.ObserveReconcile(false, time.Millisecond)
	if got := testutil.ToFloat64(m.ready); got != 0 {
		t.Errorf("ready after error = %v, want 0", got)
	}

	m.ObserveWatchRestart()
	m.ObserveWatchRestart()
	if got := testutil.ToFloat64(m.watchRestarts); got != 2 {
		t.Errorf("watch restarts = %v, want 2", got)
	}
}
