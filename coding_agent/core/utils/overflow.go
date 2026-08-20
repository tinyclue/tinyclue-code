package utils

import (
	"regexp"

	"github.com/tinyclue/tinyclue-code/api_provider/types"
)

// overflowPatterns matches error messages indicating context window overflow
// from various LLM providers.
var overflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)prompt is too long`),                                                                         // Anthropic
	regexp.MustCompile(`(?i)request_too_large`),                                                                          // Anthropic (413)
	regexp.MustCompile(`(?i)input is too long for requested model`),                                                      // Bedrock
	regexp.MustCompile(`(?i)exceeds the context window`),                                                                 // OpenAI
	regexp.MustCompile(`(?i)exceeds (?:the )?(?:model'?s )?maximum context length(?: of [\d,]+ tokens?|\s*\([\d,]+\))?`), // OpenAI-compatible / LiteLLM
	regexp.MustCompile(`(?i)input token count.*exceeds the maximum`),                                                     // Google Gemini
	regexp.MustCompile(`(?i)maximum prompt length is \d+`),                                                               // xAI (Grok)
	regexp.MustCompile(`(?i)reduce the length of the messages`),                                                          // Groq
	regexp.MustCompile(`(?i)maximum context length is \d+ tokens`),                                                       // OpenRouter
	regexp.MustCompile(`(?i)exceeds (?:the )?maximum allowed input length of [\d,]+ tokens?`),                            // OpenRouter/Poolside
	regexp.MustCompile(`(?i)input \(\d+ tokens\) is longer than the model'?s context length \(\d+ tokens\)`),             // Together AI
	regexp.MustCompile(`(?i)exceeds the limit of \d+`),                                                                   // GitHub Copilot
	regexp.MustCompile(`(?i)exceeds the available context size`),                                                         // llama.cpp
	regexp.MustCompile(`(?i)greater than the context length`),                                                            // LM Studio
	regexp.MustCompile(`(?i)context window exceeds limit`),                                                               // MiniMax
	regexp.MustCompile(`(?i)exceeded model token limit`),                                                                 // Kimi For Coding
	regexp.MustCompile(`(?i)too large for model with \d+ maximum context length`),                                        // Mistral
	regexp.MustCompile(`(?i)model_context_window_exceeded`),                                                              // z.ai
	regexp.MustCompile(`(?i)prompt too long; exceeded (?:max )?context length`),                                          // Ollama
	regexp.MustCompile(`(?i)context[_ ]length[_ ]exceeded`),                                                              // Generic fallback
	regexp.MustCompile(`(?i)too many tokens`),                                                                            // Generic fallback
	regexp.MustCompile(`(?i)token limit exceeded`),                                                                       // Generic fallback
	regexp.MustCompile(`(?i)^4(?:00|13)\s*(?:status code)?\s*\(no body\)`),                                               // Cerebras: 400/413 with no body
}

// nonOverflowPatterns matches error messages that should NOT be classified
// as overflow even if they also match an overflow pattern (e.g., rate limits
// that happen to mention "tokens").
var nonOverflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(?:Throttling error|Service unavailable):`), // Bedrock
	regexp.MustCompile(`(?i)rate limit`),                                 // Generic rate limiting
	regexp.MustCompile(`(?i)too many requests`),                          // Generic HTTP 429
}

// IsContextOverflow checks if a model context represents a context overflow error.
//
// It handles three cases:
//  1. Error-based overflow: Most providers return stop reason "error" with a
//     specific error message (errMsg) matching known overflow patterns.
//  2. Silent overflow (z.ai style): The provider accepts overflow silently but
//     the usage exceeds the context window.
//  3. Length-stop overflow (Xiaomi MiMo style): The provider truncates input to
//     fill the context window exactly, leaving no room for output (stop reason
//     "length", output=0, input fills the context window).
//
// Pass errMsg to enable error-based overflow detection (usually from ChatResponse.Error.ErrorMessage).
// Pass contextWindow to enable detection of silent and length-stop overflow.
// When contextWindow <= 0, only error-based overflow is checked.
//
// Note: contextWindow is the model's total context window (input + output tokens).
// Case 2 checks if total input alone > contextWindow — a definite overflow.
// Case 3 catches the edge case where input fills ≥99% of the window and
// the model has no room for output (stopReason="length", output=0).
func IsContextOverflow(finalResp *types.ChatResponse, contextWindow int) bool {
	// Case 1: Error-based overflow detection
	errMsg := finalResp.Error.RawMessage
	content := finalResp.Content
	if content.StopReason.FinishReason == types.FinishReasonError && errMsg != "" {
		// Skip messages matching known non-overflow patterns (e.g., throttling / rate-limit)
		isNonOverflow := false
		for _, p := range nonOverflowPatterns {
			if p.MatchString(errMsg) {
				isNonOverflow = true
				break
			}
		}
		if !isNonOverflow {
			for _, p := range overflowPatterns {
				if p.MatchString(errMsg) {
					return true
				}
			}
		}
	}

	// Only proceed with usage-based checks if contextWindow is provided
	if contextWindow <= 0 {
		return false
	}

	// Calculate total input tokens sent to the model.
	// Usage.Input = non-cached input tokens, cache is separate.
	// TotalTokens = input + output + cacheRead + cacheWrite (total processed).
	// So TotalTokens - Output = total input to the model (includes cache).
	totalInput := content.Usage.TotalTokens - content.Usage.Output

	// Case 2: Silent overflow — successful response but usage exceeds context.
	// Context window limits total (input + output) tokens. If input alone already
	// exceeds the window, it's a definite overflow.
	if content.StopReason.FinishReason == types.FinishReasonStop {
		if totalInput > contextWindow {
			return true
		}
	}

	// Case 3: Length-stop overflow — server truncates oversized input to fit
	// the context window, leaving no room for output. Returns stop reason "length"
	// with output=0 and input filling ≥99% of the context window.
	if content.StopReason.FinishReason == types.FinishReasonLength && content.Usage.Output == 0 {
		if float64(totalInput) >= float64(contextWindow)*0.99 {
			return true
		}
	}

	return false
}
