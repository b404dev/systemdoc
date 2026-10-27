package promapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func serve(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestNewValidation(t *testing.T) {
	cases := []struct{ raw, want, problem string }{
		{"", "", "no Prometheus URL"},
		{"localhost:9090", "http://localhost:9090", ""},
		{"http://localhost:9090/", "http://localhost:9090", ""},
		{"https://prom.example/prom/", "https://prom.example/prom", ""},
		{"ftp://host", "", "http or https"},
		{"http://user:pw@host:9090", "", "username or password"},
		{"http://host/?x=1", "", "query string"},
		{"http://host/#frag", "", "query string"},
		{"http://", "", "needs a host"},
	}
	for _, c := range cases {
		client, err := New(c.raw, "")
		if c.problem != "" {
			if err == nil || !strings.Contains(err.Error(), c.problem) {
				t.Errorf("%q: want error containing %q, got %v", c.raw, c.problem, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.raw, err)
			continue
		}
		if client.String() != c.want {
			t.Errorf("%q: want %q, got %q", c.raw, c.want, client.String())
		}
	}
}

func TestQueryVectorScalarAndUnknownValues(t *testing.T) {
	var method, contentType, body string
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		method, contentType = r.Method, r.Header.Get("Content-Type")
		raw, _ := readAll(r)
		body = raw
		if strings.Contains(raw, "scalar") {
			w.Write(fixture(t, "query-scalar.json"))
			return
		}
		w.Write(fixture(t, "query-vector.json"))
	})
	samples, err := client.Query(context.Background(), "node_load1", time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || contentType != "application/x-www-form-urlencoded" || !strings.Contains(body, "query=node_load1") || !strings.Contains(body, "time=1700000000") {
		t.Fatalf("query must be a POST form: %s %s %q", method, contentType, body)
	}
	if len(samples) != 3 {
		t.Fatalf("want 3 samples, got %d", len(samples))
	}
	if !samples[0].Known || samples[0].Value != 1.25 || samples[0].Labels["instance"] != "localhost:9100" || samples[0].At.Unix() != 1700000000 {
		t.Errorf("first sample: %+v", samples[0])
	}
	if samples[1].Known || samples[2].Known {
		t.Errorf("NaN and Inf must be unknown: %+v %+v", samples[1], samples[2])
	}
	if strings.Contains(samples[1].Labels["instance"], "\x1b") {
		t.Errorf("label value was not cleaned: %q", samples[1].Labels["instance"])
	}
	scalar, err := client.Query(context.Background(), "scalar(1)", time.Time{})
	if err != nil || len(scalar) != 1 || scalar[0].Value != 42 || len(scalar[0].Labels) != 0 {
		t.Fatalf("scalar: %v %+v", err, scalar)
	}
}

func TestQueryRejectsMatrix(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) { w.Write(fixture(t, "query-matrix.json")) })
	if _, err := client.Query(context.Background(), "x", time.Time{}); err == nil || !strings.Contains(err.Error(), "matrix") {
		t.Fatalf("want matrix rejection, got %v", err)
	}
}

func TestQueryRangeMatrix(t *testing.T) {
	var body string
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ = readAll(r)
		w.Write(fixture(t, "query-matrix.json"))
	})
	start, end := time.Unix(1700000000, 0), time.Unix(1700000045, 0)
	series, err := client.QueryRange(context.Background(), "rate(x[1m])", start, end, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "step=15") || !strings.Contains(body, "start=1700000000") || !strings.Contains(body, "end=1700000045") {
		t.Errorf("form: %q", body)
	}
	if len(series) != 2 || len(series[0].Points) != 4 || series[0].Labels["cpu"] != "0" {
		t.Fatalf("series: %+v", series)
	}
	if series[0].Points[2].Known || series[0].Points[3].Value != 4 {
		t.Errorf("points: %+v", series[0].Points)
	}
	if _, err := client.QueryRange(context.Background(), "x", start, end, 0); err == nil {
		t.Error("zero step must be rejected")
	}
}

func TestMetricNamesSortedCleanedFiltered(t *testing.T) {
	var query string
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/label/__name__/values" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Write(fixture(t, "label-values.json"))
	})
	names, err := client.MetricNames(context.Background(), []string{"node_cpu_seconds_total", "pve_up"}, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "match%5B%5D=node_cpu_seconds_total") || !strings.Contains(query, "match%5B%5D=pve_up") || !strings.Contains(query, "start=1700000000") {
		t.Errorf("query: %q", query)
	}
	want := []string{"alpha_metric", "node_cpu_seconds_total", "okname", "zeta_metric"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("names: %v", names)
	}
	if _, err := client.LabelValues(context.Background(), "bad label", nil, time.Time{}); err == nil {
		t.Error("invalid label name must be rejected before any request")
	}
}

