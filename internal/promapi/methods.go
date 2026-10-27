package promapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"systemdoc/internal/textutil"
)

// Ready reports whether the server answers /-/ready with success. The body is
// plain text and not stable, so only the status code is used.
func (c *Client) Ready(ctx context.Context) error {
	status, _, err := c.fetch(ctx, http.MethodGet, "/-/ready", nil)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("/-/ready: %w", ErrUnauthorized)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("/-/ready: HTTP %d from %s", status, c.String())
	}
	return nil
}

// BuildInfo returns the server version. It goes through the API prefix, so it
// also proves that the configured URL and token reach the API.
func (c *Client) BuildInfo(ctx context.Context) (BuildInfo, error) {
	var raw struct {
		Version   string `json:"version"`
		Revision  string `json:"revision"`
		GoVersion string `json:"goVersion"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/status/buildinfo", nil, &raw); err != nil {
		return BuildInfo{}, err
	}
	return BuildInfo{Version: textutil.Clean(raw.Version), Revision: short(raw.Revision), GoVersion: raw.GoVersion}, nil
}

func short(revision string) string {
	if len(revision) > 8 {
		return revision[:8]
	}
	return revision
}

// RuntimeInfo returns the server's runtime state; missing fields stay zero.
func (c *Client) RuntimeInfo(ctx context.Context) (RuntimeInfo, error) {
	var raw struct {
		StartTime           string `json:"startTime"`
		ReloadConfigSuccess bool   `json:"reloadConfigSuccess"`
		LastConfigTime      string `json:"lastConfigTime"`
		TimeSeriesCount     int64  `json:"timeSeriesCount"`
		StorageRetention    string `json:"storageRetention"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/status/runtimeinfo", nil, &raw); err != nil {
		return RuntimeInfo{}, err
	}
	return RuntimeInfo{
		StartTime:           parseTime(raw.StartTime),
		ReloadConfigSuccess: raw.ReloadConfigSuccess,
		LastConfigTime:      parseTime(raw.LastConfigTime),
		TimeSeriesCount:     raw.TimeSeriesCount,
		StorageRetention:    textutil.Clean(raw.StorageRetention),
	}, nil
}

type queryResult struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
}

// Query evaluates expr at the given instant (zero means now). A scalar result
// becomes one sample with empty labels.
func (c *Client) Query(ctx context.Context, expr string, at time.Time) ([]Sample, error) {
	form := url.Values{"query": {expr}}
	if !at.IsZero() {
		form.Set("time", strconv.FormatInt(at.Unix(), 10))
	}
	var result queryResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/query", form, &result); err != nil {
		return nil, err
	}
	switch result.ResultType {
	case "vector":
		var rows []struct {
			Metric map[string]string `json:"metric"`
			Value  json.RawMessage   `json:"value"`
		}
		if err := json.Unmarshal(result.Result, &rows); err != nil {
			return nil, fmt.Errorf("/api/v1/query: vector result: %w", err)
		}
		samples := make([]Sample, 0, len(rows))
		for _, row := range rows {
			point, err := parsePair(row.Value)
			if err != nil {
				return nil, fmt.Errorf("/api/v1/query: %w", err)
			}
			samples = append(samples, Sample{Labels: cleanLabels(row.Metric), At: point.At, Value: point.Value, Known: point.Known})
		}
		return samples, nil
	case "scalar":
		point, err := parsePair(result.Result)
		if err != nil {
			return nil, fmt.Errorf("/api/v1/query: %w", err)
		}
		return []Sample{{Labels: Labels{}, At: point.At, Value: point.Value, Known: point.Known}}, nil
	default:
		return nil, fmt.Errorf("/api/v1/query: %s result is not supported here", result.ResultType)
	}
}

