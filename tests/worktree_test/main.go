package main

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"github.com/tinyclue/tinyclue-code/config"
	"os"
	"path/filepath"
)

func main() {
	cwd := config.CLI.Cwd
	agentId := utils.CreateAgentId("")
	agentId = agentId[:8]
	info, err := utils.CreateAgentWorktree(agentId, cwd)
	fmt.Println(info)
	fmt.Println(err)

	b1 := utils.HasWorktreeChanges(info.WorktreePath, info.HeadCommit)
	fmt.Println(b1)
	// Modify a file in the worktree
	_ = os.WriteFile(filepath.Join(info.WorktreePath, "new.txt"), []byte("hello"), 0644)

	b2 := utils.HasWorktreeChanges(info.WorktreePath, info.HeadCommit)
	fmt.Println(b2)

	b3 := utils.RemoveAgentWorktree(info)
	fmt.Println(b3)
}
