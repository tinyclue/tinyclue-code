package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tinyclue/tinyclue-code/coding_agent/core/usercontext"
)

func main() {
	// Resolve data_examples directory
	dataExamples, err := filepath.Abs("coding_agent/core/usercontext/data_examples")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve data_examples path: %v\n", err)
		os.Exit(1)
	}
	cwd := filepath.Join(dataExamples, "project", "sub", "project")

	// Pre-build paths for each layer
	managedTinyclue := filepath.Join(dataExamples, ".tinyclue", "managed", "TINYCLUE.md")
	managedRules := filepath.Join(dataExamples, ".tinyclue", "managed", "rules")
	userTinyclue := filepath.Join(dataExamples, ".tinyclue", "TINYCLUE.md")
	userRules := filepath.Join(dataExamples, ".tinyclue", "rules")
	addDir := filepath.Join(dataExamples, "add-dir")
	autoMemPath := filepath.Join(dataExamples, "memory", "MEMORY.md")
	teamMemPath := filepath.Join(dataExamples, "team-memory", "MEMORY.md")

	fmt.Println("══════════════════════════════════════════════════════")
	fmt.Println("  UserContext 7-Layer Test with data_examples")
	fmt.Println("══════════════════════════════════════════════════════")
	fmt.Printf("data_examples: %s\n", dataExamples)
	fmt.Printf("CWD:           %s\n", cwd)
	fmt.Println()

	// ── Test 1: Layer 1 (Managed) only ──
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Test 1: Layer 1 (Managed) only")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	runTest(usercontext.Config{
		HomeDir:           dataExamples,
		CWD:               cwd,
		ManagedTINYCLUE:   managedTinyclue,
		ManagedRulesDir:   managedRules,
		UserTINYCLUE:      "",
		UserRulesDir:      "",
		ProjectFiles:      nil,
		ProjectRulesDir:   "",
		LocalFiles:        nil,
		AddDirectories:    nil,
		AutoMemEntrypoint: "",
		TeamMemEntrypoint: "",
	})

	// ── Test 2: Layer 2 (User) only ──
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Test 2: Layer 2 (User) only")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	runTest(usercontext.Config{
		HomeDir:           dataExamples,
		CWD:               cwd,
		ManagedTINYCLUE:   "",
		ManagedRulesDir:   "",
		UserTINYCLUE:      userTinyclue,
		UserRulesDir:      userRules,
		ProjectFiles:      nil,
		ProjectRulesDir:   "",
		LocalFiles:        nil,
		AddDirectories:    nil,
		AutoMemEntrypoint: "",
		TeamMemEntrypoint: "",
	})

	// ── Test 3: Layer 3 (Project) only ──
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Test 3: Layer 3 (Project) only")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	runTest(usercontext.Config{
		HomeDir:           dataExamples,
		CWD:               cwd,
		ManagedTINYCLUE:   "",
		ManagedRulesDir:   "",
		UserTINYCLUE:      "",
		UserRulesDir:      "",
		ProjectFiles:      []string{"TINYCLUE.md", ".tinyclue/TINYCLUE.md"},
		ProjectRulesDir:   ".tinyclue/rules",
		LocalFiles:        nil,
		AddDirectories:    nil,
		AutoMemEntrypoint: "",
		TeamMemEntrypoint: "",
	})

	// ── Test 4: Layer 4 (Local) only ──
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Test 4: Layer 4 (Local) only")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	runTest(usercontext.Config{
		HomeDir:           dataExamples,
		CWD:               cwd,
		ManagedTINYCLUE:   "",
		ManagedRulesDir:   "",
		UserTINYCLUE:      "",
		UserRulesDir:      "",
		ProjectFiles:      nil,
		ProjectRulesDir:   "",
		LocalFiles:        []string{"TINYCLUE.local.md"},
		AddDirectories:    nil,
		AutoMemEntrypoint: "",
		TeamMemEntrypoint: "",
	})

	// ── Test 5: Layer 5 (AddDirectories) only ──
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Test 5: Layer 5 (AddDirectories) only")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	runTest(usercontext.Config{
		HomeDir:           dataExamples,
		CWD:               cwd,
		ManagedTINYCLUE:   "",
		ManagedRulesDir:   "",
		UserTINYCLUE:      "",
		UserRulesDir:      "",
		ProjectFiles:      nil,
		ProjectRulesDir:   "",
		LocalFiles:        nil,
		AddDirectories:    []string{addDir},
		AutoMemEntrypoint: "",
		TeamMemEntrypoint: "",
	})

	// ── Test 6: Layer 6 (AutoMem) only ──
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Test 6: Layer 6 (AutoMem) only")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	runTest(usercontext.Config{
		HomeDir:           dataExamples,
		CWD:               cwd,
		ManagedTINYCLUE:   "",
		ManagedRulesDir:   "",
		UserTINYCLUE:      "",
		UserRulesDir:      "",
		ProjectFiles:      nil,
		ProjectRulesDir:   "",
		LocalFiles:        nil,
		AddDirectories:    nil,
		AutoMemEntrypoint: autoMemPath,
		TeamMemEntrypoint: "",
	})

	// ── Test 7: Layer 7 (TeamMem) only ──
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Test 7: Layer 7 (TeamMem) only")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	runTest(usercontext.Config{
		HomeDir:           dataExamples,
		CWD:               cwd,
		ManagedTINYCLUE:   "",
		ManagedRulesDir:   "",
		UserTINYCLUE:      "",
		UserRulesDir:      "",
		ProjectFiles:      nil,
		ProjectRulesDir:   "",
		LocalFiles:        nil,
		AddDirectories:    nil,
		AutoMemEntrypoint: "",
		TeamMemEntrypoint: teamMemPath,
	})

	// ── Test 8: ALL 7 layers combined ──
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Test 8: ALL 7 Layers Combined")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	runTest(usercontext.Config{
		HomeDir:           dataExamples,
		CWD:               cwd,
		ManagedTINYCLUE:   managedTinyclue,
		ManagedRulesDir:   managedRules,
		UserTINYCLUE:      userTinyclue,
		UserRulesDir:      userRules,
		ProjectFiles:      []string{"TINYCLUE.md", ".tinyclue/TINYCLUE.md"},
		ProjectRulesDir:   ".tinyclue/rules",
		LocalFiles:        []string{"TINYCLUE.local.md"},
		AddDirectories:    []string{addDir},
		AutoMemEntrypoint: autoMemPath,
		TeamMemEntrypoint: teamMemPath,
	})

	// ── Test 9: GetDefaultConfig with dataExamples as HomeDir ──
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Test 9: GetDefaultConfig + HomeDir/CWD override")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	t9Cfg := usercontext.GetDefaultConfig()
	t9Cfg.HomeDir = dataExamples
	t9Cfg.CWD = cwd
	t9Cfg.UserTINYCLUE = userTinyclue // override default (real home dir)
	t9Cfg.UserRulesDir = userRules
	t9Cfg.ManagedTINYCLUE = managedTinyclue
	t9Cfg.ManagedRulesDir = managedRules
	t9Cfg.AddDirectories = []string{addDir}
	t9Cfg.AutoMemEntrypoint = autoMemPath
	t9Cfg.TeamMemEntrypoint = teamMemPath
	// Note: GetDefaultConfig sets ProjectFiles/LocalFiles/ProjectRulesDir
	// which will do the upward walk from CWD
	runTest(t9Cfg)

	// ── Test 10: Disabled flag ──
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Test 10: Disabled flag (should be empty)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	ctx, err := usercontext.GetUserContext(usercontext.Config{
		HomeDir:  dataExamples,
		CWD:      cwd,
		Disabled: true,
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("TinyclueMd empty: %v (expected: true)\n", ctx.TinyclueMd == "")
		fmt.Printf("CurrentDate: %s\n", ctx.CurrentDate)
	}
	fmt.Println()

	// ── Test 11: CWD from project root (verify upward walk boundary) ──
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Test 11: CWD from project root (verify walk boundary)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	projectRoot := filepath.Join(dataExamples, "project")
	ctx, err = usercontext.GetUserContext(usercontext.Config{
		HomeDir:         dataExamples,
		CWD:             projectRoot,
		ManagedTINYCLUE: "",
		ManagedRulesDir: "",
		UserTINYCLUE:    "",
		UserRulesDir:    "",
		ProjectFiles:    []string{"TINYCLUE.md", ".tinyclue/TINYCLUE.md"},
		ProjectRulesDir: ".tinyclue/rules",
		LocalFiles:      nil,
		AddDirectories:  nil,
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Has content: %v\n", ctx.TinyclueMd != "")
		containsProject := strings.Contains(ctx.TinyclueMd, "Project TINYCLUE.md")
		containsSubProject := strings.Contains(ctx.TinyclueMd, "Sub-Project TINYCLUE.md")
		fmt.Printf("Contains project/TINYCLUE.md:     %v (expected: true)\n", containsProject)
		fmt.Printf("Contains sub/project files:     %v (expected: false, below CWD)\n", containsSubProject)
	}
	fmt.Println()

	// ── Test 12: PrependUserContext wrapper output ──
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Test 12: PrependUserContext output format")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	ctx, err = usercontext.GetUserContext(usercontext.Config{
		HomeDir:           dataExamples,
		CWD:               cwd,
		ManagedTINYCLUE:   "",
		ManagedRulesDir:   "",
		UserTINYCLUE:      "",
		UserRulesDir:      "",
		ProjectFiles:      []string{"TINYCLUE.md"},
		ProjectRulesDir:   "",
		LocalFiles:        nil,
		AddDirectories:    nil,
		AutoMemEntrypoint: "",
		TeamMemEntrypoint: "",
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		msg := usercontext.PrependUserContext(ctx)
		fmt.Println(msg)
	}

	fmt.Println("\n══════════════════════════════════════════════════════")
	fmt.Println("  All 12 tests completed.")
	fmt.Println("══════════════════════════════════════════════════════")
}

