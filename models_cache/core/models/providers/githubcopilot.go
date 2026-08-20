package providers

import (
	"regexp"
	"strings"

	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

var claude4Regex = regexp.MustCompile(`^claude-(haiku|sonnet|opus)-4([.\-]|$)`)

var noEagerTool = map[string]bool{
	"github-copilot:claude-haiku-4.5":  true,
	"github-copilot:claude-sonnet-4":   true,
	"github-copilot:claude-sonnet-4.5": true,
}

type GitHubCopilotProvider struct {
	ProviderSkeleton
}

func (p *GitHubCopilotProvider) Name() string { return types.ProviderNameGitHubCopilot }

func (p *GitHubCopilotProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.GitHubCopilot
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		if m.Status != nil && *m.Status == "deprecated" {
			continue
		}

		isClaude4 := claude4Regex.MatchString(id)
		needsResponses := strings.HasPrefix(id, "gpt-5") || strings.HasPrefix(id, "oswe")

		var api string
		var compat any
		switch {
		case isClaude4:
			api = "anthropic-messages"
			key := "github-copilot:" + id
			if noEagerTool[key] {
				compat = map[string]any{"supportsEagerToolInputStreaming": false}
			}
		case needsResponses:
			api = "openai-responses"
		default:
			api = "openai-completions"
			compat = map[string]any{
				"supportsStore":           false,
				"supportsDeveloperRole":   false,
				"supportsReasoningEffort": false,
			}
		}

		model := BuildModel(id, m, api, "github-copilot",
			"https://api.individual.githubcopilot.com")
		if m.Limit == nil || m.Limit.Context == nil {
			model.ContextWindow = 128000
		}
		if m.Limit == nil || m.Limit.Output == nil {
			model.MaxTokens = 8192
		}
		model.Headers = map[string]string{
			"User-Agent":             "GitHubCopilotChat/0.35.0",
			"Editor-Version":         "vscode/1.107.0",
			"Editor-Plugin-Version":  "copilot-chat/0.35.0",
			"Copilot-Integration-Id": "vscode-chat",
		}
		if compat != nil {
			model.Compat = compat
		}
		result = append(result, model)
	}
	return result
}

func (p *GitHubCopilotProvider) Patch(models []types.Model) []types.Model {
	var clone *types.Model

	for i, m := range models {
		if m.ID == "claude-opus-4-6" || m.ID == "claude-sonnet-4-6" ||
			m.ID == "claude-opus-4.6" || m.ID == "claude-sonnet-4.6" {
			models[i].ContextWindow = 1000000
		}
		if m.ID == "gpt-5.2-codex" {
			c := m
			clone = &c
		}
	}

	// Clone gpt-5.2-codex → gpt-5.3-codex if source exists.
	if clone != nil {
		exists := false
		for _, m := range models {
			if m.ID == "gpt-5.3-codex" {
				exists = true
				break
			}
		}
		if !exists {
			c := *clone
			c.ID = "gpt-5.3-codex"
			c.Name = "GPT-5.3 Codex"
			models = append(models, c)
		}
	}

	return models
}

func (p *GitHubCopilotProvider) Inject() []types.Model {
	return nil
}
