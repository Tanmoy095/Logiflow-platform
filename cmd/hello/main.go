package main

import (
	"context"
	"encoding/json"
	"log"
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

	serviceName := os.Getenv("SERVICE_NAME")
	if serviceName == "" {
		serviceName = "hello"
	}

	// 2. Log startup info (useful for debugging: PID, hostname)
	pid := os.Getpid()
	hostname, _ := os.Hostname()
	log.Printf("%s starting: PID=%d hostname=%s port=%s", serviceName, pid, hostname, port)

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
		// Signal received, begin graceful shutdown
		log.Printf("%s received shutdown signal, draining...", serviceName)
		obs.Logger.Info("shutdown initiated, setting readiness to false")

		// Give Kubernetes time to stop sending traffic (optional)
		// If you have a preStop hook, you can add a small delay here.
		// time.Sleep(2 * time.Second)

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("forced shutdown: %v", err)
		}
		log.Printf("%s stopped cleanly", serviceName)
	}
	obs.Logger.Info("service stopped")
	obs.Logger.Info("service stopped cleanly")
}
