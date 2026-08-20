package api

import (
	"net/http"
	"time"
)

// httpClient 是模型清单抓取的共享 HTTP 客户端。
// 所有 fetch 共用同一个超时，避免半开连接（如断网/黑盒网络）导致启动无限挂起：
// models_cache.Run 顺序抓取 4 个上游，任一 fetch 在 timeout 后即失败返回。
const fetchTimeout = 15 * time.Second

var httpClient = &http.Client{Timeout: fetchTimeout}
