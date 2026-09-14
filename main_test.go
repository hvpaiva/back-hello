package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testInfo() Info {
	return Info{
		Service:   "hello",
		Version:   "sha-1a2b3c4",
		Env:       "staging",
		Pod:       "hello-5d8f7c9b6-x2k4p",
		Namespace: "hello-staging",
		Node:      "back-control-plane",
		Spec:      Spec{Size: "medium", Replicas: 2, CPU: "100m", Memory: "128Mi", Public: true},
		Started:   time.Now().Add(-90 * time.Second),
	}
}

func get(t *testing.T, path string) (*http.Response, string) {
	t.Helper()
	return getWith(t, path, nil, nil)
}

func getWith(t *testing.T, path string, bucket *bucketProbe, database *databaseProbe) (*http.Response, string) {
	t.Helper()
	srv := httptest.NewServer(routes(testInfo(), bucket, database))
	defer srv.Close()
	res, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(body)
}

func TestHealthz(t *testing.T) {
	res, body := get(t, "/healthz")
	if res.StatusCode != http.StatusOK || body != "ok\n" {
		t.Fatalf("got %d %q, want 200 \"ok\\n\"", res.StatusCode, body)
	}
}

func TestInfoAPI(t *testing.T) {
	res, body := get(t, "/api/info")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("got status %d", res.StatusCode)
	}
	var got Info
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != "sha-1a2b3c4" || got.Env != "staging" || got.Namespace != "hello-staging" {
		t.Fatalf("unexpected info: %+v", got)
	}
	if got.Bucket != nil {
		t.Fatalf("a service without a bucket reported one: %+v", got.Bucket)
	}
	if got.Database != nil {
		t.Fatalf("a service without a database reported one: %+v", got.Database)
	}
}

func TestBucket(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		state string
	}{
		{"reachable", nil, "reachable"},
		{"unreachable", errors.New("connection refused"), "unreachable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &bucketProbe{name: "hello-staging-hello", check: func(context.Context) error { return tc.err }}
			_, body := getWith(t, "/api/info", probe, nil)
			var got Info
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatal(err)
			}
			want := &BucketStatus{Name: "hello-staging-hello", Reachable: tc.err == nil}
			if tc.err != nil {
				want.Error = tc.err.Error()
			}
			if !reflect.DeepEqual(got.Bucket, want) {
				t.Fatalf("bucket: got %+v, want %+v", got.Bucket, want)
			}
			_, page := getWith(t, "/", probe, nil)
			if !strings.Contains(page, "hello-staging-hello") || !strings.Contains(page, ">"+tc.state+"<") {
				t.Errorf("page doesn't show the bucket as %s", tc.state)
			}
		})
	}
}

func TestDatabase(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		state string
	}{
		{"reachable", nil, "reachable"},
		{"unreachable", errors.New("connection refused"), "unreachable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &databaseProbe{host: "hello-postgres-rw", size: "small", check: func(context.Context) error { return tc.err }}
			_, body := getWith(t, "/api/info", nil, probe)
			var got Info
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatal(err)
			}
			want := &DatabaseStatus{Host: "hello-postgres-rw", Size: "small", Reachable: tc.err == nil}
			if tc.err != nil {
				want.Error = tc.err.Error()
			}
			if !reflect.DeepEqual(got.Database, want) {
				t.Fatalf("database: got %+v, want %+v", got.Database, want)
			}
			_, page := getWith(t, "/", nil, probe)
			if !strings.Contains(page, "hello-postgres-rw") || !strings.Contains(page, ">"+tc.state+"<") {
				t.Errorf("page doesn't show the database as %s", tc.state)
			}
		})
	}
}

func TestDatabaseOutsideCluster(t *testing.T) {
	if probe := newDatabaseProbe(context.Background(), ""); probe != nil {
		t.Fatalf("a probe without a host: %+v", probe)
	}
}

func TestPage(t *testing.T) {
	res, body := get(t, "/")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("got status %d", res.StatusCode)
	}
	for _, want := range []string{
		"sha-1a2b3c4", `class="spin-inner env-staging"`, "hello-staging", "hello-5d8f7c9b6-x2k4p", "data-started=",
		`data-declared="2"`, ">medium<", "100m cpu",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}

func TestUnknownPath(t *testing.T) {
	res, _ := get(t, "/nope")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("got status %d, want 404", res.StatusCode)
	}
}

func TestFonts(t *testing.T) {
	res, _ := get(t, "/fonts/barlow-condensed-700.woff2")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("got status %d", res.StatusCode)
	}
}

func TestFavicon(t *testing.T) {
	res, _ := get(t, "/favicon.png")
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("got %d %q, want 200 image/png", res.StatusCode, res.Header.Get("Content-Type"))
	}
}

func TestLogo(t *testing.T) {
	res, _ := get(t, "/logo.png")
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("got %d %q, want 200 image/png", res.StatusCode, res.Header.Get("Content-Type"))
	}
}

func TestSpec(t *testing.T) {
	t.Setenv("APP_SIZE", "medium")
	t.Setenv("APP_REPLICAS", "2")
	t.Setenv("APP_CPU", "100m")
	t.Setenv("APP_MEMORY", "128Mi")
	t.Setenv("APP_MEMORY_LIMIT", "256Mi")
	t.Setenv("APP_PUBLIC", "true")
	want := Spec{Size: "medium", Replicas: 2, CPU: "100m", Memory: "128Mi", MemoryLimit: "256Mi", Public: true}
	if got := newSpec(); !reflect.DeepEqual(got, want) {
		t.Fatalf("spec: got %+v, want %+v", got, want)
	}
}

// Outside a cluster nothing is declared, and the page shows none of it.
func TestSpecOutsideCluster(t *testing.T) {
	if got := newSpec(); got.Replicas != 0 || got.Size != "" {
		t.Fatalf("spec outside a cluster: got %+v", got)
	}
}

func TestBarcodeFollowsVersion(t *testing.T) {
	a, b := Info{Version: "sha-1a2b3c4"}, Info{Version: "sha-9f8e7d6"}
	if !reflect.DeepEqual(a.Barcode(), a.Barcode()) {
		t.Error("the same version must always draw the same barcode")
	}
	if reflect.DeepEqual(a.Barcode(), b.Barcode()) {
		t.Error("different versions should draw different barcodes")
	}
}
