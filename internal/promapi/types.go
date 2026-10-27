// Package promapi queries a Prometheus server's HTTP API with bounded requests
// and decodes the responses into plain values. PromQL construction belongs to
// the caller; this package owns transport, decoding and the panel catalog.
package promapi

import (
	"regexp"
	"time"
)

// Labels is a metric's label set. Keys and values have been passed through
// textutil.Clean; callers still escape terminal markup before rendering.
type Labels map[string]string

// Name returns the metric name carried in __name__.
func (l Labels) Name() string { return l["__name__"] }

// Sample is one instant-query result. Known is false when the server reported
// NaN or an infinity, which are the API's encodings of undefined data.
type Sample struct {
	Labels Labels
	At     time.Time
	Value  float64
	Known  bool
}

// Point is one value in a range-query series.
type Point struct {
	At    time.Time
	Value float64
	Known bool
}

// Series is one range-query result.
type Series struct {
	Labels Labels
	Points []Point
}

// Target is one active scrape target.
type Target struct {
	Job, Instance  string
	ScrapeURL      string
	Health         string // up, down or unknown
	LastError      string
	Pool           string
	LastScrape     time.Time
	ScrapeDuration time.Duration
	ScrapeInterval time.Duration
	Labels         Labels
}

// BuildInfo describes the server binary.
type BuildInfo struct {
	Version, Revision, GoVersion string
}

// RuntimeInfo is the server's self-reported runtime state. Prometheus warns
// that these fields may change between versions, so every field is optional.
type RuntimeInfo struct {
	StartTime           time.Time
	ReloadConfigSuccess bool
	LastConfigTime      time.Time
	TimeSeriesCount     int64
	StorageRetention    string
}

var metricName = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

// ValidMetricName reports whether name is a syntactically valid metric name.
func ValidMetricName(name string) bool { return metricName.MatchString(name) }
