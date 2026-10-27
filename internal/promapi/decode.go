package promapi

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"systemdoc/internal/textutil"
)

// envelope is the common /api/v1 response shape.
type envelope struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	ErrorType string          `json:"errorType"`
	Error     string          `json:"error"`
	Warnings  []string        `json:"warnings"`
}

// parseValue decodes the API's string-encoded sample value. NaN and the
// infinities are reported as unknown rather than as numbers a chart would draw.
func parseValue(raw json.RawMessage) (float64, bool, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, false, fmt.Errorf("sample value is not a string: %w", err)
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false, fmt.Errorf("sample value %q: %w", textutil.Clean(text), err)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false, nil
	}
	return value, true, nil
}

// parsePair decodes a [unixSeconds, "value"] pair.
func parsePair(raw json.RawMessage) (Point, error) {
	var pair []json.RawMessage
	if err := json.Unmarshal(raw, &pair); err != nil || len(pair) != 2 {
		return Point{}, fmt.Errorf("sample is not a [time, value] pair")
	}
	var seconds float64
	if err := json.Unmarshal(pair[0], &seconds); err != nil {
		return Point{}, fmt.Errorf("sample time: %w", err)
	}
	value, known, err := parseValue(pair[1])
	if err != nil {
		return Point{}, err
	}
	whole, fraction := math.Modf(seconds)
	return Point{At: time.Unix(int64(whole), int64(fraction*1e9)).UTC(), Value: value, Known: known}, nil
}

func cleanLabels(in map[string]string) Labels {
	out := make(Labels, len(in))
	for key, value := range in {
		out[textutil.Clean(key)] = textutil.Clean(value)
	}
	return out
}

// parseTime accepts RFC 3339 with or without fractional seconds; Prometheus
// reports an unset time as the zero RFC 3339 instant, which stays zero here.
func parseTime(text string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, text); err == nil {
			if parsed.Year() <= 1 {
				return time.Time{}
			}
			return parsed
		}
	}
	return time.Time{}
}

// firstLine keeps server messages to one line for status rows.
func firstLine(text string) string {
	for i, r := range text {
		if r == '\n' || r == '\r' {
			return text[:i]
		}
	}
	return text
}
