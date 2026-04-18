package contenthash

import "testing"

func TestComputeStable(t *testing.T) {
	h1 := Compute("title", "body", []string{"a", "b"})
	h2 := Compute("title", "body", []string{"a", "b"})
	if h1 != h2 {
		t.Errorf("expected identical hash for identical inputs, got %q vs %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex sha-256, got %d chars", len(h1))
	}
}

func TestComputeTagOrderAgnostic(t *testing.T) {
	a := Compute("t", "c", []string{"x", "y", "z"})
	b := Compute("t", "c", []string{"z", "x", "y"})
	if a != b {
		t.Errorf("tag order should not affect hash")
	}
}

func TestComputeTagCaseNormalized(t *testing.T) {
	a := Compute("t", "c", []string{"Foo", "bar"})
	b := Compute("t", "c", []string{"foo", "BAR"})
	if a != b {
		t.Errorf("tag case should not affect hash")
	}
}

func TestComputeWhitespaceNormalized(t *testing.T) {
	a := Compute("my title", "hello world", nil)
	b := Compute("my    title", "hello\n\tworld\n", nil)
	if a != b {
		t.Errorf("whitespace differences should not affect hash: %q vs %q", a, b)
	}
}

func TestComputeDifferentContent(t *testing.T) {
	a := Compute("t", "one", nil)
	b := Compute("t", "two", nil)
	if a == b {
		t.Errorf("different content should hash differently")
	}
}

func TestComputeDifferentTitle(t *testing.T) {
	a := Compute("title one", "c", nil)
	b := Compute("title two", "c", nil)
	if a == b {
		t.Errorf("different title should hash differently")
	}
}

func TestComputeEmptyContent(t *testing.T) {
	if h := Compute("t", "", []string{"x"}); h != "" {
		t.Errorf("empty content should produce empty hash, got %q", h)
	}
	if h := Compute("", "   ", nil); h != "" {
		t.Errorf("whitespace-only content should produce empty hash")
	}
}

func TestComputeTagSeparatorNotAmbiguous(t *testing.T) {
	// A tag containing unusual characters must not collide with a split version.
	a := Compute("t", "c", []string{"a,b"})
	b := Compute("t", "c", []string{"a", "b"})
	if a == b {
		t.Errorf("tags with delimiter-like chars must not collide with split tags")
	}
}
