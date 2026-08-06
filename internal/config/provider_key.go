package config

import (
	"fmt"
	"os"
	"strings"
)

// KeyEnvName returns the API-key environment variable for provider.
func KeyEnvName(provider string) string {
	switch provider {
	case ProviderOpenAICompat:
		return "OPENAI_API_KEY"
	case ProviderAnthropic:
		return "ANTHROPIC_API_KEY"
	default:
		return ""
	}
}

func declaredAPIKey(llm LLM) (string, error) {
	if llm.APIKeyFile == "" {
		return llm.APIKey, nil
	}
	raw, err := os.ReadFile(llm.APIKeyFile)
	if err != nil {
		return "", fmt.Errorf("read api_key_file: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}
