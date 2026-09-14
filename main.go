// Command hello is the BACK lab's sample service: a status page about itself.
package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// version is set at build time: go build -ldflags "-X main.version=sha-1a2b3c4".
var version = "local"

//go:embed web
var web embed.FS

var page = template.Must(template.ParseFS(web, "web/index.html"))

// Info is what the page shows, also served as JSON at /api/info.
type Info struct {
	Service   string        `json:"service"`
	Version   string        `json:"version"`
	Env       string        `json:"env"`
	Pod       string        `json:"pod"`
	Namespace string        `json:"namespace"`
	Node      string        `json:"node"`
	Spec      Spec          `json:"spec"`
	Started   time.Time     `json:"started"`
	Bucket    *BucketStatus `json:"bucket,omitempty"`
}

// Spec is what this service asked the platform for, as the platform resolved
// it. The page shows it, so a change to values.yaml is visible in the browser
// without reading any YAML.
type Spec struct {
	Size        string `json:"size"`
	Replicas    int    `json:"replicas"`
	CPU         string `json:"cpu"`
	Memory      string `json:"memory"`
	MemoryLimit string `json:"memoryLimit,omitempty"`
	Public      bool   `json:"public"`
}

// BucketStatus says whether the service reaches its bucket.
type BucketStatus struct {
	Name      string `json:"name"`
	Versioned bool   `json:"versioned"`
	Reachable bool   `json:"reachable"`
	Error     string `json:"error,omitempty"`
}

type bucketProbe struct {
	name      string
	versioned bool
	check     func(context.Context) error
}

func newBucketProbe(ctx context.Context, name string) *bucketProbe {
	if name == "" {
		return nil
	}
	versioned := os.Getenv("BUCKET_VERSIONING") == "true"
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return &bucketProbe{name: name, versioned: versioned, check: func(context.Context) error { return err }}
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		// LocalStack serves buckets at <endpoint>/<bucket>.
		o.UsePathStyle = cfg.BaseEndpoint != nil
		o.RetryMaxAttempts = 1
	})
	return &bucketProbe{name: name, versioned: versioned, check: func(ctx context.Context) error {
		_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(name)})
		return err
	}}
}

func (p *bucketProbe) status(ctx context.Context) *BucketStatus {
	if p == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	s := &BucketStatus{Name: p.name, Versioned: p.versioned, Reachable: true}
	if err := p.check(ctx); err != nil {
		s.Reachable, s.Error = false, err.Error()
	}
	return s
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

func envInt(key string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return n
	}
	return fallback
}

// newSpec reads what the platform's chart resolved for this service. Outside a
// cluster nothing is set, and the page simply has nothing to show.
func newSpec() Spec {
	return Spec{
		Size:        os.Getenv("APP_SIZE"),
		Replicas:    envInt("APP_REPLICAS", 0),
		CPU:         os.Getenv("APP_CPU"),
		Memory:      os.Getenv("APP_MEMORY"),
		MemoryLimit: os.Getenv("APP_MEMORY_LIMIT"),
		Public:      os.Getenv("APP_PUBLIC") == "true",
	}
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
		Spec:      newSpec(),
		Started:   now,
	}
}

func routes(info Info, bucket *bucketProbe) http.Handler {
	mux := http.NewServeMux()
	fonts, _ := fs.Sub(web, "web")
	mux.Handle("GET /fonts/", http.FileServerFS(fonts))
	mux.HandleFunc("GET /favicon.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, web, "web/favicon.png")
	})
	mux.HandleFunc("GET /logo.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, web, "web/logo.png")
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	current := func(r *http.Request) Info {
		now := info
		now.Bucket = bucket.status(r.Context())
		return now
	}
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(current(r))
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := page.Execute(w, current(r)); err != nil {
			slog.Error("render page", "err", err)
		}
	})
	return mux
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	info := newInfo(time.Now())
	bucket := newBucketProbe(context.Background(), os.Getenv("BUCKET_NAME"))
	srv := &http.Server{
		Addr:              ":" + env("PORT", "8080"),
		Handler:           routes(info, bucket),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Kubernetes sends SIGTERM before killing a pod: drain in-flight requests.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	slog.Info("listening", "addr", srv.Addr, "version", info.Version, "env", info.Env, "bucket", os.Getenv("BUCKET_NAME"))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
