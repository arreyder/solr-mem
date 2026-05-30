// Package contenthash produces stable SHA-256 hashes over memory content for
// dedup purposes. Normalization is deliberate: the same logical memory sent
// with different whitespace or tag ordering should collide.
package contenthash

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// Compute returns a hex-encoded SHA-256 over normalized inputs.
//
// Normalization:
//   - title and content are trimmed and collapsed (runs of whitespace → single space)
//   - tags are sorted and each tag is lowercased + trimmed
//   - fields are joined with 0x1f (unit separator) to avoid ambiguity
//
// An empty content produces an empty hash (caller decides whether to dedup).
func Compute(title, content string, tags []string) string {
	content = collapse(content)
	if content == "" {
		return ""
	}
	title = collapse(title)

	normTags := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			normTags = append(normTags, t)
		}
	}
	sort.Strings(normTags)

	h := sha256.New()
	h.Write([]byte(title))
	h.Write([]byte{0x1f})
	h.Write([]byte(content))
	h.Write([]byte{0x1f})
	// Join tags with 0x1e so a tag containing a literal delimiter can't
	// collide with two tags split by it.
	h.Write([]byte(strings.Join(normTags, "\x1e")))
	return hex.EncodeToString(h.Sum(nil))
}

// collapse trims and replaces runs of whitespace with a single space.
func collapse(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !space {
				b.WriteByte(' ')
				space = true
			}
			continue
		}
		b.WriteRune(r)
		space = false
	}
	return b.String()
}