func runTest(cfg usercontext.Config) {
	ctx, err := usercontext.GetUserContext(cfg)
	if err != nil {
		fmt.Printf("Error: %v\n\n", err)
		return
	}

	tinyclueMd := ctx.TinyclueMd
	if tinyclueMd == "" {
		fmt.Println("(no TINYCLUE.md content found)")
		fmt.Println()
		return
	}

	fmt.Printf("Total: %d chars, %d lines\n", len(tinyclueMd), strings.Count(tinyclueMd, "\n")+1)
	fmt.Println("──────────────────────────────────────────────────────")

	// Show file references found
	lines := strings.Split(tinyclueMd, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Contents of ") {
			fmt.Println("  " + line)
		}
	}

	fmt.Println("──────────────────────────────────────────────────────")
	fmt.Println("Content preview (first 50 lines):")
	fmt.Println("──────────────────────────────────────────────────────")
	previewLines := strings.Split(tinyclueMd, "\n")
	if len(previewLines) > 50 {
		previewLines = previewLines[:50]
	}
	for i, line := range previewLines {
		fmt.Printf("%4d: %s\n", i+1, strings.TrimRight(line, " \t"))
	}
	if strings.Count(tinyclueMd, "\n")+1 > 50 {
		fmt.Printf("  ... (%d more lines)\n", strings.Count(tinyclueMd, "\n")+1-50)
	}
	fmt.Println()
}
