package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/davidborzek/munpae/internal/endpoint"
	"github.com/davidborzek/munpae/internal/plan"
)

// external-dns webhook protocol constants — kept byte-identical so any
// external-dns webhook provider server interoperates with munpae.
const (
	webhookMediaType   = "application/external.dns.webhook+json;version=1"
	webhookAcceptHdr   = "Accept"
	webhookContentType = "Content-Type"
)

// Webhook delegates DNS operations to an external server speaking the
// external-dns webhook protocol, so any external-dns webhook provider works.
type Webhook struct {
	base   string // base URL without trailing slash
	client *http.Client
}

// NewWebhook builds the provider and negotiates the protocol version.
func NewWebhook(rawURL string, timeout time.Duration) (*Webhook, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("webhook: URL required (MUNPAE_WEBHOOK_URL)")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("webhook: invalid URL: %w", err)
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	w := &Webhook{base: strings.TrimSuffix(u.String(), "/"), client: &http.Client{Timeout: timeout}}
	if err := w.negotiate(); err != nil {
		return nil, err
	}
	return w, nil
}

// negotiate performs the initial GET / handshake and verifies the media type.
func (w *Webhook) negotiate() error {
	req, err := http.NewRequest(http.MethodGet, w.base+"/", nil)
	if err != nil {
		return err
	}
	req.Header.Set(webhookAcceptHdr, webhookMediaType)
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: negotiate: %w", err)
	}
	defer drain(resp.Body)
	if resp.StatusCode/100 != 2 { // spec: only 2xx is success
		return fmt.Errorf("webhook: negotiate status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get(webhookContentType); ct != webhookMediaType {
		return fmt.Errorf("webhook: unexpected content type %q", ct)
	}
	return nil
}

// Records fetches the current records via GET /records.
func (w *Webhook) Records(ctx context.Context) ([]endpoint.Endpoint, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.base+"/records", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(webhookAcceptHdr, webhookMediaType)
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webhook records: %w", err)
	}
	defer drain(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("webhook records: status %d", resp.StatusCode)
	}
	var wire []wireEndpoint
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("webhook records: decode: %w", err)
	}
	out := make([]endpoint.Endpoint, 0, len(wire))
	for _, e := range wire {
		out = append(out, e.toEndpoint())
	}
	return out, nil
}

// ApplyChanges posts the changes to POST /records in external-dns' shape.
func (w *Webhook) ApplyChanges(ctx context.Context, ch *plan.Changes) error {
	body := wireChanges{
		Create:    toWire(ch.Create),
		UpdateOld: toWire(ch.UpdateOld),
		UpdateNew: toWire(ch.Update),
		Delete:    toWire(ch.Delete),
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.base+"/records", &buf)
	if err != nil {
		return err
	}
	req.Header.Set(webhookContentType, webhookMediaType)
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook apply: %w", err)
	}
	defer drain(resp.Body)
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("webhook apply: status %d", resp.StatusCode)
	}
	return nil
}

// AdjustEndpoints asks the server to normalize desired endpoints (POST
// /adjustendpoints). A 404 (server without the optional route) is treated as
// identity so minimal servers still interoperate.
func (w *Webhook) AdjustEndpoints(ctx context.Context, eps []endpoint.Endpoint) ([]endpoint.Endpoint, error) {
	if len(eps) == 0 {
		return eps, nil
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(toWire(eps)); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.base+"/adjustendpoints", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set(webhookContentType, webhookMediaType)
	req.Header.Set(webhookAcceptHdr, webhookMediaType)
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webhook adjustendpoints: %w", err)
	}
	defer drain(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return eps, nil // optional route not implemented → identity
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("webhook adjustendpoints: status %d", resp.StatusCode)
	}
	var wire []wireEndpoint
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("webhook adjustendpoints: decode: %w", err)
	}
	out := make([]endpoint.Endpoint, 0, len(wire))
	for _, e := range wire {
		out = append(out, e.toEndpoint())
	}
	return out, nil
}

func drain(rc io.ReadCloser) {
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}

// wireEndpoint mirrors external-dns' endpoint.Endpoint JSON.
type wireEndpoint struct {
	DNSName          string            `json:"dnsName,omitempty"`
	Targets          []string          `json:"targets,omitempty"`
	RecordType       string            `json:"recordType,omitempty"`
	RecordTTL        int64             `json:"recordTTL,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	SetIdentifier    string            `json:"setIdentifier,omitempty"`
	ProviderSpecific []wireProp        `json:"providerSpecific,omitempty"`
}

type wireProp struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// wireChanges mirrors external-dns' plan.Changes JSON (capitalized keys).
type wireChanges struct {
	Create    []wireEndpoint `json:"Create,omitempty"`
	UpdateOld []wireEndpoint `json:"UpdateOld,omitempty"`
	UpdateNew []wireEndpoint `json:"UpdateNew,omitempty"`
	Delete    []wireEndpoint `json:"Delete,omitempty"`
}

func (e wireEndpoint) toEndpoint() endpoint.Endpoint {
	ep := endpoint.New(e.DNSName, e.Targets, endpoint.RecordType(e.RecordType), e.RecordTTL)
	ep.Labels = e.Labels
	return ep
}

func toWire(eps []endpoint.Endpoint) []wireEndpoint {
	if len(eps) == 0 {
		return nil
	}
	out := make([]wireEndpoint, 0, len(eps))
	for _, e := range eps {
		out = append(out, wireEndpoint{
			DNSName:    e.DNSName,
			Targets:    e.Targets,
			RecordType: string(e.RecordType),
			RecordTTL:  e.TTL,
			Labels:     e.Labels,
		})
	}
	return out
}
