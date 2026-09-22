// Command llm-gateway is the composition root for the LLM Gateway service.
//
// Responsibilities:
//   - wire the application dependency graph once, at boot;
//   - serve the HTTP runtime contract (health probes + graceful shutdown).
//
// No business logic lives here.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/infrastructure/provider"
)

const (
	defaultPort       = "8080"
	readHeaderTimeout = 5 * time.Second
	shutdownGrace     = 10 * time.Second
)

func main() {
	svc, err := buildService()
	if err != nil {
		log.Fatalf("llm-gateway: startup failed: %v", err)
	}
	log.Printf("llm-gateway: dependency graph wired")

	// Opt-in smoke check. Makes a real provider call, so off by default.
	if os.Getenv("LLM_GATEWAY_STARTUP_SMOKE") == "1" {
		if err := runStartupSmoke(svc); err != nil {
			log.Fatalf("llm-gateway: startup smoke check failed: %v", err)
		}
		log.Printf("llm-gateway: startup smoke check passed")
	}

	gateway := &applicationGateway{service: svc}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/startupz", healthHandler)
	mux.HandleFunc("/live", healthHandler)
	mux.HandleFunc("/v1/complete", gateway.complete)

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	// ListenAndServe blocks, so run it in a goroutine and surface its error
	// on a channel for the select below.
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("llm-gateway: listening on %s", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		// ErrServerClosed means Shutdown was called cleanly — not a failure.
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("llm-gateway: HTTP server failed: %v", err)
		}

	case sig := <-quit:
		log.Printf("llm-gateway: received %s, beginning graceful shutdown", sig)
		ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Fatalf("llm-gateway: HTTP shutdown failed: %v", err)
		}
		log.Printf("llm-gateway: shutdown complete")
	}
}

// buildService wires the full dependency graph bottom-up.
//
// Any failure here is a composition-root bug and must abort startup — never
// surface as a runtime 500 on the first request.
func buildService() (*application.Service, error) {
	// Fake providers stand in for real adapters. The Service and Router
	// cannot tell the difference — that is the point of the Provider port.
	primary := provider.NewFakeProvider(
		"fake-primary",
		"test-model-a",
		`{"shipment_id":"SH-123","risk":"high_risk","confidence":0.91,"reasons":["port congestion"]}`,
	)
	secondary := provider.NewFakeProvider(
		"fake-secondary",
		"test-model-b",
		`{"shipment_id":"SH-123","risk":"medium_risk","confidence":0.72,"reasons":["weather"]}`,
	)

	// Bounded retry per provider, then deterministic fallback to the next.
	// nil Sleeper uses DefaultSleeper, which is cancellation-aware.
	router, err := application.NewProviderRouter(
		[]application.Provider{primary, secondary},
		application.RetryPolicy{
			MaxAttemptsPerProvider: 2,
			AttemptTimeout:         500 * time.Millisecond,
		},
		application.FallbackPolicy{
			Enabled:      true,
			MaxProviders: 2,
		},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("build router: %w", err)
	}

	registry, err := application.NewTaskRegistry(
		application.NewShipmentRiskTaskDefinition(),
	)
	if err != nil {
		return nil, fmt.Errorf("build registry: %w", err)
	}

	// nil usage publisher = telemetry disabled for this deployment.
	svc, err := application.NewService(
		router,
		registry,
		domain.ExecutionPolicy{
			CurrentContractVersion: application.CurrentContractVersion,
			MaxEvidenceReferences:  8,
		},
		nil,
		systemClock{},
		cryptoHexEventID,
	)
	if err != nil {
		return nil, fmt.Errorf("build service: %w", err)
	}
	return svc, nil
}

// runStartupSmoke exercises the full pipeline once so a broken wiring fails
// at boot instead of on the first real request.
func runStartupSmoke(svc *application.Service) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, metadata, err := svc.Complete(ctx, application.CompleteCommand{
		ContractVersion:     application.CurrentContractVersion,
		TenantID:            "tenant-smoke",
		RequestID:           "request-smoke",
		IdempotencyKey:      "operation-smoke",
		TaskType:            domain.TaskShipmentDelayRisk,
		TaskSchemaVersion:   "shipment_delay_risk.v1",
		Prompt:              "Startup smoke check: classify shipment delay risk.",
		PromptVersion:       "shipment-delay-risk.prompt.v1",
		OutputSchemaVersion: "shipment_delay_risk.result.v1",
		MaxTokens:           300,
		Subjects: []domain.EntityReference{
			{Type: domain.EntityShipment, ID: "SH-123"},
		},
	})
	if err != nil {
		return err
	}
	if metadata.Status != domain.ExecutionSucceeded {
		return fmt.Errorf("smoke: status = %q, want %q", metadata.Status, domain.ExecutionSucceeded)
	}
	return nil
}

// applicationGateway holds the wired service so handlers reach it through
// the receiver rather than a package-level global.
type applicationGateway struct {
	service *application.Service
}

// complete is a stub for the transport adapter that will decode an inbound
// request into CompleteCommand, invoke the service, and map domain.Kind to
// HTTP status codes. Replace the body when the transport module lands.
func (g *applicationGateway) complete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotImplemented)
	_, _ = w.Write([]byte(`{"error":"completion endpoint not yet implemented"}`))
}

// healthHandler answers all three Kubernetes probes. The process is Ready if
// it is listening because the dependency graph was validated at boot.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// systemClock is the production Clock. Local to main because it is a
// composition concern, not an application one.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// cryptoHexEventID returns a 128-bit random hex string. crypto/rand avoids
// cross-replica coordination.
func cryptoHexEventID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
