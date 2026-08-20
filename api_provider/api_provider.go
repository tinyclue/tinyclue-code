// Package api_provider 提供 API 调用门面层，自动管理厂商路由。
// 使用模版方法模式：Client.Chat / Client.ChatStream 统一处理 HTTP 传输层，
// 各厂商通过 ProtocolAdapter 接口实现协议转换。
package api_provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/log"
	"github.com/tinyclue/tinyclue-code/subscription"
)

// Client 是门面层，对外提供统一调用入口，对内使用模版方法模式。
type Client struct {
	adapter    types.ProtocolAdapter
	httpClient *http.Client
	baseURL    string
	times      int
}

// Adapter 返回当前 Client 使用的协议适配器。
func (c *Client) Adapter() types.ProtocolAdapter {
	return c.adapter
}

// NewWithAdapter 使用指定的适配器创建 Client。
func NewWithAdapter(adapter types.ProtocolAdapter) *Client {
	return &Client{
		adapter:    adapter,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// Chat 发送非流式对话请求，返回 EventStream。
func (c *Client) Chat(ctx context.Context, req *types.ChatRequest) *types.EventStream {
	s := types.NewEventStream(c.adapter.Name(), c.adapter.Model(), c.baseURL)

	req.ReasoningEffort = config.Cnf.ReasoningEffort()

	body, err := c.adapter.BuildRequest(req, false)
	if err != nil {
		s.SendError(types.ErrCodeBuildRequest, fmt.Sprintf("%s: build request: %v", c.adapter.Name(), err), "")
		return s
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+c.adapter.Endpoint(), bytes.NewReader(body))
	if err != nil {
		s.SendError(types.ErrCodeNewRequest, fmt.Sprintf("%s: create request: %v", c.adapter.Name(), err), "")
		return s
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.adapter.SetHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			s.SendAborted(err.Error())
		} else if errors.Is(err, context.DeadlineExceeded) {
			s.SendError(types.ErrCodeTimeout, err.Error(), "")
		} else {
			s.SendError(types.ErrCodeRequest, err.Error(), "")
		}
		return s
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		bodyStr := string(respBody)
		s.SendError(resp.StatusCode, parseErrorMsg(respBody), bodyStr)
		return s
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		s.SendError(types.ErrCodeReadBody, fmt.Sprintf("%s: read response: %v", c.adapter.Name(), err), "")
		return s
	}

	chatResp, err := c.adapter.ParseResponse(respBody)
	if err != nil {
		s.SendError(types.ErrCodeParseResponse, err.Error(), "")
		return s
	}
	chatResp.RawBody = respBody

	// ParseResponse 填充了 Content、ResponseID、ResponseModel、StopReason、Usage
	// 拷贝到 EventStream 内部 response，保留 Provider/Model/API/Timestamp/Error
	r := s.Response()
	r.Content = chatResp.Content
	r.RawBody = chatResp.RawBody

	// 计算费用
	if mc := c.adapter.ModelCost(); mc != nil && r.Content.Usage.TotalTokens > 0 {
		r.Content.Usage.Cost = types.CalculateCost(*mc,
			r.Content.Usage.Input, r.Content.Usage.Output,
			r.Content.Usage.CacheRead, r.Content.Usage.CacheWrite)
	}

	return s
}

// ChatStream 发送流式对话请求，返回 EventStream。
func (c *Client) ChatStream(ctx context.Context, req *types.ChatRequest) *types.EventStream {
	s := types.NewEventStream(c.adapter.Name(), c.adapter.Model(), c.baseURL)
	s.InitStream()

	req.ReasoningEffort = config.Cnf.ReasoningEffort()

	body, err := c.adapter.BuildRequest(req, true)
	if err != nil {
		s.SendError(types.ErrCodeBuildRequest, fmt.Sprintf("%s: build stream request: %v", c.adapter.Name(), err), "")
		return s
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+c.adapter.Endpoint(), bytes.NewReader(body))
	if err != nil {
		s.SendError(types.ErrCodeNewRequest, fmt.Sprintf("%s: create stream request: %v", c.adapter.Name(), err), "")
		return s
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.adapter.SetHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			s.SendAborted(err.Error())
		} else if errors.Is(err, context.DeadlineExceeded) {
			s.SendError(types.ErrCodeTimeout, err.Error(), "")
		} else {
			s.SendError(types.ErrCodeRequest, err.Error(), "")
		}
		return s
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		bodyStr := string(respBody)
		s.SendError(resp.StatusCode, parseErrorMsg(respBody), bodyStr)
		return s
	}

	go c.adapter.ReadStream(ctx, resp, s)
	return s
}

// =============================================================
// 全局默认 Client
// =============================================================

var defaultClient *Client

func init() {
	c, err := NewFromConfig()
	if err != nil {
		log.Errorf(context.Background(), "api_provider: init client failed: %v", err)
		return
	}
	defaultClient = c
}

// GetClient 返回全局默认 Client。调用方应注意 nil 检查。
func GetClient() *Client {
	return defaultClient
}

// UpdateFromConfig 重新从配置文件创建 Client 并替换全局默认实例，用于配置热更新。
func UpdateFromConfig() error {
	c, err := NewFromConfig()
	if err != nil {
		return err
	}
	defaultClient = c
	return nil
}

// parseErrorMsg 从厂商错误响应体中提取可读的错误描述。
// 支持的格式: {error:{message:...}} / {type:error,error:{message:...}}
func parseErrorMsg(body []byte) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return string(body)
}

func NewFromConfig() (*Client, error) {
	registerProviders()

	name := config.Cnf.DefaultProvider()
	if name == "" {
		return nil, fmt.Errorf("api_provider: no default provider in settings")
	}

	authType := config.Cnf.AuthTypeFor(name)
	var key string
	switch authType {
	case "oauth":
		// 订阅 OAuth：取可刷新的 access token（x/oauth2 TokenSource 就近刷新 + saving source 落盘）。
		tok, err := subscription.AccessToken(context.Background(), name)
		if err != nil {
			// 订阅 token 暂不可用（缺失/刷新失败）：不整体失败返回 nil client（否则调用方
			// GetClient().Adapter() 直接 nil deref）。构建空凭据 client，请求会收到厂商 401
			// 在 chat 呈现；重登（/login）后 UpdateFromConfig 恢复。
			log.Errorf(context.Background(), "api_provider: oauth token for %q unavailable: %v; requests will fail until /login re-auth", name, err)
		} else {
			key = tok
		}
	default:
		var ok bool
		key, ok = config.Cnf.AuthFor(name)
		if !ok || key == "" {
			// 同上：缺 api-key 不整体失败，构建空凭据 client（首次请求收到厂商 401 提示）。
			log.Errorf(context.Background(), "api_provider: no auth for default provider %q; requests will fail until configured via /login", name)
		}
	}

	factory, ok := types.GetAdapterFactory(name)
	if !ok {
		return nil, fmt.Errorf("api_provider: unknown provider %q (did you import its package?)", name)
	}

	adapter := factory(key, config.Cnf.DefaultModel(), authType)

	return &Client{
		adapter:    adapter,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		baseURL:    adapter.BaseURL(),
	}, nil
}
