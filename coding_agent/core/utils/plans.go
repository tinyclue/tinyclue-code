package utils

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/config"
	"os"
	"path/filepath"
)

var planFileCache = make(map[string]string)

// resolvePlanSlug 返回 sessionId 对应的 plan slug（命中缓存则复用，否则生成并缓存）。
// GetPlanFilePath / GetPlanSlug 共用，保证同会话内路径一致。
func resolvePlanSlug(sessionId string) string {
	slug, ok := planFileCache[sessionId]
	if ok {
		return slug
	}
	for i := 0; i < 10; i++ {
		wordSlug, err := GenerateWordSlug()
		if err != nil {
			continue
		}
		slug = wordSlug
		filePath := filepath.Join(GetPlanDir(), fmt.Sprintf("%s.md", wordSlug))
		exists, _ := PathExists(filePath)
		if exists {
			break
		}
	}
	planFileCache[sessionId] = slug
	return slug
}

// GetPlanSlug 返回 sessionId 对应的 plan slug（EnterPlan 落盘 PlanState.Slug 用）。
func GetPlanSlug(sessionId string) string {
	return resolvePlanSlug(sessionId)
}

// SeedPlanFileCache 用已持久化的 slug 固定 plan 文件路径（会话恢复时调用，
// 否则新进程随机 slug 会把已写的 plan 文件孤儿化）。
func SeedPlanFileCache(sessionId string, slug string) {
	if slug == "" {
		return
	}
	planFileCache[sessionId] = slug
}

func GetPlanFilePath(sessionId string) string {
	slug := resolvePlanSlug(sessionId)
	return filepath.Join(GetPlanDir(), fmt.Sprintf("%s.md", slug))
}

func GetPlanDir() string {
	base, _ := config.TinyClueDir()
	dir := filepath.Join(base, "plans")
	exists, _ := PathExists(dir)
	if !exists {
		os.MkdirAll(dir, 0755)
	}
	return dir
}
