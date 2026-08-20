package models

import (
	"strings"

	"github.com/tinyclue/tinyclue-code/models_cache/core/models/providers"
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

// FinalizeModels applies global post-processing to the complete model list:
//   - DeepSeek compat propagation
//   - thinkingLevelMap for all models
//   - Deduplication by (provider, id)
func FinalizeModels(all []types.Model) []types.Model {
	// ── DeepSeek compat propagation ──────────────────────────────────────────
	deepseekCompat := map[string]any{
		"requiresReasoningContentOnAssistantMessages": true,
		"thinkingFormat": "deepseek",
	}
	for i, m := range all {
		if m.API == "openai-completions" && strings.Contains(m.ID, "deepseek-v4") {
			if m.Provider == types.ProviderNameOpenRouter {
				// OpenRouter gets a lightweight compat merge.
				providers.MergeCompat(&all[i], map[string]any{
					"requiresReasoningContentOnAssistantMessages": true,
				})
			} else {
				providers.MergeCompat(&all[i], deepseekCompat)
			}
		}
	}

	// ── thinkingLevelMap for all models ──────────────────────────────────────
	applyThinkingLevelMap(all)

	// ── Deduplicate by (provider, id), keep first occurrence ─────────────────
	seen := map[string]bool{}
	result := make([]types.Model, 0, len(all))
	for _, m := range all {
		key := m.Provider + "\x00" + m.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, m)
	}
	return result
}

