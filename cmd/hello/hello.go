package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/internal/observability"
)

func main() {
	// Create a context that cancels when SIGINT or SIGTERM is received.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Bootstrap observability with the service identity.
	obs := observability.New("hello")

	obs.Logger.Info("service starting")

	// Simulate a running server (in reality, an HTTP/gRPC server would be here).
	done := make(chan struct{})
	go func() {
		obs.Logger.Info("hello service is ready")

		// Block until shutdown signal.
		<-ctx.Done()

		obs.Logger.Info("shutdown signal received, draining...")
		time.Sleep(2 * time.Second) // mimic graceful drain of in-flight requests

		close(done)
	}()

	// Wait for clean shutdown or a second signal (force quit).
	select {
	case <-done:
		obs.Logger.Info("service stopped cleanly")
	case <-ctx.Done():
		obs.Logger.Info("service forced to stop")
	}

	obs.Logger.Info("goodbye")
}
