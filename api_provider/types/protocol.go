// Package types 定义统一的 API 调用协议类型。
package types

import (
	"context"
	"net/http"
)

// =============================================================
// 统一协议 — 内部标准数据类型
// =============================================================

// ChatRequest 统一的聊天请求参数。
type ChatRequest struct {
	Messages        []Message
	Tools           []Tool
	Temperature     *float64 // nil 表示使用厂商默认值
	MaxTokens       *int     // nil 表示使用厂商默认值
	ReasoningEffort string   // 推理级别: low/medium/high/max，空字符串表示厂商默认
	Stream          bool
}

// ModelCost 定义模型每百万 token 的单价（美元）。
// 各厂商在各自文件中定义具体的价格表。
type ModelCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`  // 缓存命中（读取）单价
	CacheWrite float64 `json:"cacheWrite"` // 缓存写入单价
}

// CalculateCost 根据用量和模型单价计算费用。
// input 为非缓存 input tokens，cacheRead/cacheWrite 为缓存部分，三者互斥不重叠。
func CalculateCost(cost ModelCost, input, output, cacheRead, cacheWrite int) Cost {
	c := Cost{}
	c.Input = (cost.Input / 1_000_000) * float64(input)
	c.Output = (cost.Output / 1_000_000) * float64(output)
	c.CacheRead = (cost.CacheRead / 1_000_000) * float64(cacheRead)
	c.CacheWrite = (cost.CacheWrite / 1_000_000) * float64(cacheWrite)
	c.Total = c.Input + c.Output + c.CacheRead + c.CacheWrite
	return c
}

// CalculateUsage 根据原始 token 数计算 Usage。
// Input 为非缓存 input tokens，CacheRead/CacheWrite 为缓存部分。
// 各厂商 API 行为不同，调用前需统一为"input 不含 cache"的约定：
//   - Anthropic/DeepSeek: input_tokens 本身已是非缓存值，直通即可
//   - OpenAI: prompt_tokens 包含缓存，调用前需扣除 cacheRead/cacheWrite
//
// TotalTokens = input + output + cacheRead + cacheWrite（反映模型实际处理总量，用于上下文窗口跟踪）。
func CalculateUsage(input, output, cacheRead, cacheWrite int) Usage {
	return Usage{
		Input:       input,
		Output:      output,
		CacheRead:   cacheRead,
		CacheWrite:  cacheWrite,
		TotalTokens: input + output + cacheRead + cacheWrite,
	}
}

type Cost struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
	Total      float64
}

type Usage struct {
	Input       int
	Output      int
	CacheRead   int
	CacheWrite  int
	TotalTokens int
	Cost        Cost
}

type ChatResponse struct {
	Content AssistantMessage
	Error   Error
	RawBody []byte // 原始响应体，供 WebSearch 等工具自行解析
}

type Error struct {
	IsSuccess    bool   // 是否成功
	Code         int    // 错误码：10000-19999=内部错误，HTTP 状态码=服务器返回
	ErrorMessage string // 错误信息
	RawMessage   string // 原始返回
}

// =============================================================
// 流式事件定义
// =============================================================

// 流式事件类型常量。
const (
	EventTypeStart         = "start"
	EventTypeTextStart     = "text_start"
	EventTypeTextDelta     = "text_delta"
	EventTypeTextEnd       = "text_end"
	EventTypeThinkingStart = "thinking_start"
	EventTypeThinkingDelta = "thinking_delta"
	EventTypeThinkingEnd   = "thinking_end"
	EventTypeToolCallStart = "toolcall_start"
	EventTypeToolCallDelta = "toolcall_delta"
	EventTypeToolCallEnd   = "toolcall_end"
	EventTypeDone          = "done"
	EventTypeError         = "error"
)

// StreamEvent 流式响应事件。
// EventType 标明事件阶段；Delta 存放 text_delta / thinking_delta 的增量文本；
// Response 是指针，readStream 在整个流生命周期中复用同一对象，在其上积累 Content。
type StreamEvent struct {
	EventType string
	Delta     string
	Response  *ChatResponse
}

// =============================================================
// 错误码定义
//
// 10000-19999 — 内部错误（无 HTTP 状态码可用）
// HTTP 状态码 — 服务器返回的实际 HTTP 状态码（400, 429, 500 等）
// =============================================================

const (
	ErrCodeBuildRequest  = 10000 + iota // 构建请求体失败 — 不可重试
	ErrCodeNewRequest                   // 创建 HTTP 请求失败 — 不可重试
	ErrCodeRequest                      // HTTP 传输失败（网络/DNS/连接拒绝）— 可重试
	ErrCodeTimeout                      // 请求超时 — 可重试
	ErrCodeAborted                      // 调用方取消（context.Canceled）— 不可重试
	ErrCodeReadBody                     // 读取响应体失败 — 可重试
	ErrCodeParseResponse                // 解析响应体失败 — 不可重试
	ErrCodeStreamParse                  // 解析流式块 JSON 失败 — 不可重试
	ErrCodeStreamRead                   // 读取流式数据 I/O 错误 — 可重试
	ErrCodeStreamDecode                 // JSON 解码流式块失败 — 可重试
	ErrCodeStreamSend                   // 流式事件发送失败（channel 异常）— 不可重试
)

func IsHttpStatusCode(code int) bool {
	return code >= 100 && code <= 599
}

// IsRetryableCode 返回给定错误码是否可重试。
func IsRetryableCode(code int) bool {
	switch code {
	case ErrCodeRequest, ErrCodeTimeout, ErrCodeReadBody, ErrCodeStreamRead, ErrCodeStreamDecode:
		return true
	default:
		return false
	}
}

// =============================================================
// ProtocolAdapter — 协议转换接口
// =============================================================

// ProtocolAdapter 定义各厂商协议转换的接口。
// api_provider.Client 使用模版方法模式调用这些接口，
// 统一处理 HTTP 传输层，各厂商只需实现协议转换。
type ProtocolAdapter interface {
	// Name 返回提供商名称（如 "deepseek"、"openai"）。
	Name() string
	// Model 返回模型名称。
	Model() string
	// ModelCost 返回模型价格表。
	ModelCost() *ModelCost
	// BaseURL 返回 API 基础地址。
	BaseURL() string

	// Endpoint 返回 API 路径（如 "/chat/completions"）。
	Endpoint() string
	// BuildRequest 将内部请求转换为厂商协议的字节流。
	BuildRequest(req *ChatRequest, stream bool) ([]byte, error)
	// ParseResponse 将厂商响应解析为内部 ChatResponse。
	// 返回的 ChatResponse 应包含 Content、ResponseID、ResponseModel、StopReason、Usage（不含 Cost）。
	ParseResponse(respBody []byte) (*ChatResponse, error)
	// SetHeaders 设置厂商特定的 HTTP 请求头。
	SetHeaders(r *http.Request)
	// ReadStream 读取流式响应，将事件发送到 s。
	ReadStream(ctx context.Context, resp *http.Response, s *EventStream)
	// SupportsVision 返回当前模型是否支持图片输入。
	SupportsVision() bool
	// ContextWindow 返回模型的最大上下文窗口（input + output 总 token 数）。
	// 用于检测 context overflow。未知模型返回 0。
	ContextWindow() int
	// MaxTokens 返回模型的最大输出 token 数。
	// 用于限制单次生成的最大长度。未知模型返回 0。
	MaxTokens() int
}

// =============================================================
// ProtocolAdapter 工厂注册
// =============================================================

// AdapterFactory 创建 ProtocolAdapter 的工厂函数。
// authType 为鉴权方式（"api-key"/"oauth"），适配器据此决定请求头形态。
type AdapterFactory func(key, model, authType string) ProtocolAdapter

var registry = map[string]AdapterFactory{}

// RegisterAdapter 注册一个 Adapter 的工厂函数。
func RegisterAdapter(name string, factory AdapterFactory) {
	registry[name] = factory
}

// GetAdapterFactory 按名称查找已注册的 Adapter 工厂。
func GetAdapterFactory(name string) (AdapterFactory, bool) {
	f, ok := registry[name]
	return f, ok
}
