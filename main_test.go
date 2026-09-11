package main

import (
	"encoding/json"
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
		Started:   time.Now().Add(-90 * time.Second),
	}
}

func get(t *testing.T, path string) (*http.Response, string) {
	t.Helper()
	srv := httptest.NewServer(routes(testInfo()))
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
}

func TestPage(t *testing.T) {
	res, body := get(t, "/")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("got status %d", res.StatusCode)
	}
	for _, want := range []string{"sha-1a2b3c4", `class="tag env-staging"`, "hello-staging", "hello-5d8f7c9b6-x2k4p", "1m 30s"} {
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

func TestBarcodeFollowsVersion(t *testing.T) {
	a, b := Info{Version: "sha-1a2b3c4"}, Info{Version: "sha-9f8e7d6"}
	if !reflect.DeepEqual(a.Barcode(), a.Barcode()) {
		t.Error("the same version must always draw the same barcode")
	}
	if reflect.DeepEqual(a.Barcode(), b.Barcode()) {
		t.Error("different versions should draw different barcodes")
	}
}

func TestUptime(t *testing.T) {
	now := time.Now()
	for d, want := range map[time.Duration]string{
		45 * time.Second:              "45s",
		3*time.Minute + 5*time.Second: "3m 5s",
		2*time.Hour + 7*time.Minute:   "2h 7m",
	} {
		if got := (Info{Started: now.Add(-d)}).Uptime(now); got != want {
			t.Errorf("uptime after %v: got %q, want %q", d, got, want)
		}
	}
}
