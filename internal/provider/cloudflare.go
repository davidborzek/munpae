package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudflare/cloudflare-go"

	"github.com/davidborzek/munpae/internal/endpoint"
	"github.com/davidborzek/munpae/internal/plan"
)

// proxiedLabel carries a per-record Cloudflare proxied override in Endpoint.Labels.
const proxiedLabel = "cloudflare-proxied"

// cfAPI is the subset of cloudflare-go the provider uses (fakeable in tests).
type cfAPI interface {
	ListZones(ctx context.Context, z ...string) ([]cloudflare.Zone, error)
	ListDNSRecords(ctx context.Context, rc *cloudflare.ResourceContainer, params cloudflare.ListDNSRecordsParams) ([]cloudflare.DNSRecord, *cloudflare.ResultInfo, error)
	CreateDNSRecord(ctx context.Context, rc *cloudflare.ResourceContainer, params cloudflare.CreateDNSRecordParams) (cloudflare.DNSRecord, error)
	UpdateDNSRecord(ctx context.Context, rc *cloudflare.ResourceContainer, params cloudflare.UpdateDNSRecordParams) (cloudflare.DNSRecord, error)
	DeleteDNSRecord(ctx context.Context, rc *cloudflare.ResourceContainer, recordID string) error
}

// Cloudflare programs DNS records in every zone the API token can access.
// A/AAAA/CNAME records are proxied per the global default, overridable per
// record via the `cloudflare-proxied` label; TXT is never proxied.
type Cloudflare struct {
	api     cfAPI
	proxied bool
}

// NewCloudflare builds the provider from an API token.
func NewCloudflare(token string, proxied bool) (*Cloudflare, error) {
	if token == "" {
		return nil, fmt.Errorf("cloudflare: API token required (MUNPAE_CF_API_TOKEN)")
	}
	api, err := cloudflare.NewWithAPIToken(token)
	if err != nil {
		return nil, fmt.Errorf("cloudflare client: %w", err)
	}
	return &Cloudflare{api: api, proxied: proxied}, nil
}

// Records lists A/AAAA/CNAME/TXT across all accessible zones, merging records
// that share a name+type into one multi-target endpoint.
func (c *Cloudflare) Records(ctx context.Context) ([]endpoint.Endpoint, error) {
	zones, err := c.api.ListZones(ctx)
	if err != nil {
		return nil, fmt.Errorf("cloudflare list zones: %w", err)
	}
	byKey := map[string]*endpoint.Endpoint{}
	var order []string
	for _, z := range zones {
		recs, err := c.listRecords(ctx, z.ID)
		if err != nil {
			return nil, err
		}
		for _, r := range recs {
			rt, ok := recordType(r.Type)
			if !ok {
				continue
			}
			name := strings.TrimSuffix(r.Name, ".")
			key := string(rt) + "/" + name
			if ep := byKey[key]; ep != nil {
				ep.Targets = append(ep.Targets, r.Content)
				continue
			}
			ep := endpoint.New(name, []string{r.Content}, rt, int64(r.TTL))
			if rt != endpoint.TypeTXT {
				ep.Labels = map[string]string{proxiedLabel: strconv.FormatBool(r.Proxied != nil && *r.Proxied)}
			}
			byKey[key] = &ep
			order = append(order, key)
		}
	}
	out := make([]endpoint.Endpoint, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out, nil
}

// listRecords returns every DNS record in a zone, following pagination.
func (c *Cloudflare) listRecords(ctx context.Context, zoneID string) ([]cloudflare.DNSRecord, error) {
	var all []cloudflare.DNSRecord
	params := cloudflare.ListDNSRecordsParams{}
	for {
		recs, info, err := c.api.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(zoneID), params)
		if err != nil {
			return nil, fmt.Errorf("cloudflare list records: %w", err)
		}
		all = append(all, recs...)
		if info == nil || !info.HasMorePages() {
			break
		}
		params.ResultInfo = info.Next()
	}
	return all, nil
}

