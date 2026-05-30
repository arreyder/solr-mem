package privacy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScrubAnthropicKey(t *testing.T) {
	// Anthropic keys start with sk-ant-.
	in := "My key is sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA done"
	r := Scrub(in)
	if strings.Contains(r.Content, "sk-ant-") {
		t.Errorf("expected sk-ant- to be redacted, got: %s", r.Content)
	}
	if !strings.Contains(r.Content, "[REDACTED:anthropic_key]") {
		t.Errorf("expected REDACTED marker, got: %s", r.Content)
	}
	if r.Hits["anthropic_key"] != 1 {
		t.Errorf("expected 1 anthropic_key hit, got %d", r.Hits["anthropic_key"])
	}
}

func TestScrubAnthropicBeforeOpenAI(t *testing.T) {
	// sk-ant- keys must not be labeled as OpenAI keys.
	in := "sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	r := Scrub(in)
	if r.Hits["openai_key"] != 0 {
		t.Errorf("anthropic key should not match openai pattern, got %+v", r.Hits)
	}
	if r.Hits["anthropic_key"] != 1 {
		t.Errorf("expected anthropic match, got %+v", r.Hits)
	}
}

func TestScrubOpenAIKey(t *testing.T) {
	// OpenAI keys are sk- followed by exactly 48 alphanumeric chars.
	in := "use sk-" + strings.Repeat("a", 48) + " done"
	r := Scrub(in)
	if !strings.Contains(r.Content, "[REDACTED:openai_key]") {
		t.Errorf("expected openai redaction, got: %s", r.Content)
	}
}

func TestScrubGithubToken(t *testing.T) {
	in := "token=ghp_1234567890abcdefghijKLMNOPQRSTUVwxyz extra"
	r := Scrub(in)
	if r.Hits["github_token"] != 1 {
		t.Errorf("expected github_token hit, got %+v", r.Hits)
	}
	if !strings.Contains(r.Content, "[REDACTED:github_token]") {
		t.Errorf("expected redaction, got: %s", r.Content)
	}
}

func TestScrubGithubPAT(t *testing.T) {
	// github_pat_ has exactly 82 chars of [A-Za-z0-9_] after the prefix.
	suffix := strings.Repeat("a", 82)
	in := "token: github_pat_" + suffix
	r := Scrub(in)
	if r.Hits["github_pat"] != 1 {
		t.Errorf("expected github_pat hit, got %+v", r.Hits)
	}
}

func TestScrubAWSAccessKey(t *testing.T) {
	in := "AWS_KEY=AKIAIOSFODNN7EXAMPLE here"
	r := Scrub(in)
	if r.Hits["aws_access_key"] != 1 {
		t.Errorf("expected aws_access_key hit, got %+v", r.Hits)
	}
}

func TestScrubAWSSecretKey(t *testing.T) {
	in := "aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	r := Scrub(in)
	if r.Hits["aws_secret_key"] != 1 {
		t.Errorf("expected aws_secret_key hit, got %+v", r.Hits)
	}
}

func TestScrubSlackToken(t *testing.T) {
	in := "slack=xoxb-12345-67890-abcdef leaks"
	r := Scrub(in)
	if r.Hits["slack_token"] != 1 {
		t.Errorf("expected slack_token hit, got %+v", r.Hits)
	}
}

func TestScrubBearerToken(t *testing.T) {
	in := "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc"
	r := Scrub(in)
	if r.Hits["bearer_token"] != 1 {
		t.Errorf("expected bearer_token hit, got %+v", r.Hits)
	}
	if strings.Contains(r.Content, "eyJ") {
		t.Errorf("JWT should be redacted, got: %s", r.Content)
	}
}

func TestScrubPrivateKeyBlock(t *testing.T) {
	in := "note:\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAvZ\nnextline\n-----END RSA PRIVATE KEY-----\nend"
	r := Scrub(in)
	if r.Hits["private_key_block"] != 1 {
		t.Errorf("expected private_key_block hit, got %+v", r.Hits)
	}
	if strings.Contains(r.Content, "MIIEowIBAAK") {
		t.Errorf("key body should be gone, got: %s", r.Content)
	}
}

func TestScrubURLCreds(t *testing.T) {
	in := "connect https://admin:s3cr3t@db.example.com:5432/foo"
	r := Scrub(in)
	if r.Hits["url_creds"] != 1 {
		t.Errorf("expected url_creds hit, got %+v", r.Hits)
	}
	if !strings.Contains(r.Content, "https://[REDACTED:url_creds]@db.example.com") {
		t.Errorf("expected host preserved with redacted creds, got: %s", r.Content)
	}
}

