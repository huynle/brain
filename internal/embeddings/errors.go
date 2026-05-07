package embeddings

import "fmt"

// ProviderError describes an embedding provider failure with optional HTTP details.
type ProviderError struct {
	Provider   string
	StatusCode int
	Body       string
	Err        error
}

func (e *ProviderError) Error() string {
	if e == nil {
		return "embedding provider error"
	}
	if e.StatusCode != 0 {
		if e.Body != "" {
			return fmt.Sprintf("%s embedding provider error (%d): %s", e.Provider, e.StatusCode, e.Body)
		}
		return fmt.Sprintf("%s embedding provider error (%d)", e.Provider, e.StatusCode)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s embedding provider error: %v", e.Provider, e.Err)
	}
	return fmt.Sprintf("%s embedding provider error", e.Provider)
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
