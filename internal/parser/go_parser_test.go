package parser

import (
	"strings"
	"testing"
)

// Shape taken from the Go SDK's cmd/objdump/testdata/fmthello.go, which is
// vendored into real repos. The //line directive remaps every position after
// it to ~1e6, far past the end of a 20-line file.
const lineDirectiveSrc = `package main

import "fmt"

func main() {
	Println("hello, world")
	if flag {
//line fmthello.go:999999
		Println("bad line")
		for {
		}
	}
}

//go:noinline
func Println(s string) {
	fmt.Println(s)
}

var flag bool
`

// Before the fix this panicked with "slice bounds out of range [1000005:21]"
// and killed the whole indexing run.
func TestParse_LineDirectiveBeyondEOF(t *testing.T) {
	p := &GoParser{}
	info, err := p.Parse("fmthello.go", []byte(lineDirectiveSrc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	total := len(strings.Split(lineDirectiveSrc, "\n"))
	byName := map[string]Symbol{}
	for _, s := range info.Symbols {
		byName[s.Name] = s
	}

	// Every symbol must land inside the physical file.
	for _, s := range info.Symbols {
		if s.LineStart < 1 || s.LineStart > total {
			t.Errorf("%s: LineStart %d outside file of %d lines", s.Name, s.LineStart, total)
		}
		if s.LineEnd < s.LineStart || s.LineEnd > total {
			t.Errorf("%s: LineEnd %d invalid (start %d, %d lines)", s.Name, s.LineEnd, s.LineStart, total)
		}
	}

	// Println is declared after the directive; its physical line is 16, and the
	// body we slice out must be the real source text, not empty or misaligned.
	println, ok := byName["Println"]
	if !ok {
		t.Fatalf("Println not extracted; got %d symbols", len(info.Symbols))
	}
	if println.LineStart != 16 {
		t.Errorf("Println LineStart = %d, want 16 (physical line)", println.LineStart)
	}
	if !strings.Contains(println.Body, "func Println(s string)") {
		t.Errorf("Println body does not contain its own declaration: %q", println.Body)
	}
}

func TestExtractLines_Bounds(t *testing.T) {
	lines := []string{"a", "b", "c"}

	tests := []struct {
		name       string
		start, end int
		want       string
	}{
		{"normal range", 1, 2, "a\nb"},
		{"single line", 2, 2, "b"},
		{"end past EOF clamps", 2, 99, "b\nc"},
		{"start below 1 clamps", -5, 1, "a"},
		{"start past EOF is empty", 99, 100, ""},
		{"inverted range is empty", 3, 1, ""},
		{"empty input", 1, 1, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := lines
			if tc.name == "empty input" {
				in = nil
			}
			if got := extractLines(in, tc.start, tc.end); got != tc.want {
				t.Errorf("extractLines(%d, %d) = %q, want %q", tc.start, tc.end, got, tc.want)
			}
		})
	}
}
