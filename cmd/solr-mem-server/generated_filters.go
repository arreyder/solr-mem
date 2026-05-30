package main

import "strings"

// generatedFilePatterns are file_path wildcard patterns for generated code that
// is still indexed (Go protobuf/mocks are already dropped at index time, but
// these are not). Excluding them keeps hand-written code from being buried.
//
// Solr matches these wildcards against the file_path term dictionary (a few
// tens of thousands of distinct paths), so they are cheap as filter clauses.
var generatedFilePatterns = []string{
	"*_pb.ts",        // TS protobuf messages
	"*.pb.ts",        // TS protobuf (alt)
	"*_pb.d.ts",      // TS protobuf type defs
	"*_connectquery.ts", // connect-query generated clients
	"*_pb2.py",       // python protobuf
	"*_pb2_grpc.py",  // python grpc stubs
	"*.pb.go",   // go protobuf base (already index-skipped; belt-and-suspenders)
	"*.pb.*.go", // any protoc plugin output: .pb.gw.go, .pb.validate.go, .pb.authz.go, .pb.pgdb.go, …
	"*/pbts/*",  // generated TS protobuf tree (files named e.g. user.ts, not *_pb.ts)
	"pbts/*",    // same, at repo root
}

// generatedExclusionFilter returns a single Solr filter query that excludes
// vendored and generated code, or "" if exclusion is disabled. Vendored code
// carries a "vendor" tag (exact match); generated code is matched by file path.
func generatedExclusionFilter(exclude bool) string {
	if !exclude {
		return ""
	}
	clauses := []string{"*:*", `-tags:"vendor"`}
	for _, pat := range generatedFilePatterns {
		clauses = append(clauses, "-file_path:"+pat)
	}
	return strings.Join(clauses, " ")
}