// ApplyChanges creates/updates/deletes records, resolving each to its zone and
// converging the full target set per record.
func (c *Cloudflare) ApplyChanges(ctx context.Context, ch *plan.Changes) error {
	zones, err := c.api.ListZones(ctx)
	if err != nil {
		return fmt.Errorf("cloudflare list zones: %w", err)
	}
	names := make([]string, 0, len(zones))
	id := map[string]string{}
	for _, z := range zones {
		id[z.Name] = z.ID
		names = append(names, z.Name)
	}

	// Index existing records for update/delete lookups by "TYPE/name".
	type ref struct{ zone, record string }
	index := map[string][]ref{}
	for _, z := range zones {
		recs, err := c.listRecords(ctx, z.ID)
		if err != nil {
			return err
		}
		for _, r := range recs {
			key := r.Type + "/" + strings.TrimSuffix(r.Name, ".")
			index[key] = append(index[key], ref{z.ID, r.ID})
		}
	}
	zoneID := func(name string) (string, error) {
		if zn := longestZone(name, names); zn != "" {
			return id[zn], nil
		}
		return "", fmt.Errorf("cloudflare: no accessible zone for %s", name)
	}
	create := func(e endpoint.Endpoint) error {
		zid, err := zoneID(e.DNSName)
		if err != nil {
			return err
		}
		for _, t := range e.Targets {
			if _, err := c.api.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(zid), c.createParams(e, t)); err != nil {
				return fmt.Errorf("cloudflare create %s: %w", e.DNSName, err)
			}
		}
		return nil
	}
	del := func(refs []ref) error {
		for _, r := range refs {
			if err := c.api.DeleteDNSRecord(ctx, cloudflare.ZoneIdentifier(r.zone), r.record); err != nil {
				return fmt.Errorf("cloudflare delete: %w", err)
			}
		}
		return nil
	}

	for _, e := range ch.Create {
		if err := create(e); err != nil {
			return err
		}
	}
	for _, e := range ch.Update {
		refs := index[e.Key()]
		if len(refs) == 1 && len(e.Targets) == 1 { // in-place single-value update
			r := refs[0]
			if _, err := c.api.UpdateDNSRecord(ctx, cloudflare.ZoneIdentifier(r.zone), c.updateParams(e, e.Targets[0], r.record)); err != nil {
				return fmt.Errorf("cloudflare update %s: %w", e.DNSName, err)
			}
			continue
		}
		// Converge multi-value sets: replace the whole RRset.
		if err := del(refs); err != nil {
			return err
		}
		if err := create(e); err != nil {
			return err
		}
	}
	for _, e := range ch.Delete {
		if err := del(index[e.Key()]); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cloudflare) createParams(e endpoint.Endpoint, target string) cloudflare.CreateDNSRecordParams {
	proxied := c.proxiedFor(e)
	return cloudflare.CreateDNSRecordParams{
		Type:    string(e.RecordType),
		Name:    e.DNSName,
		Content: target,
		TTL:     c.ttl(e),
		Proxied: &proxied,
	}
}

func (c *Cloudflare) updateParams(e endpoint.Endpoint, target, recordID string) cloudflare.UpdateDNSRecordParams {
	proxied := c.proxiedFor(e)
	return cloudflare.UpdateDNSRecordParams{
		ID:      recordID,
		Type:    string(e.RecordType),
		Name:    e.DNSName,
		Content: target,
		TTL:     c.ttl(e),
		Proxied: &proxied,
	}
}

// proxiedFor resolves the effective proxied flag: a per-record label wins over
// the global default; only A/AAAA/CNAME are ever proxied.
func (c *Cloudflare) proxiedFor(e endpoint.Endpoint) bool {
	if !proxiable(e.RecordType) {
		return false
	}
	if v, ok := e.Labels[proxiedLabel]; ok {
		return v == "true"
	}
	return c.proxied
}

func (c *Cloudflare) ttl(e endpoint.Endpoint) int {
	if c.proxiedFor(e) {
		return 1 // Cloudflare requires TTL 1 (automatic) for proxied records
	}
	if e.TTL >= 60 {
		return int(e.TTL)
	}
	return 1
}

func proxiable(t endpoint.RecordType) bool {
	return t == endpoint.TypeA || t == endpoint.TypeAAAA || t == endpoint.TypeCNAME
}

func recordType(t string) (endpoint.RecordType, bool) {
	switch t {
	case "A", "AAAA", "CNAME", "TXT":
		return endpoint.RecordType(t), true
	default:
		return "", false
	}
}

// longestZone returns the most specific accessible zone `name` belongs to, or "".
func longestZone(name string, zones []string) string {
	name = strings.TrimSuffix(name, ".")
	best := ""
	for _, z := range zones {
		z = strings.TrimSuffix(z, ".")
		if (name == z || strings.HasSuffix(name, "."+z)) && len(z) > len(best) {
			best = z
		}
	}
	return best
}
