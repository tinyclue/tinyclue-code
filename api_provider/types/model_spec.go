package types

import "strings"

// modelContextWindows maps model name prefixes to their context windows
// (max total tokens = input + output). Ordered from most specific to least.
// Unknown models return 0.
var modelContextWindows = []struct {
	prefix string
	window int
}{
	{"claude-", 200000},

	// ── OpenAI ──
	{"o3", 200000},
	{"o1-pro", 200000},
	{"o1", 200000},
	{"gpt-4o-mini", 128000},
	{"gpt-4o", 128000},
	{"gpt-4-turbo", 128000},
	{"gpt-4-32k", 32768},
	{"gpt-4", 8192},
	{"gpt-3.5-turbo-16k", 16385},
	{"gpt-3.5-turbo", 16385},

	// ── DeepSeek ──
	{"deepseek-v4", 1000000},
	{"deepseek-r1", 128000},
	{"deepseek-v3", 64000},
	{"deepseek-v2", 64000},
	{"deepseek-chat", 64000},

	// ── Google Gemini ──
	{"gemini-2.5-pro", 1000000},
	{"gemini-2.0-flash", 1000000},
	{"gemini-1.5-pro", 2000000},
	{"gemini-1.5-flash", 1000000},

	// ── xAI Grok ──
	{"grok-3", 131072},
	{"grok-2", 131072},
	{"grok-", 131072},

	// ── Amazon Bedrock / Meta Llama ──
	{"llama-3.1-405b", 131072},
	{"llama-3.1-70b", 131072},
	{"llama-3.1-8b", 131072},
	{"llama-3-70b", 8192},
	{"llama-3-8b", 8192},
	{"llama-2-70b", 4096},
	{"llama-2-13b", 4096},
	{"llama-2-7b", 4096},

	// ── Mistral ──
	{"mistral-large", 128000},
	{"mistral-small", 128000},
	{"mistral-medium", 32000},
	{"mistral-", 32000},

	// ── Cohere ──
	{"command-r-plus", 128000},
	{"command-r", 128000},
	{"command-", 4096},

	// ── General fallbacks (low confidence) ──
	// If none of the above match, check for common keywords in the model name.
}

// ContextWindow returns the known context window (max total tokens) for a
// given model name. Returns 0 if the model is unknown / not recognized.
//
// modelName is matched case-insensitively against known prefixes.
// More specific prefixes are checked first.
func ContextWindow(modelName string) int {
	name := strings.ToLower(modelName)
	for _, entry := range modelContextWindows {
		if strings.HasPrefix(name, entry.prefix) {
			return entry.window
		}
	}
	return 0
}

// modelMaxTokens maps model name prefixes to their maximum output token limits.
// Unknown models return 0.
var modelMaxTokens = []struct {
	prefix    string
	maxTokens int
}{
	// ── Anthropic Claude ──
	{"claude-4", 8192},
	{"claude-3", 8192},

	// ── OpenAI ──
	{"o3", 100000},
	{"o1-pro", 100000},
	{"o1", 100000},
	{"gpt-4o-mini", 16384},
	{"gpt-4o", 16384},
	{"gpt-4-turbo", 4096},
	{"gpt-4-32k", 4096},
	{"gpt-4", 4096},
	{"gpt-3.5-turbo-16k", 4096},
	{"gpt-3.5-turbo", 4096},

	// ── DeepSeek ──
	{"deepseek-v4", 8192},
	{"deepseek-r1", 8192},
	{"deepseek-v3", 8192},
	{"deepseek-v2", 8192},
	{"deepseek-chat", 8192},

	// ── Google Gemini ──
	{"gemini-2.5-pro", 8192},
	{"gemini-2.0-flash", 8192},
	{"gemini-1.5-pro", 8192},
	{"gemini-1.5-flash", 8192},

	// ── xAI Grok ──
	{"grok-3", 131072},
	{"grok-2", 131072},
	{"grok-", 131072},

	// ── Meta Llama ──
	{"llama-3.1", 8192},
	{"llama-3", 8192},
	{"llama-2", 4096},

	// ── Mistral ──
	{"mistral-large", 8192},
	{"mistral-small", 8192},
	{"mistral-medium", 4096},
	{"mistral-", 4096},

	// ── Cohere ──
	{"command-r-plus", 4096},
	{"command-r", 4096},
	{"command-", 4096},
}

// MaxTokens returns the known maximum output token limit for a given model name.
// Returns 0 if the model is unknown / not recognized.
func MaxTokens(modelName string) int {
	name := strings.ToLower(modelName)
	for _, entry := range modelMaxTokens {
		if strings.HasPrefix(name, entry.prefix) {
			return entry.maxTokens
		}
	}
	return 0
}
