package application

/*
                  ┌──────────────────────┐
                  │ Request Comes In     │
                  └──────────┬───────────┘
                             │
                             ▼
                ┌──────────────────────────┐
                │ Call BuildCacheKey(...)  │
                └────────────┬─────────────┘
                             │
                             ▼
              Cache Key: "v1:a3f89b1c2e..."
                             │
                             ▼
                ┌──────────────────────────┐
                │ Check Redis / Cache      │
                └────────────┬─────────────┘
                             │
             ┌───────────────┴───────────────┐
             │                               │
       Cache HIT                       Cache MISS
 (Key exists in Redis)            (Key not in Redis)
             │                               │
             ▼                               ▼
    Return saved response            Call LLM API (e.g. OpenAI)
    (Instant & Free)                         │
                                             ▼
                                     Save response to Redis
                                     using "v1:a3f89b1c2e..."
*/

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

// BuildCacheKey creates a tenant- and version-scoped fingerprint for an
// equivalent completion request.
//
// The raw prompt is included only as an input to SHA-256; it is not returned,
// logged, or stored as the cache key itself. A later production implementation
// can replace the digest primitive with a keyed hash if stronger protection
// against offline guessing is required.
//
// IMPORTANT:
// This function is intentionally based on request semantics. Never add
// provider credentials or arbitrary transport headers to the key.
func BuildCacheKey(
	tenantID string,
	taskType domain.TaskType,
	taskSchemaVersion string,
	promptVersion string,
	outputSchemaVersion string,
	prompt string,
	subjectIDs []string,
	evidenceIDs []string,
) (CacheKey, error) {
	if strings.TrimSpace(tenantID) == "" {
		return "", fmt.Errorf("tenant_id must not be empty")
	}
	if strings.TrimSpace(string(taskType)) == "" {
		return "", fmt.Errorf("task_type must not be empty")
	}
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("prompt must not be empty")
	}

	h := sha256.New()
	writePart := func(value string) {
		fmt.Fprintf(h, "%d:", len(value))
		h.Write([]byte(value))
	}

	// Version fields are part of the identity so a prompt/schema change does
	// not silently reuse an incompatible old result.
	for _, value := range []string{
		tenantID,
		string(taskType),
		taskSchemaVersion,
		promptVersion,
		outputSchemaVersion,
		prompt,
	} {
		writePart(value)
	}

	for _, id := range subjectIDs {
		writePart("subject:" + id)
	}
	for _, id := range evidenceIDs {
		writePart("evidence:" + id)
	}

	sum := h.Sum(nil)
	return CacheKey("v1:" + hex.EncodeToString(sum)), nil
}
