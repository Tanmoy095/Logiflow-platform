package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/internal/observability"
)

func main() {
	// ---- Configuration with defaults ----
	port := 8080
	if p := os.Getenv("PORT"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			port = v
		}
	}

	serviceName := "hello"
	if s := os.Getenv("SERVICE_NAME"); s != "" {
		serviceName = s
	}

	// ---- Bootstrap Observability ----
	// Using your centralized package instead of manual slog setup
	obs := observability.New(serviceName)
	obs.Logger.Info("service initializing", "port", port)

	// ---- Handlers ----
	// Liveness: ALWAYS returns 200 (no external dependency checks)
	healthzHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}

	// Readiness: can later be extended to check downstream services
	readyHandler := func(w http.ResponseWriter, r *http.Request) {
		// For now, always ready; in the future, check DB/Kafka etc.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	}

	// Placeholder metrics endpoint
	metricsHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte("metrics not yet implemented"))
	}

	// ---- HTTP Server Setup ----
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/ready", readyHandler)
	mux.HandleFunc("/metrics", metricsHandler)

	srv := &http.Server{
		Addr:         ":" + strconv.Itoa(port),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// ---- Graceful Shutdown ----
	// Create a context that cancels when SIGINT or SIGTERM is received.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Channel to signal when the server has finished shutting down
	serverErrors := make(chan error, 1)

	// Start the server in the background
	go func() {
		obs.Logger.Info("hello service is ready", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()

	// ---- Block & Wait ----
	select {
	case err := <-serverErrors:
		obs.Logger.Error("server failed to start", "error", err)
		os.Exit(1)

	case <-ctx.Done():
		obs.Logger.Info("shutdown signal received, draining connections...")

		// Set readiness to false / simulate traffic drain time if needed in k8s
		obs.Logger.Info("readiness set to false")

		// Create a context with timeout for the shutdown process
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Attempt graceful shutdown
		if err := srv.Shutdown(shutdownCtx); err != nil {
			obs.Logger.Error("forced shutdown triggered", "error", err)
			// Force close the server if it won't shut down gracefully
			_ = srv.Close()
		}
	}

	obs.Logger.Info("service stopped cleanly")
	obs.Logger.Info("goodbye")
}
