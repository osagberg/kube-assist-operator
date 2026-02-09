/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// console-backend is a standalone HTTP server that aggregates multiple
// Kubernetes clusters behind a single REST API. It serves as the backend
// for ConsoleDataSource, enabling cross-cluster health monitoring.
//
// Usage:
//
//	go run ./cmd/console-backend/ \
//	  --kubeconfigs=cluster-a=/tmp/kind-a.yaml,cluster-b=/tmp/kind-b.yaml \
//	  --addr=:8085
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/osagberg/kube-assist-operator/internal/console"
)

func main() {
	var addr string
	var kubeconfigsFlag string
	var authToken string
	var tlsCert, tlsKey string
	flag.StringVar(&addr, "addr", ":8085", "HTTP listen address.")
	flag.StringVar(&kubeconfigsFlag, "kubeconfigs", "",
		"Comma-separated cluster-id=kubeconfig-path pairs (e.g. cluster-a=/tmp/kind-a.yaml,cluster-b=/tmp/kind-b.yaml).")
	flag.StringVar(&authToken, "auth-token", "", "Bearer token for API authentication (env: CONSOLE_AUTH_TOKEN).")
	flag.StringVar(&tlsCert, "tls-cert", "", "Path to TLS certificate file.")
	flag.StringVar(&tlsKey, "tls-key", "", "Path to TLS key file.")
	var allowInsecure bool
	flag.BoolVar(&allowInsecure, "allow-insecure", false, "Allow running without auth/TLS (NOT for production).")
	flag.Parse()

	if authToken == "" {
		authToken = os.Getenv("CONSOLE_AUTH_TOKEN")
	}

	if authToken == "" && !allowInsecure {
		slog.Error("Console backend requires authentication. Set --auth-token, CONSOLE_AUTH_TOKEN, or use --allow-insecure for development.")
		os.Exit(1)
	}
	if (tlsCert == "" || tlsKey == "") && !allowInsecure {
		slog.Error("Console backend requires TLS. Set --tls-cert and --tls-key, or use --allow-insecure for development.")
		os.Exit(1)
	}
	if allowInsecure {
		slog.Warn("WARNING: Console backend running without auth/TLS — NOT FOR PRODUCTION USE")
	}

	if kubeconfigsFlag == "" {
		slog.Error("--kubeconfigs is required")
		os.Exit(1)
	}

	configs, err := parseKubeconfigs(kubeconfigsFlag)
	if err != nil {
		slog.Error("Failed to parse --kubeconfigs", "error", err)
		os.Exit(1)
	}

	agg, err := console.NewAggregator(configs)
	if err != nil {
		slog.Error("Failed to create aggregator", "error", err)
		os.Exit(1)
	}

	h := console.NewHandler(agg)
	mux := http.NewServeMux()
	console.RegisterRoutes(mux, h)

	var handler http.Handler = mux
	if authToken != "" {
		handler = console.AuthMiddleware(authToken, handler)
		slog.Info("Console backend authentication enabled")
		if tlsCert == "" || tlsKey == "" {
			slog.Warn("Auth token configured without TLS — tokens will be sent in plaintext")
		}
	} else {
		slog.Warn("Console backend authentication not configured. Set --auth-token or CONSOLE_AUTH_TOKEN to secure the API.")
	}
	handler = console.SecurityHeaders(handler)

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("Console backend starting", "addr", addr, "clusters", agg.ClusterIDs())
		var err error
		if tlsCert != "" && tlsKey != "" {
			slog.Info("Console backend TLS enabled", "cert", tlsCert)
			err = srv.ListenAndServeTLS(tlsCert, tlsKey)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("Shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// parseKubeconfigs parses "id1=path1,id2=path2" into ClusterConfig slice.
func parseKubeconfigs(s string) ([]console.ClusterConfig, error) {
	configs := make([]console.ClusterConfig, 0, strings.Count(s, ",")+1)
	for pair := range strings.SplitSeq(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid kubeconfig pair: %q (expected id=path)", pair)
		}
		configs = append(configs, console.ClusterConfig{
			ID:             parts[0],
			KubeconfigPath: parts[1],
		})
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("no kubeconfig pairs provided")
	}
	return configs, nil
}
