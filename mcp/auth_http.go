// auth_http.go：http 请求头注入（headerRoundTripper）。跨平台浏览器打开器已迁至 oauth 包
// （oauth.BrowserOpener），mcp 交互授权直接引用。
package mcp

import (
	"net/http"
)

// headerRoundTripper 在请求上附加 .mcp.json 配置的 headers；已存在的 Authorization
// （OAuth Bearer，由 transport 的 OAuthHandler 设置）不被覆盖。
type headerRoundTripper struct {
	headers map[string]string
	base    http.RoundTripper
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	for k, v := range rt.headers {
		if k == "Authorization" && r.Header.Get("Authorization") != "" {
			continue
		}
		r.Header.Set(k, v)
	}
	return rt.base.RoundTrip(r)
}

// httpClientFor 为 http server 构造 HTTP 客户端：配置了 headers 时注入自定义 RoundTripper，
// 否则返回 nil（go-sdk transport 回落 http.DefaultClient）。
func httpClientFor(headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return nil
	}
	return &http.Client{Transport: &headerRoundTripper{headers: headers, base: http.DefaultTransport}}
}
