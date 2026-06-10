package solr

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// BuildQueryString constructs a Solr query string from QueryParams.
func BuildQueryString(p QueryParams) string {
	v := url.Values{}
	v.Set("q", p.Query)
	v.Set("wt", "json")

	if p.Rows > 0 {
		v.Set("rows", fmt.Sprintf("%d", p.Rows))
	}
	if p.Start > 0 {
		v.Set("start", fmt.Sprintf("%d", p.Start))
	}
	if p.Sort != "" {
		v.Set("sort", p.Sort)
	}
	if p.MM != "" {
		v.Set("mm", p.MM)
	}
	if len(p.Fields) > 0 {
		v.Set("fl", strings.Join(p.Fields, ","))
	}
	for _, fq := range p.FilterQueries {
		v.Add("fq", fq)
	}

	if p.Highlight {
		v.Set("hl", "on")
		v.Set("hl.fl", "content,title")
	} else {
		v.Set("hl", "off")
	}

	if p.Facet {
		v.Set("facet", "on")
		if len(p.FacetFields) > 0 {
			for _, ff := range p.FacetFields {
				v.Add("facet.field", ff)
			}
		}
	} else {
		v.Set("facet", "off")
	}

	return v.Encode()
}

// ParseQueryResponse reads a Solr JSON response and extracts docs, highlighting, and facets.
func ParseQueryResponse(r io.Reader) (*QueryResponse, error) {
	var raw struct {
		Response struct {
			NumFound int              `json:"numFound"`
			Start    int              `json:"start"`
			Docs     []map[string]any `json:"docs"`
		} `json:"response"`
		Highlighting map[string]map[string][]string `json:"highlighting"`
		FacetCounts  *struct {
			FacetFields map[string]json.RawMessage `json:"facet_fields"`
		} `json:"facet_counts"`
	}

	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode solr response: %w", err)
	}

	result := &QueryResponse{
		NumFound:     raw.Response.NumFound,
		Start:        raw.Response.Start,
		Docs:         raw.Response.Docs,
		Highlighting: raw.Highlighting,
		Facets:       make(map[string][]FacetCount),
	}

	// Parse facet fields: Solr returns alternating [value, count, value, count, ...]
	if raw.FacetCounts != nil {
		for field, rawValues := range raw.FacetCounts.FacetFields {
			var items []any
			if err := json.Unmarshal(rawValues, &items); err != nil {
				continue
			}
			var facets []FacetCount
			for i := 0; i+1 < len(items); i += 2 {
				val, _ := items[i].(string)
				count, _ := items[i+1].(float64)
				if val != "" && int(count) > 0 {
					facets = append(facets, FacetCount{Value: val, Count: int(count)})
				}
			}
			result.Facets[field] = facets
		}
	}

	return result, nil
}