// applyThinkingLevelMap injects thinkingLevelMap and compat into models
// matching specific provider/model/api patterns.
func applyThinkingLevelMap(all []types.Model) {
	for i := range all {
		m := &all[i]

		// ── OpenAI / Azure OpenAI gpt-5* ──
		if (m.API == "openai-responses" || m.API == "azure-openai-responses") &&
			strings.HasPrefix(m.ID, "gpt-5") {
			mergeTLM(m, map[string]*string{"off": nil})
		}
		if m.Provider == types.ProviderNameGitHubCopilot && strings.HasPrefix(m.ID, "gpt-5") {
			low := strPtr("low")
			mergeTLM(m, map[string]*string{"minimal": low})
		}
		if m.API == "openai-responses" && m.Provider == types.ProviderNameOpenAI &&
			noneReasoningModels[m.ID] {
			none := strPtr("none")
			mergeTLM(m, map[string]*string{"off": none})
		}
		if supportsOpenAiXhigh(m.ID) {
			xhigh := strPtr("xhigh")
			mergeTLM(m, map[string]*string{"xhigh": xhigh})
		}
		if m.Provider == types.ProviderNameOpenAI && m.ID == "gpt-5.5" {
			mergeTLM(m, map[string]*string{"minimal": nil})
		}
		if strings.HasSuffix(m.ID, "gpt-5.5-pro") {
			mergeTLM(m, map[string]*string{"off": nil, "minimal": nil, "low": nil})
		}

		// ── Anthropic / Bedrock ──
		if strings.Contains(m.ID, "opus-4-6") || strings.Contains(m.ID, "opus-4.6") {
			max := strPtr("max")
			mergeTLM(m, map[string]*string{"xhigh": max})
		}
		if strings.Contains(m.ID, "opus-4-7") || strings.Contains(m.ID, "opus-4.7") ||
			strings.Contains(m.ID, "opus-4-8") || strings.Contains(m.ID, "opus-4.8") {
			xhigh := strPtr("xhigh")
			mergeTLM(m, map[string]*string{"xhigh": xhigh})
		}
		if (m.API == "anthropic-messages" || m.API == "bedrock-converse-stream") &&
			strings.Contains(m.ID, "fable-5") {
			xhigh := strPtr("xhigh")
			mergeTLM(m, map[string]*string{"off": nil, "xhigh": xhigh})
		}

		// ── Anthropic adaptive thinking / temperature compat ──
		if m.API == "anthropic-messages" {
			if isAnthropicAdaptiveThinking(m.ID) {
				providers.MergeCompat(m, map[string]any{"forceAdaptiveThinking": true})
			}
			if isAnthropicTemperatureUnsupported(m.ID) {
				providers.MergeCompat(m, map[string]any{"supportsTemperature": false})
			}
		}

		// ── DeepSeek V4 thinking level map ──
		if m.API == "openai-completions" && strings.Contains(m.ID, "deepseek-v4") {
			if m.Provider == types.ProviderNameOpenRouter {
				mergeTLM(m, map[string]*string{
					"minimal": nil, "low": nil, "medium": nil,
					"high": strPtr("high"), "xhigh": strPtr("xhigh"),
				})
			} else {
				mergeTLM(m, map[string]*string{
					"minimal": nil, "low": nil, "medium": nil,
					"high": strPtr("high"), "xhigh": strPtr("max"),
				})
			}
		}

		// ── Google thinking ──
		if m.API == "google-generative-ai" || m.API == "google-vertex" {
			if isGemini3Pro(m.ID) {
				mergeTLM(m, map[string]*string{
					"off": nil, "minimal": nil, "low": strPtr("LOW"),
					"medium": nil, "high": strPtr("HIGH"),
				})
			}
			if isGemini3Flash(m.ID) {
				mergeTLM(m, map[string]*string{"off": nil})
			}
			if isGemma4(m.ID) {
				mergeTLM(m, map[string]*string{
					"off": nil, "minimal": strPtr("MINIMAL"),
					"low": nil, "medium": nil, "high": strPtr("HIGH"),
				})
			}
		}

		// ── Groq ──
		if m.Provider == types.ProviderNameGroq && m.ID == "qwen/qwen3-32b" {
			def := strPtr("default")
			mergeTLM(m, map[string]*string{"minimal": nil, "low": nil, "medium": nil, "high": def})
		}

		// ── OpenAI Codex ──
		if m.Provider == types.ProviderNameOpenAICodex && supportsOpenAiXhigh(m.ID) {
			low := strPtr("low")
			mergeTLM(m, map[string]*string{"minimal": low})
		}

		// ── OpenRouter ──
		if m.Provider == types.ProviderNameOpenRouter && strings.HasPrefix(m.ID, "inception/mercury-2") {
			mergeTLM(m, map[string]*string{"off": nil})
		}

		// ── OpenCode Go ──
		if m.Provider == types.ProviderNameOpenCodeGo && m.ID == "kimi-k2.6" {
			mergeTLM(m, map[string]*string{"minimal": nil, "low": nil, "medium": nil})
		}

		// ── OpenCode Zen ──
		if m.Provider == types.ProviderNameOpenCode && m.ID == "grok-build-0.1" {
			mergeTLM(m, map[string]*string{"off": nil, "minimal": nil, "low": nil, "medium": nil})
		}

		// ── Ant Ling ──
		if m.Provider == types.ProviderNameAntLing && m.Reasoning {
			xhigh := strPtr("xhigh")
			mergeTLM(m, map[string]*string{
				"off": nil, "minimal": nil, "low": nil, "medium": nil,
				"high": strPtr("high"), "xhigh": xhigh,
			})
		}
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────────

func mergeTLM(m *types.Model, vals map[string]*string) {
	if m.ThinkingLevelMap == nil {
		m.ThinkingLevelMap = make(map[string]*string, len(vals))
	}
	for k, v := range vals {
		m.ThinkingLevelMap[k] = v
	}
}

func strPtr(s string) *string { return &s }

// noneReasoningModels are openai-responses models that get off:"none".
var noneReasoningModels = map[string]bool{
	"gpt-5.1":       true,
	"gpt-5.2":       true,
	"gpt-5.3-codex": true,
	"gpt-5.4":       true,
	"gpt-5.4-mini":  true,
	"gpt-5.4-nano":  true,
	"gpt-5.5":       true,
}

func supportsOpenAiXhigh(id string) bool {
	return strings.Contains(id, "gpt-5.2") ||
		strings.Contains(id, "gpt-5.3") ||
		strings.Contains(id, "gpt-5.4") ||
		strings.Contains(id, "gpt-5.5")
}

func isAnthropicAdaptiveThinking(id string) bool {
	return strings.Contains(id, "opus-4-6") ||
		strings.Contains(id, "opus-4.6") ||
		strings.Contains(id, "opus-4-7") ||
		strings.Contains(id, "opus-4.7") ||
		strings.Contains(id, "opus-4-8") ||
		strings.Contains(id, "opus-4.8") ||
		strings.Contains(id, "sonnet-4-6") ||
		strings.Contains(id, "sonnet-4.6") ||
		strings.Contains(id, "fable-5")
}

func isAnthropicTemperatureUnsupported(id string) bool {
	lower := strings.ToLower(id)
	return strings.Contains(lower, "opus-4-7") || strings.Contains(lower, "opus-4.7") ||
		strings.Contains(lower, "opus-4-8") || strings.Contains(lower, "opus-4.8")
}

func isGemini3Pro(id string) bool {
	s := strings.ToLower(id)
	return strings.HasPrefix(s, "gemini-3") && strings.Contains(s, "-pro") && !strings.Contains(s, "-flash")
}

func isGemini3Flash(id string) bool {
	s := strings.ToLower(id)
	return strings.HasPrefix(s, "gemini-3") && strings.Contains(s, "-flash")
}

func isGemma4(id string) bool {
	s := strings.ToLower(id)
	return strings.HasPrefix(s, "gemma-4") || strings.HasPrefix(s, "gemma4")
}
