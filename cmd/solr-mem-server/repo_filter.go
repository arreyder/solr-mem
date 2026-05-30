package main

import (
	"fmt"
	"strings"
)

// repoFilterQuery builds a Solr filter-query for the repo_url field.
//
// repo_url is stored as the full path the indexer saw, which varies by source:
//   - local-path indexer: "/Users/<user>/solr-mem-repos/ductone_c1"
//   - git-URL indexer:    "git@github.com:ductone/c1.git"
//
// Callers (agents) naturally pass the short project name "ductone/c1", which
// exact-matches neither form and silently returns zero results — easily
// mistaken for "not indexed". To avoid that trap we normalize short names into
// a wildcard that matches both stored forms, while still honoring an explicit
// full path or URL as an exact match.
//
// Returns "" when v is empty (no filter should be applied).
func repoFilterQuery(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}

	// A full local path or a git/HTTP URL is already a concrete stored value —
	// match it exactly (phrase query on the string field).
	if strings.HasPrefix(v, "/") || strings.Contains(v, "://") || strings.Contains(v, "@") {
		return fmt.Sprintf("repo_url:%q", v)
	}

	// Short name like "ductone/c1", "ductone_c1", or "ductone/c1.git".
	// Split on the separators that differ between stored forms ('/' vs '_')
	// and rejoin with the single-char wildcard '?' so either form matches.
	name := strings.TrimSuffix(v, ".git")
	segs := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '_' })
	if len(segs) == 0 {
		return fmt.Sprintf("repo_url:%q", v)
	}
	for i, s := range segs {
		segs[i] = escapeSolrTerm(s)
	}
	return "repo_url:*" + strings.Join(segs, "?") + "*"
}

// escapeSolrTerm backslash-escapes Lucene query-syntax metacharacters in a
// literal term so it can be embedded safely inside a wildcard query. It does
// not escape '*' or '?' — callers add those deliberately — but the segments
// passed here never contain them.
func escapeSolrTerm(s string) string {
	const special = `+-&|!(){}[]^"~:\/ ` // chars Lucene treats specially
	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, r := range s {
		if strings.ContainsRune(special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
