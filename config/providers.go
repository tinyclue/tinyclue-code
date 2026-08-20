package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/tinyclue/tinyclue-code/models_cache"
	"github.com/tinyclue/tinyclue-code/models_cache/core/output"
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

// ProviderInfo describes a provider entry shown in the login panel.
type ProviderInfo struct {
	Name  string // internal name, used as key in auth.json
	Label string // display name shown in the UI
}

// Providers is the list of all known providers, populated at init.
var Providers []ProviderInfo

// Data holds the full model-cache data, populated at init.
// Key is provider name, value is map of model ID → Model.
var Data map[string]map[string]types.Model

func init() {
	// 模型清单读缓存目录（可用 TINYCLUE_CACHE_DIR 覆盖；默认 ~/.tinyclue/caches），
	// 不依赖 CWD。缓存缺失/损坏则联网生成；
	// 生成失败视为启动失败（提示恢复方式后退出）。
	p, err := ModelCachePath()
	if err != nil {
		fatalErrf("无法确定 model 缓存路径: %v", err)
	}

	raw, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(raw, &Data) != nil {
		// 缓存缺失或损坏 → 联网生成。
		result, runErr := models_cache.Run()
		if runErr != nil {
			fatalErrf("生成 model-cache 失败（可能离线）: %v\n请联网重试，或重新运行 ./install.sh 以从仓库种子缓存文件到 %s", runErr, p)
		}
		if mkErr := os.MkdirAll(filepath.Dir(p), 0755); mkErr != nil {
			fatalErrf("创建缓存目录失败: %v", mkErr)
		}
		if saveErr := output.SaveJSON(result, p); saveErr != nil {
			fatalErrf("保存 model-cache.json 失败: %v", saveErr)
		}
		Data = result
	}

	// Build provider list from JSON keys, sorted by label.
	Providers = make([]ProviderInfo, 0, len(Data))
	for name := range Data {
		Providers = append(Providers, ProviderInfo{
			Name:  name,
			Label: types.ProviderLabel(name),
		})
	}
	sort.Slice(Providers, func(i, j int) bool {
		return Providers[i].Label < Providers[j].Label
	})
}

// fatalErrf 打印启动失败原因到 stderr 后退出。
// 启动阶段 log 包默认 writer 为空（除非 slog.json 配置了文件日志），
// 若只走 log.Errorf 用户看不到失败原因，因此启动失败必须直接写 stderr。
func fatalErrf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "tinyclue: 启动失败: "+format+"\n", args...)
	os.Exit(1)
}
