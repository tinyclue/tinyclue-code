// callback.go：OAuth 授权回调的本地监听器与跨平台浏览器打开器（从 mcp 包抽取）。
package oauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sync"
)

// Result 是浏览器回调携带的授权结果。code 存在即成功；Err 非 nil 表示授权被拒或回调异常。
type Result struct {
	Code  string
	State string
	Iss   string
	Err   error
}

// CallbackServer 是 OAuth 授权回调的本地监听器（127.0.0.1 随机端口）。授权完成后浏览器
// 带 code/state/iss 跳回，Wait 阻塞直到收到结果、被关闭或 ctx 取消。
type CallbackServer struct {
	ln       net.Listener
	server   *http.Server
	resultCh chan *Result
	closed   chan struct{}
	once     sync.Once
}

// NewCallbackServer 绑定本地随机端口并启动 http 服务。
func NewCallbackServer() (*CallbackServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("oauth: bind callback listener: %w", err)
	}
	cb := &CallbackServer{
		ln:       ln,
		resultCh: make(chan *Result, 1),
		closed:   make(chan struct{}),
	}
	cb.server = &http.Server{Handler: cb}
	go func() { _ = cb.server.Serve(ln) }()
	return cb, nil
}

// URL 返回 redirect_uri（授权服务器把 code/state 跳回此处）。
func (cb *CallbackServer) URL() string {
	return "http://" + cb.ln.Addr().String() + "/callback"
}

func (cb *CallbackServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var res *Result
	switch {
	case q.Get("code") != "":
		res = &Result{
			Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss"),
		}
	case q.Get("error") != "":
		res = &Result{Err: fmt.Errorf("oauth: authorization error: %s: %s", q.Get("error"), q.Get("error_description"))}
	default:
		res = &Result{Err: fmt.Errorf("oauth: unexpected callback: %s", r.URL.String())}
	}
	// select 兜底：回调重复命中（浏览器刷新/二次跳转）或服务器已关闭时丢弃结果，绝不永久阻塞
	// 在 channel 写（否则 handler goroutine 泄漏）。结果消费方（Wait）关闭后迟到回调被丢弃。
	select {
	case cb.resultCh <- res:
	case <-cb.closed:
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Authorization complete — you can close this tab and return to tinyclue."))
}

// Wait 阻塞等待回调结果；ctx 取消或 Close 时返回错误。
func (cb *CallbackServer) Wait(ctx context.Context) (*Result, error) {
	select {
	case res := <-cb.resultCh:
		return res, nil
	case <-cb.closed:
		return nil, errors.New("oauth: callback server closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close 幂等关闭监听器。
func (cb *CallbackServer) Close() {
	cb.once.Do(func() {
		_ = cb.server.Close()
		close(cb.closed)
	})
}

// BrowserOpener 可被测试替换（防止测试真开浏览器）。
var BrowserOpener = DefaultOpenBrowser

// DefaultOpenBrowser 按平台打开浏览器（darwin open / windows rundll32 / 其他 xdg-open）。
func DefaultOpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
