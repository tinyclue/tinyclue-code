package types

// ── Provider name constants ─────────────────────────────────────────────────────

const (
	ProviderNameAmazonBedrock       = "amazon-bedrock"
	ProviderNameAnthropic           = "anthropic"
	ProviderNameAntLing             = "ant-ling"
	ProviderNameCerebras            = "cerebras"
	ProviderNameCloudflareAIGateway = "cloudflare-ai-gateway"
	ProviderNameCloudflareWorkersAI = "cloudflare-workers-ai"
	ProviderNameDeepSeek            = "deepseek"
	ProviderNameFireworks           = "fireworks"
	ProviderNameGitHubCopilot       = "github-copilot"
	ProviderNameGoogle              = "google"
	ProviderNameGoogleVertex        = "google-vertex"
	ProviderNameGroq                = "groq"
	ProviderNameHuggingFace         = "huggingface"
	ProviderNameKimiForCoding       = "kimi-for-coding"
	ProviderNameMiniMax             = "minimax"
	ProviderNameMiniMaxCN           = "minimax-cn"
	ProviderNameMistral             = "mistral"
	ProviderNameMoonshotAI          = "moonshotai"
	ProviderNameMoonshotAICN        = "moonshotai-cn"
	ProviderNameNvidia              = "nvidia"
	ProviderNameOpenAI              = "openai"
	ProviderNameOpenAICodex         = "openai-codex"
	ProviderNameOpenCode            = "opencode"
	ProviderNameOpenCodeGo          = "opencode-go"
	ProviderNameOpenRouter          = "openrouter"
	ProviderNameTogether            = "together"
	ProviderNameVercelAIGateway     = "vercel-ai-gateway"
	ProviderNameXAI                 = "xai"
	ProviderNameXiaomi              = "xiaomi"
	ProviderNameZAICodingPlan       = "zai-coding-plan"
)

// ── Provider label constants ────────────────────────────────────────────────────

const (
	ProviderLabelAmazonBedrock       = "Amazon Bedrock"
	ProviderLabelAnthropic           = "Anthropic (Claude Pro/Max)"
	ProviderLabelAntLing             = "Ant Ling"
	ProviderLabelCerebras            = "Cerebras"
	ProviderLabelCloudflareAIGateway = "Cloudflare AI Gateway"
	ProviderLabelCloudflareWorkersAI = "Cloudflare Workers AI"
	ProviderLabelDeepSeek            = "DeepSeek"
	ProviderLabelFireworks           = "Fireworks AI"
	ProviderLabelGitHubCopilot       = "GitHub Copilot"
	ProviderLabelGoogle              = "Google AI Studio"
	ProviderLabelGoogleVertex        = "Google Gemini (Vertex AI)"
	ProviderLabelGroq                = "Groq"
	ProviderLabelHuggingFace         = "Hugging Face"
	ProviderLabelKimiForCoding       = "Kimi for Coding"
	ProviderLabelMiniMax             = "Minimax"
	ProviderLabelMiniMaxCN           = "Minimax (China)"
	ProviderLabelMistral             = "Mistral AI"
	ProviderLabelMoonshotAI          = "Moonshot AI"
	ProviderLabelMoonshotAICN        = "Moonshot AI (China)"
	ProviderLabelNvidia              = "NVIDIA AI"
	ProviderLabelOpenAI              = "OpenAI"
	ProviderLabelOpenAICodex         = "OpenAI Codex"
	ProviderLabelOpenCode            = "OpenCode"
	ProviderLabelOpenCodeGo          = "OpenCode Go"
	ProviderLabelOpenRouter          = "OpenRouter"
	ProviderLabelTogether            = "Together AI"
	ProviderLabelVercelAIGateway     = "Vercel AI Gateway"
	ProviderLabelXAI                 = "xAI"
	ProviderLabelXiaomi              = "Xiaomi"
	ProviderLabelZAICodingPlan       = "ZAI Coding Plan"
)

// ProviderLabel returns the display label for a given provider name.
// Falls back to the name itself if unknown.
func ProviderLabel(name string) string {
	switch name {
	case ProviderNameAmazonBedrock:
		return ProviderLabelAmazonBedrock
	case ProviderNameAnthropic:
		return ProviderLabelAnthropic
	case ProviderNameAntLing:
		return ProviderLabelAntLing
	case ProviderNameCerebras:
		return ProviderLabelCerebras
	case ProviderNameCloudflareAIGateway:
		return ProviderLabelCloudflareAIGateway
	case ProviderNameCloudflareWorkersAI:
		return ProviderLabelCloudflareWorkersAI
	case ProviderNameDeepSeek:
		return ProviderLabelDeepSeek
	case ProviderNameFireworks:
		return ProviderLabelFireworks
	case ProviderNameGitHubCopilot:
		return ProviderLabelGitHubCopilot
	case ProviderNameGoogle:
		return ProviderLabelGoogle
	case ProviderNameGoogleVertex:
		return ProviderLabelGoogleVertex
	case ProviderNameGroq:
		return ProviderLabelGroq
	case ProviderNameHuggingFace:
		return ProviderLabelHuggingFace
	case ProviderNameKimiForCoding:
		return ProviderLabelKimiForCoding
	case ProviderNameMiniMax:
		return ProviderLabelMiniMax
	case ProviderNameMiniMaxCN:
		return ProviderLabelMiniMaxCN
	case ProviderNameMistral:
		return ProviderLabelMistral
	case ProviderNameMoonshotAI:
		return ProviderLabelMoonshotAI
	case ProviderNameMoonshotAICN:
		return ProviderLabelMoonshotAICN
	case ProviderNameNvidia:
		return ProviderLabelNvidia
	case ProviderNameOpenAI:
		return ProviderLabelOpenAI
	case ProviderNameOpenAICodex:
		return ProviderLabelOpenAICodex
	case ProviderNameOpenCode:
		return ProviderLabelOpenCode
	case ProviderNameOpenCodeGo:
		return ProviderLabelOpenCodeGo
	case ProviderNameOpenRouter:
		return ProviderLabelOpenRouter
	case ProviderNameTogether:
		return ProviderLabelTogether
	case ProviderNameVercelAIGateway:
		return ProviderLabelVercelAIGateway
	case ProviderNameXAI:
		return ProviderLabelXAI
	case ProviderNameXiaomi:
		return ProviderLabelXiaomi
	case ProviderNameZAICodingPlan:
		return ProviderLabelZAICodingPlan
	default:
		return name
	}
}
