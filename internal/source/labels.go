package source

import (
	"log/slog"
	"strings"
	"sync"
)

// Label separators between the `dns` kind and its field. The dotted form is the
// spec-conformant one (LABEL-SPEC.md); the slash form is deprecated.
const (
	dnsDot   = ".dns."
	dnsSlash = ".dns/"
)

// labelReader reads munpae's `<prefix>.dns.<field>` labels with a backwards-
// compatible fallback to the deprecated `<prefix>.dns/<field>` (slash) form.
// The dotted form wins when both are set. Reading a deprecated key logs a
// one-time warning and increments munpae_deprecated_label_total via onDeprecated.
//
// The slash form is deprecated per LABEL-SPEC.md and will be removed in a
// future minor release.
type labelReader struct {
	prefix       string
	log          *slog.Logger
	onDeprecated func(oldKey string)

	mu     sync.Mutex
	warned map[string]bool
}

func newLabelReader(prefix string, log *slog.Logger, onDeprecated func(string)) *labelReader {
	return &labelReader{prefix: prefix, log: log, onDeprecated: onDeprecated, warned: map[string]bool{}}
}

// field returns the value of `<prefix>.dns.<field>`, falling back to the
// deprecated `<prefix>.dns/<field>` form. The dotted form wins when both exist.
func (r *labelReader) field(labels map[string]string, field string) string {
	if v, ok := labels[r.prefix+dnsDot+field]; ok {
		return v
	}
	old := r.prefix + dnsSlash + field
	if v, ok := labels[old]; ok {
		r.deprecate(old)
		return v
	}
	return ""
}

// deprecate records a use of a deprecated (slash-form) label key: it increments
// the metric on every occurrence and warns once per distinct key.
func (r *labelReader) deprecate(oldKey string) {
	if r.onDeprecated != nil {
		r.onDeprecated(oldKey)
	}
	r.mu.Lock()
	first := !r.warned[oldKey]
	r.warned[oldKey] = true
	r.mu.Unlock()
	if first && r.log != nil {
		r.log.Warn("deprecated label separator '/'; use '.' instead",
			"deprecated", oldKey,
			"replacement", strings.Replace(oldKey, dnsSlash, dnsDot, 1))
	}
}
