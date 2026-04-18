package main

import (
	"log"
	"os"
	"strings"
	"sync"

	"github.com/arreyder/solr-mem/internal/privacy"
)

// privacyScrubDisabled is resolved once from the environment.
var (
	privacyScrubOnce sync.Once
	privacyScrubOff  bool
)

// privacyScrubEnabled reports whether secret scrubbing is on. Controlled by
// SOLR_MEM_PRIVACY_SCRUB=off (default: on).
func privacyScrubEnabled() bool {
	privacyScrubOnce.Do(func() {
		v := strings.ToLower(strings.TrimSpace(os.Getenv("SOLR_MEM_PRIVACY_SCRUB")))
		if v == "off" || v == "false" || v == "0" || v == "disabled" {
			privacyScrubOff = true
			log.Println("privacy: scrub disabled by SOLR_MEM_PRIVACY_SCRUB")
		}
	})
	return !privacyScrubOff
}

// scrubString returns a scrubbed copy of s and the hit map; if scrubbing is
// disabled or s is empty, the hit map is nil.
func scrubString(s string) (string, map[string]int) {
	if !privacyScrubEnabled() || s == "" {
		return s, nil
	}
	r := privacy.Scrub(s)
	if r.Count() == 0 {
		return r.Content, nil
	}
	return r.Content, r.Hits
}
