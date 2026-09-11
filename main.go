// Command hello is the lab's sample service: a status page that shows which
// version is running, in which environment, and on which pod.
package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// version is set at build time: go build -ldflags "-X main.version=sha-1a2b3c4".
var version = "local"

//go:embed web
var web embed.FS

var page = template.Must(template.ParseFS(web, "web/index.html"))

// Info is what the page shows, also served as JSON at /api/info.
type Info struct {
	Service   string    `json:"service"`
	Version   string    `json:"version"`
	Env       string    `json:"env"`
	Pod       string    `json:"pod"`
	Namespace string    `json:"namespace"`
	Node      string    `json:"node"`
	Started   time.Time `json:"started"`
}

// Uptime formats how long the service has been running, like "3h 12m" or "45s".
func (i Info) Uptime(now time.Time) string {
	d := now.Sub(i.Started)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// Barcode is drawn from the version, so each version looks different at a glance.
type Barcode struct {
	Bars  []Bar
	Width int
}

// Bar is one bar of the barcode, in barcode units.
type Bar struct{ X, W int }

// Barcode derives a barcode from a hash of the version.
func (i Info) Barcode() Barcode {
	sum := sha256.Sum256([]byte(i.Version))
	var code Barcode
	for _, b := range sum[:22] {
		for _, n := range []byte{b >> 4, b & 0x0f} {
			w := 1 + int(n%3)
			code.Bars = append(code.Bars, Bar{X: code.Width, W: w})
			code.Width += w + 1 + int(n>>2)%2
		}
	}
	return code
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func newInfo(now time.Time) Info {
	host, _ := os.Hostname()
	return Info{
		Service:   "hello",
		Version:   version,
		Env:       env("APP_ENV", "local"),
		Pod:       env("POD_NAME", host),
		Namespace: os.Getenv("POD_NAMESPACE"),
		Node:      os.Getenv("NODE_NAME"),
		Started:   now,
	}
}

func routes(info Info) http.Handler {
	mux := http.NewServeMux()
	fonts, _ := fs.Sub(web, "web")
	mux.Handle("GET /fonts/", http.FileServerFS(fonts))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(info)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		data := struct {
			Info
			Now time.Time
		}{info, time.Now()}
		if err := page.Execute(w, data); err != nil {
			slog.Error("render page", "err", err)
		}
	})
	return mux
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	info := newInfo(time.Now())
	srv := &http.Server{
		Addr:              ":" + env("PORT", "8080"),
		Handler:           routes(info),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Kubernetes sends SIGTERM before stopping a pod: finish in-flight requests, then exit.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	slog.Info("listening", "addr", srv.Addr, "version", info.Version, "env", info.Env)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
