package provider

import (
	"strconv"
	"strings"

	"github.com/davidborzek/munpae/internal/endpoint"
)

// munpae keeps TXT values raw internally (e.g. the ownership value
// `heritage=munpae,munpae/owner=<id>`). Backends that take TXT content in
// zone-file form want it quoted: Cloudflare adds the quotes itself (logging a
// warning) when sent raw, and UniFi rejects a raw value that contains a comma
// ("Comma must be within a quoted string"). external-dns, whose webhook
// protocol munpae speaks, quotes TXT targets too. quoteTXT and unquoteTXT
// convert between the two forms at the provider boundary, write and read
// symmetrically.

// quoteTXT wraps TXT content in double quotes for a backend; it is the exact
// inverse of unquoteTXT on read. Non-TXT content, and already-quoted content,
// are returned unchanged.
func quoteTXT(rt endpoint.RecordType, s string) string {
	if rt != endpoint.TypeTXT {
		return s
	}
	if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		return s
	}
	return strconv.Quote(s)
}

// unquoteTXT normalizes quoted TXT content back to the raw value. Backends
// return TXT content wrapped in double quotes, and may split values longer
// than 255 bytes into several adjacent quoted chunks (`"aaa" "bbb"`). A plain
// value (no surrounding quote) is returned unchanged.
func unquoteTXT(s string) string {
	if !strings.HasPrefix(s, `"`) {
		return s
	}
	if v, err := strconv.Unquote(s); err == nil {
		return v
	}
	// Concatenated 255-byte chunks: unquote each, dropping the joining spaces.
	var b strings.Builder
	for _, chunk := range strings.Split(s, `" "`) {
		if !strings.HasPrefix(chunk, `"`) {
			chunk = `"` + chunk
		}
		if !strings.HasSuffix(chunk, `"`) {
			chunk += `"`
		}
		if v, err := strconv.Unquote(chunk); err == nil {
			b.WriteString(v)
		} else {
			b.WriteString(strings.Trim(chunk, `"`))
		}
	}
	return b.String()
}
