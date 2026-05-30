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
	"*.pb.go",        // go protobuf (already index-skipped; belt-and-suspenders for older indexes)
	"*.pb.gw.go",     // grpc-gateway
	"*.pb.validate.go", // protoc-gen-validate
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
