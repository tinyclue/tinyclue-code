package utils

import (
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	types2 "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

func ExtractCompactionDiscoveredTool(entries []types2.SessionEntry, prevCompactionIndex int) []string {
	var result []string
	if prevCompactionIndex < 0 {
		return result
	}
	prev, ok := entries[prevCompactionIndex].(*types2.CompactionEntry)
	if !ok {
		return result
	}
	return prev.DiscoveredTools
}

func ExtractDiscoveredToolsFromMessage(entries []types2.SessionEntry, prevTools []string) []string {
	var names []string
	for _, entry := range entries {
		me, ok := entry.(*types2.MessageEntry)
		if !ok {
			continue
		}
		tempNames := ExtractToolsFromMsg(me.Message)
		if len(tempNames) > 0 {
			names = append(names, tempNames...)
		}
	}
	names = append(names, prevTools...)
	seen := make(map[string]struct{})
	var result []string
	for _, name := range names {
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

func ExtractToolsFromMsg(msg apitypes.Message) []string {
	var result []string
	toolResult, ok := msg.(apitypes.ToolResultMessage)
	if !ok {
		return result
	}
	var names []string
	for _, ref := range toolResult.ToolReferences {
		name, ok := ref["tool_name"].(string)
		if !ok || name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// 从消息列表中提取 tool_reference 引用的工具名称，并去重。
func ExtractDiscoveredToolNames(messages []apitypes.Message) []string {
	var result []string
	for _, msg := range messages {
		toolResult, ok := msg.(apitypes.ToolResultMessage)
		if !ok {
			continue
		}
		for _, ref := range toolResult.ToolReferences {
			name, ok := ref["tool_name"].(string)
			if !ok || name == "" {
				continue
			}
			result = append(result, name)
		}
	}
	return result
}