func TestScrubPrivateTag(t *testing.T) {
	in := "before <private>hidden stuff</private> after"
	r := Scrub(in)
	if r.Hits["private_tag"] != 1 {
		t.Errorf("expected private_tag hit, got %+v", r.Hits)
	}
	if strings.Contains(r.Content, "hidden stuff") {
		t.Errorf("private content should be gone, got: %s", r.Content)
	}
}

func TestScrubSecretTag(t *testing.T) {
	in := "<secret>foo bar</secret>"
	r := Scrub(in)
	if r.Hits["secret_tag"] != 1 {
		t.Errorf("expected secret_tag hit, got %+v", r.Hits)
	}
}

func TestScrubEmpty(t *testing.T) {
	r := Scrub("")
	if r.Count() != 0 {
		t.Errorf("empty input should have no hits")
	}
}

func TestScrubNoMatches(t *testing.T) {
	in := "this is plain text with no secrets"
	r := Scrub(in)
	if r.Content != in {
		t.Errorf("no matches should leave content unchanged")
	}
	if r.Count() != 0 {
		t.Errorf("no matches should have zero count")
	}
}

func TestScrubMultipleKinds(t *testing.T) {
	in := "AKIAIOSFODNN7EXAMPLE and ghp_1234567890abcdefghijKLMNOPQRSTUVwxyz"
	r := Scrub(in)
	if r.Hits["aws_access_key"] != 1 || r.Hits["github_token"] != 1 {
		t.Errorf("expected both hits, got %+v", r.Hits)
	}
	if r.Count() != 2 {
		t.Errorf("expected total count 2, got %d", r.Count())
	}
	if got := r.Kinds(); len(got) != 2 || got[0] != "aws_access_key" || got[1] != "github_token" {
		t.Errorf("expected sorted kinds, got %v", got)
	}
}

func TestScrubIdempotent(t *testing.T) {
	// Scrubbing already-scrubbed content should be a no-op.
	in := "sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	first := Scrub(in)
	second := Scrub(first.Content)
	if second.Count() != 0 {
		t.Errorf("re-scrubbing should produce no hits, got %+v", second.Hits)
	}
	if second.Content != first.Content {
		t.Errorf("re-scrubbing changed content: %q -> %q", first.Content, second.Content)
	}
}

func TestMergeMetadataFresh(t *testing.T) {
	hits := map[string]int{"anthropic_key": 2, "github_token": 1}
	got := MergeMetadata("", hits)
	var obj map[string]any
	if err := json.Unmarshal([]byte(got), &obj); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if c, _ := obj["scrub_count"].(float64); int(c) != 3 {
		t.Errorf("expected scrub_count=3, got %v", obj["scrub_count"])
	}
	kinds, _ := obj["scrub_kinds"].([]any)
	if len(kinds) != 2 {
		t.Errorf("expected 2 kinds, got %v", kinds)
	}
}

func TestMergeMetadataExisting(t *testing.T) {
	existing := `{"source":"chat","importance":0.7}`
	hits := map[string]int{"slack_token": 1}
	got := MergeMetadata(existing, hits)
	var obj map[string]any
	if err := json.Unmarshal([]byte(got), &obj); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if obj["source"] != "chat" {
		t.Errorf("existing keys should be preserved, got %v", obj)
	}
	if c, _ := obj["scrub_count"].(float64); int(c) != 1 {
		t.Errorf("expected scrub_count=1, got %v", obj["scrub_count"])
	}
}

func TestMergeMetadataNoHits(t *testing.T) {
	existing := `{"foo":"bar"}`
	got := MergeMetadata(existing, map[string]int{})
	if got != existing {
		t.Errorf("no hits should leave metadata untouched, got %q", got)
	}
}

func TestMergeMetadataInvalidExisting(t *testing.T) {
	// Garbage existing metadata should be replaced by a fresh object.
	got := MergeMetadata("not-json", map[string]int{"github_token": 1})
	var obj map[string]any
	if err := json.Unmarshal([]byte(got), &obj); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}
	if _, ok := obj["scrub_count"]; !ok {
		t.Errorf("expected scrub_count key, got %v", obj)
	}
}

func TestMergeHits(t *testing.T) {
	a := map[string]int{"anthropic_key": 1, "github_token": 1}
	b := map[string]int{"anthropic_key": 2, "slack_token": 1}
	got := MergeHits(a, b)
	if got["anthropic_key"] != 3 {
		t.Errorf("expected sum for overlapping key, got %d", got["anthropic_key"])
	}
	if got["github_token"] != 1 || got["slack_token"] != 1 {
		t.Errorf("expected non-overlapping keys kept, got %+v", got)
	}
}
