package utils

import (
	"sort"
	"strings"

	"github.com/tinyclue/tinyclue-code/api_provider/types"
	types2 "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

// NewFileOps creates a new types2.FileOperations with initialized slices.
func NewFileOps() *types2.FileOperations {
	return &types2.FileOperations{
		Read:    make([]string, 0),
		Written: make([]string, 0),
		Edited:  make([]string, 0),
	}
}

// addOnce appends s to the slice only if it isn't already present.
func addOnce(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// ExtractFileOpsFromMsg extracts file paths from tool calls in an assistant
// message and adds them to the given types2.FileOperations.
func ExtractFileOpsFromMsg(msg types.Message, fileOps *types2.FileOperations) {
	assistant, ok := msg.(types.AssistantMessage)
	if !ok {
		return
	}

	for _, tc := range assistant.ToolCalls {
		path, ok := tc.Arguments["path"].(string)
		if !ok || path == "" {
			continue
		}

		switch tc.Name {
		case types2.READ_TOOL_NAME:
			fileOps.Read = addOnce(fileOps.Read, path)
		case types2.WRITE_TOOL_NAME:
			fileOps.Written = addOnce(fileOps.Written, path)
		case types2.EDIT_TOOL_NAME:
			fileOps.Edited = addOnce(fileOps.Edited, path)
		}
	}
}

// ExtractFileOpsFromMessage extracts file paths from tool calls in all message
// entries and adds them to the given types2.FileOperations.
func ExtractFileOpsFromMessage(entries []types2.SessionEntry, fileOps *types2.FileOperations) {
	for _, entry := range entries {
		me, ok := entry.(*types2.MessageEntry)
		if !ok {
			continue
		}
		ExtractFileOpsFromMsg(me.Message, fileOps)
	}
}

// ExtractCompactionOperations carries forward file paths from a previous
// compaction entry into the given types2.FileOperations.
func ExtractCompactionOperations(entries []types2.SessionEntry, prevCompactionIndex int, fileOps *types2.FileOperations) {
	if prevCompactionIndex < 0 {
		return
	}
	prev, ok := entries[prevCompactionIndex].(*types2.CompactionEntry)
	if !ok {
		return
	}
	for _, f := range prev.ReadFiles {
		fileOps.Read = addOnce(fileOps.Read, f)
	}
	for _, f := range prev.ModifyFiles {
		fileOps.Edited = addOnce(fileOps.Edited, f)
	}
}

// ComputeFileLists separates file operations into read-only and modified sets.
func ComputeFileLists(fileOps *types2.FileOperations) types2.FileLists {
	modified := make(map[string]struct{})
	for _, f := range fileOps.Edited {
		modified[f] = struct{}{}
	}
	for _, f := range fileOps.Written {
		modified[f] = struct{}{}
	}

	var readOnly []string
	for _, f := range fileOps.Read {
		if _, ok := modified[f]; !ok {
			readOnly = append(readOnly, f)
		}
	}

	modifiedFiles := make([]string, 0, len(modified))
	for f := range modified {
		modifiedFiles = append(modifiedFiles, f)
	}

	sort.Strings(readOnly)
	sort.Strings(modifiedFiles)

	return types2.FileLists{
		ReadFiles:     readOnly,
		ModifiedFiles: modifiedFiles,
	}
}

// FormatFileOperations formats file lists into an XML-like tag section.
func FormatFileOperations(fl types2.FileLists) string {
	var sections []string
	if len(fl.ReadFiles) > 0 {
		sections = append(sections, "<read-files>\n"+strings.Join(fl.ReadFiles, "\n")+"\n</read-files>")
	}
	if len(fl.ModifiedFiles) > 0 {
		sections = append(sections, "<modified-files>\n"+strings.Join(fl.ModifiedFiles, "\n")+"\n</modified-files>")
	}
	if len(sections) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(sections, "\n\n")
}
