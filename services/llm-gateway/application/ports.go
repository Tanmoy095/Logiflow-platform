// services/llm-gateway/application/ports.go
package application

import (
	"context"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

/*

Provider is the application-owned port for external AI completion.

The provider returns raw, untrusted output.
The application layer is responsible for parsing,
schema validation, domain validation, and creation
of the trusted domain.CompletionResult.


Why does your Provider interface return string instead of CompletionResult?”


Because provider output is untrusted external data. I deliberately keep the Provider port raw-output oriented
so the application owns parsing and schema validation, and the domain owns business invariants.
CompletionResult is created only after those checks pass, making it a trust-bearing type rather than a provider transport type.

*/

type Provider interface {
	Complete(
		ctx context.Context,
		req domain.Request,
	) (string, error)
}
