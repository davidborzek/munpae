package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/davidborzek/munpae/internal/endpoint"
	"github.com/davidborzek/munpae/internal/plan"
)

// webhookServer stands up an external-dns webhook protocol server: GET / negotiation,
// GET /records returns `records`, POST /records decodes into `captured`.
func webhookServer(records []wireEndpoint, captured *wireChanges) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(webhookContentType, webhookMediaType)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"filters":[]}`)
	})
	mux.HandleFunc("/records", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set(webhookContentType, webhookMediaType)
			_ = json.NewEncoder(w).Encode(records)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(captured)
		w.WriteHeader(http.StatusNoContent)
	})
	return httptest.NewServer(mux)
}

func TestWebhookRecordsAndApply(t *testing.T) {
	records := []wireEndpoint{
		{DNSName: "a.example.com", Targets: []string{"10.0.0.1"}, RecordType: "A", RecordTTL: 300},
		{DNSName: "c.example.com", Targets: []string{"anchor.example.com"}, RecordType: "CNAME", Labels: map[string]string{"k": "v"}},
	}
	var captured wireChanges
	srv := webhookServer(records, &captured)
	defer srv.Close()

	w, err := NewWebhook(srv.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	eps, err := w.Records(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 2 || eps[0].DNSName != "a.example.com" || eps[0].RecordType != endpoint.TypeA || eps[0].TTL != 300 {
		t.Fatalf("records decode: %+v", eps)
	}
	if eps[1].RecordType != endpoint.TypeCNAME || eps[1].Labels["k"] != "v" {
		t.Fatalf("type/labels decode: %+v", eps[1])
	}

	ch := &plan.Changes{
		Create:    []endpoint.Endpoint{endpoint.New("new.example.com", []string{"10.0.0.2"}, endpoint.TypeA, 0)},
		Update:    []endpoint.Endpoint{endpoint.New("upd.example.com", []string{"10.0.0.9"}, endpoint.TypeA, 0)},
		UpdateOld: []endpoint.Endpoint{endpoint.New("upd.example.com", []string{"10.0.0.1"}, endpoint.TypeA, 0)},
		Delete:    []endpoint.Endpoint{endpoint.New("del.example.com", []string{"10.0.0.3"}, endpoint.TypeA, 0)},
	}
	if err := w.ApplyChanges(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	// munpae's Update maps to the external-dns UpdateNew/UpdateOld pair.
	if len(captured.Create) != 1 || len(captured.UpdateNew) != 1 || len(captured.UpdateOld) != 1 || len(captured.Delete) != 1 {
		t.Fatalf("changes not mapped to external-dns shape: %+v", captured)
	}
	if captured.UpdateNew[0].Targets[0] != "10.0.0.9" || captured.UpdateOld[0].Targets[0] != "10.0.0.1" {
		t.Fatalf("update new/old mismatch: %+v", captured)
	}
}

func TestWebhookNegotiateRejectsWrongContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(webhookContentType, "application/json") // not the webhook media type
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if _, err := NewWebhook(srv.URL, time.Second); err == nil {
		t.Fatal("wrong content type must fail negotiation")
	}
}

func TestWebhookRequiresURL(t *testing.T) {
	if _, err := NewWebhook("", time.Second); err == nil {
		t.Fatal("empty URL must error")
	}
}

func TestWebhookAdjustEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set(webhookContentType, webhookMediaType)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "{}")
	})
	mux.HandleFunc("/adjustendpoints", func(w http.ResponseWriter, r *http.Request) {
		var in []wireEndpoint
		_ = json.NewDecoder(r.Body).Decode(&in)
		for i := range in {
			in[i].RecordTTL = 60 // server normalizes TTL
		}
		w.Header().Set(webhookContentType, webhookMediaType)
		_ = json.NewEncoder(w).Encode(in)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w, err := NewWebhook(srv.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	out, err := w.AdjustEndpoints(context.Background(),
		[]endpoint.Endpoint{endpoint.New("a.example.com", []string{"10.0.0.1"}, endpoint.TypeA, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].TTL != 60 {
		t.Fatalf("server adjustment not applied: %+v", out)
	}
}

func TestWebhookAdjustEndpoints404Identity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" { // no /adjustendpoints route → 404
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set(webhookContentType, webhookMediaType)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "{}")
	}))
	defer srv.Close()

	w, err := NewWebhook(srv.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	in := []endpoint.Endpoint{endpoint.New("a.example.com", []string{"10.0.0.1"}, endpoint.TypeA, 0)}
	out, err := w.AdjustEndpoints(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].DNSName != "a.example.com" || out[0].TTL != 0 {
		t.Fatalf("missing /adjustendpoints must be identity: %+v", out)
	}
}
