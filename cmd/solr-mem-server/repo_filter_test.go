package main

import "testing"

func TestRepoFilterQuery(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace", "   ", ""},
		{"short slash form", "ductone/c1", "repo_url:*ductone?c1*"},
		{"short underscore form", "ductone_c1", "repo_url:*ductone?c1*"},
		{"short with .git suffix", "ductone/c1.git", "repo_url:*ductone?c1*"},
		{"hyphenated repo name", "ConductorOne/baton-sdk", `repo_url:*ConductorOne?baton\-sdk*`},
		{"absolute local path is exact", "/Users/arreyder/solr-mem-repos/ductone_c1", `repo_url:"/Users/arreyder/solr-mem-repos/ductone_c1"`},
		{"git url is exact", "git@github.com:ductone/c1.git", `repo_url:"git@github.com:ductone/c1.git"`},
		{"https url is exact", "https://github.com/ductone/c1", `repo_url:"https://github.com/ductone/c1"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := repoFilterQuery(tt.in); got != tt.want {
				t.Errorf("repoFilterQuery(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// The short-name wildcard must match both stored forms a repo can take:
// the local-path clone (underscore separator) and the git URL (slash + .git).
func TestRepoFilterQueryMatchesBothStoredForms(t *testing.T) {
	fq := repoFilterQuery("ductone/c1")
	if fq != "repo_url:*ductone?c1*" {
		t.Fatalf("unexpected fq: %q", fq)
	}
	// '?' matches exactly one char: '_' in the local path and '/' in the git URL.
	for _, stored := range []string{
		"/Users/arreyder/solr-mem-repos/ductone_c1",
		"git@github.com:ductone/c1.git",
	} {
		if !wildcardMatches("*ductone?c1*", stored) {
			t.Errorf("pattern should match stored form %q", stored)
		}
	}
}

// wildcardMatches is a tiny Lucene-style matcher ('*' = any run, '?' = one
// char) used only to assert the generated pattern matches the stored forms.
func wildcardMatches(pat, s string) bool {
	// dp over pattern/string indices
	m, n := len(pat), len(s)
	dp := make([][]bool, m+1)
	for i := range dp {
		dp[i] = make([]bool, n+1)
	}
	dp[0][0] = true
	for i := 1; i <= m; i++ {
		if pat[i-1] == '*' {
			dp[i][0] = dp[i-1][0]
		}
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			switch pat[i-1] {
			case '*':
				dp[i][j] = dp[i-1][j] || dp[i][j-1]
			case '?':
				dp[i][j] = dp[i-1][j-1]
			default:
				dp[i][j] = dp[i-1][j-1] && pat[i-1] == s[j-1]
			}
		}
	}
	return dp[m][n]
}