func TestTargets(t *testing.T) {
	var query string
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Write(fixture(t, "targets.json"))
	})
	targets, err := client.Targets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if query != "state=active" {
		t.Errorf("query: %q", query)
	}
	if len(targets) != 2 || targets[0].Job != "node" || targets[0].Health != "up" || targets[0].ScrapeInterval != 15*time.Second || targets[0].ScrapeDuration != 12300*time.Microsecond || targets[0].LastScrape.IsZero() {
		t.Fatalf("targets: %+v", targets)
	}
	down := targets[1]
	if down.Health != "down" || !down.LastScrape.IsZero() || strings.Contains(down.LastError, "\n") || !strings.Contains(down.LastError, "connection refused") {
		t.Errorf("down target: %+v", down)
	}
}

func TestBuildInfoAndRuntimeInfo(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.Write(fixture(t, "buildinfo.json"))
		case "/api/v1/status/runtimeinfo":
			w.Write(fixture(t, "runtimeinfo.json"))
		default:
			http.NotFound(w, r)
		}
	})
	build, err := client.BuildInfo(context.Background())
	if err != nil || build.Version != "2.53.1" || build.Revision != "01234567" {
		t.Fatalf("buildinfo: %v %+v", err, build)
	}
	runtime, err := client.RuntimeInfo(context.Background())
	if err != nil || !runtime.ReloadConfigSuccess || runtime.TimeSeriesCount != 48213 || runtime.StorageRetention != "15d" || runtime.StartTime.IsZero() {
		t.Fatalf("runtimeinfo: %v %+v", err, runtime)
	}
}

func TestReady(t *testing.T) {
	status := http.StatusOK
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/-/ready" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.WriteHeader(status)
		w.Write([]byte("Prometheus Server is Ready.\n"))
	})
	if err := client.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	status = http.StatusServiceUnavailable
	if err := client.Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("want 503 error, got %v", err)
	}
}

func TestBearerHeaderSentAndNeverInErrors(t *testing.T) {
	const token = "s3cret-token-value"
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom " + seen))
	}))
	defer server.Close()
	client, err := New(server.URL+"/prom", token)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.BuildInfo(context.Background())
	if seen != "Bearer "+token {
		t.Fatalf("authorization header: %q", seen)
	}
	if err == nil || strings.Contains(err.Error(), token) || strings.Contains(client.String(), token) {
		t.Fatalf("token leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("want HTTP 500 in error, got %v", err)
	}
}

func TestEnvelopeErrorAndUnauthorized(t *testing.T) {
	status := http.StatusBadRequest
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write(fixture(t, "error-bad-data.json"))
	})
	_, err := client.Query(context.Background(), "x(", time.Time{})
	if err == nil || !strings.Contains(err.Error(), "bad_data") || !strings.Contains(err.Error(), "parse error") || strings.Contains(err.Error(), "more") {
		t.Fatalf("envelope error: %v", err)
	}
	status = http.StatusUnauthorized
	_, err = client.Query(context.Background(), "x", time.Time{})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestNonJSONBodyAndRedirect(t *testing.T) {
	hits := 0
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.Header().Set("Location", "/elsewhere")
			w.WriteHeader(http.StatusFound)
		case "/elsewhere":
			t.Error("redirect must not be followed")
		default:
			w.Write([]byte("<html>grafana</html>"))
		}
	})
	_, err := client.Query(context.Background(), "x", time.Time{})
	if err == nil || !strings.Contains(err.Error(), "not a Prometheus API response") {
		t.Fatalf("html body: %v", err)
	}
	_, err = client.BuildInfo(context.Background())
	if err == nil || !strings.Contains(err.Error(), "redirected") || hits != 2 {
		t.Fatalf("redirect: %v (hits %d)", err, hits)
	}
}

func TestBodyLimit(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"success","data":"`))
		chunk := strings.Repeat("x", 64*1024)
		for i := 0; i < 80; i++ {
			w.Write([]byte(chunk))
		}
		w.Write([]byte(`"}`))
	})
	_, err := client.BuildInfo(context.Background())
	if err == nil || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("want size error, got %v", err)
	}
}

func TestContextCancel(t *testing.T) {
	release := make(chan struct{})
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	started := time.Now()
	_, err := client.BuildInfo(ctx)
	if !errors.Is(err, context.Canceled) || time.Since(started) > time.Second {
		t.Fatalf("want prompt cancellation, got %v after %s", err, time.Since(started))
	}
}

func TestPrefixPathJoined(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Write(fixture(t, "buildinfo.json"))
	}))
	defer server.Close()
	client, err := New(server.URL+"/prom/", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.BuildInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if path != "/prom/api/v1/status/buildinfo" {
		t.Errorf("path: %q", path)
	}
}

func TestTLSUsesSystemRootsOnly(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(fixture(t, "buildinfo.json")) }))
	defer server.Close()
	client, err := New(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.BuildInfo(context.Background()); err == nil || !strings.Contains(strings.ToLower(err.Error()), "certificate") {
		t.Fatalf("self-signed certificate must fail with system roots: %v", err)
	}
}

func readAll(r *http.Request) (string, error) {
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			return b.String(), nil
		}
	}
}