// QueryRange evaluates expr over [start, end] at the given step. The caller
// chooses the step so the point count stays proportional to the chart width.
func (c *Client) QueryRange(ctx context.Context, expr string, start, end time.Time, step time.Duration) ([]Series, error) {
	if step <= 0 {
		return nil, fmt.Errorf("/api/v1/query_range: step must be positive")
	}
	form := url.Values{
		"query": {expr},
		"start": {strconv.FormatInt(start.Unix(), 10)},
		"end":   {strconv.FormatInt(end.Unix(), 10)},
		"step":  {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)},
	}
	var result queryResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/query_range", form, &result); err != nil {
		return nil, err
	}
	if result.ResultType != "matrix" {
		return nil, fmt.Errorf("/api/v1/query_range: %s result is not a matrix", result.ResultType)
	}
	var rows []struct {
		Metric map[string]string `json:"metric"`
		Values []json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(result.Result, &rows); err != nil {
		return nil, fmt.Errorf("/api/v1/query_range: matrix result: %w", err)
	}
	series := make([]Series, 0, len(rows))
	for _, row := range rows {
		s := Series{Labels: cleanLabels(row.Metric), Points: make([]Point, 0, len(row.Values))}
		for _, raw := range row.Values {
			point, err := parsePair(raw)
			if err != nil {
				return nil, fmt.Errorf("/api/v1/query_range: %w", err)
			}
			s.Points = append(s.Points, point)
		}
		series = append(series, s)
	}
	return series, nil
}

// MetricNames lists metric names with series matching any of match (bare
// metric names are valid selectors) since the given time. An empty match
// lists every name the server knows, which can be large; callers bound it.
func (c *Client) MetricNames(ctx context.Context, match []string, since time.Time) ([]string, error) {
	values, err := c.LabelValues(ctx, "__name__", match, since)
	if err != nil {
		return nil, err
	}
	names := values[:0]
	for _, name := range values {
		if ValidMetricName(name) {
			names = append(names, name)
		}
	}
	return names, nil
}

// LabelValues lists the values of one label, optionally restricted to series
// matching the selectors since the given time. Values are cleaned and sorted.
func (c *Client) LabelValues(ctx context.Context, label string, match []string, since time.Time) ([]string, error) {
	if !ValidMetricName(label) {
		return nil, fmt.Errorf("label name %q is not valid", label)
	}
	form := url.Values{}
	for _, selector := range match {
		form.Add("match[]", selector)
	}
	if !since.IsZero() {
		form.Set("start", strconv.FormatInt(since.Unix(), 10))
	}
	var values []string
	if err := c.do(ctx, http.MethodGet, "/api/v1/label/"+label+"/values", form, &values); err != nil {
		return nil, err
	}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		cleaned = append(cleaned, textutil.Clean(value))
	}
	sort.Strings(cleaned)
	return cleaned, nil
}

// Targets lists active scrape targets. Dropped targets are not requested
// because a large service-discovery setup can report thousands of them.
func (c *Client) Targets(ctx context.Context) ([]Target, error) {
	var raw struct {
		Active []struct {
			Labels             map[string]string `json:"labels"`
			ScrapePool         string            `json:"scrapePool"`
			ScrapeURL          string            `json:"scrapeUrl"`
			Health             string            `json:"health"`
			LastError          string            `json:"lastError"`
			LastScrape         string            `json:"lastScrape"`
			LastScrapeDuration float64           `json:"lastScrapeDuration"`
			ScrapeInterval     string            `json:"scrapeInterval"`
		} `json:"activeTargets"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/targets", url.Values{"state": {"active"}}, &raw); err != nil {
		return nil, err
	}
	targets := make([]Target, 0, len(raw.Active))
	for _, row := range raw.Active {
		labels := cleanLabels(row.Labels)

		interval, _ := time.ParseDuration(row.ScrapeInterval)
		targets = append(targets, Target{
			Job:            labels["job"],
			Instance:       labels["instance"],
			ScrapeURL:      textutil.Clean(row.ScrapeURL),
			Health:         textutil.Clean(row.Health),
			LastError:      firstLine(textutil.Clean(row.LastError)),
			Pool:           textutil.Clean(row.ScrapePool),
			LastScrape:     parseTime(row.LastScrape),
			ScrapeDuration: time.Duration(row.LastScrapeDuration * float64(time.Second)),
			ScrapeInterval: interval,
			Labels:         labels,
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Job != targets[j].Job {
			return targets[i].Job < targets[j].Job
		}
		return targets[i].Instance < targets[j].Instance
	})
	return targets, nil
}
