// Package privacy scrubs secrets from strings before they are stored as memory
// content. It is conservative: a missed redaction is preferred over mangling
// legitimate content, so patterns are anchored and stand-alone.
package privacy

import (
	"encoding/json"
	"regexp"
	"sort"
)

// Result is the outcome of a Scrub call.
type Result struct {
	// Content is the input string with any matched secrets replaced.
	Content string
	// Hits maps pattern name -> number of matches redacted.
	Hits map[string]int
}

// Count returns the total number of redactions applied.
func (r Result) Count() int {
	n := 0
	for _, v := range r.Hits {
		n += v
	}
	return n
}

// Kinds returns a sorted list of the pattern names that matched.
func (r Result) Kinds() []string {
	out := make([]string, 0, len(r.Hits))
	for k := range r.Hits {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type pattern struct {
	name string
	re   *regexp.Regexp
	// replace overrides the default "[REDACTED:<name>]" template.
	// Use Go regexp replacement syntax ($1, $2, ...) if needed.
	replace string
}

// Patterns are applied in order. Multi-line / block patterns run first so
// they subsume any single-line keys that would otherwise match inside the
// block. The sk-ant- pattern runs before the generic sk- OpenAI pattern so
// Anthropic keys aren't mislabeled.
var patterns = []pattern{
	{name: "private_key_block", re: regexp.MustCompile(`(?s)-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY(?: BLOCK)?-----.*?-----END[^-]*-----`)},
	{name: "private_tag", re: regexp.MustCompile(`(?is)<private>.*?</private>`)},
	{name: "secret_tag", re: regexp.MustCompile(`(?is)<secret>.*?</secret>`)},
	{name: "anthropic_key", re: regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{20,}`)},
	{name: "openai_key", re: regexp.MustCompile(`\bsk-[A-Za-z0-9]{48}\b`)},
	{name: "github_pat", re: regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{82}\b`)},
	{name: "github_token", re: regexp.MustCompile(`\bghp_[A-Za-z0-9]{36}\b`)},
	{name: "aws_access_key", re: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{name: "aws_secret_key", re: regexp.MustCompile(`(?i)aws_secret_access_key\s*[=:]\s*[A-Za-z0-9/+=]{40}`)},
	{name: "slack_token", re: regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9\-]+`)},
	{name: "bearer_token", re: regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9_\-\.=]{20,}`)},
	{name: "url_creds", re: regexp.MustCompile(`(https?://)[^:/@\s]+:[^@\s]+@`), replace: "${1}[REDACTED:url_creds]@"},
}

// Scrub replaces recognized secrets in s with [REDACTED:<kind>] and returns
// the scrubbed content plus a tally of what was hit.
func Scrub(s string) Result {
	res := Result{Content: s, Hits: map[string]int{}}
	if s == "" {
		return res
	}
	for _, p := range patterns {
		matches := p.re.FindAllStringIndex(res.Content, -1)
		if len(matches) == 0 {
			continue
		}
		res.Hits[p.name] = len(matches)
		replace := p.replace
		if replace == "" {
			replace = "[REDACTED:" + p.name + "]"
		}
		res.Content = p.re.ReplaceAllString(res.Content, replace)
	}
	return res
}

// MergeHits combines two hit maps into a new one.
func MergeHits(a, b map[string]int) map[string]int {
	out := make(map[string]int, len(a)+len(b))
	for k, v := range a {
		out[k] += v
	}
	for k, v := range b {
		out[k] += v
	}
	return out
}

// MergeMetadata augments an existing metadata JSON string with scrub_count and
// scrub_kinds fields. If existing is empty or invalid JSON, a fresh object is
// produced. If the result has no hits, existing is returned unchanged.
func MergeMetadata(existing string, hits map[string]int) string {
	total := 0
	kinds := make([]string, 0, len(hits))
	for k, v := range hits {
		total += v
		kinds = append(kinds, k)
	}
	if total == 0 {
		return existing
	}
	sort.Strings(kinds)

	var obj map[string]any
	if existing != "" {
		_ = json.Unmarshal([]byte(existing), &obj)
	}
	if obj == nil {
		obj = map[string]any{}
	}
	obj["scrub_count"] = total
	obj["scrub_kinds"] = kinds

	b, err := json.Marshal(obj)
	if err != nil {
		return existing
	}
	return string(b)
}
