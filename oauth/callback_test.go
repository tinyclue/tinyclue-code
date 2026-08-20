package oauth

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestCallbackServerDoubleHitDoesNotBlock 验证回调重复命中（channel 满、无人消费）时不永久阻塞：
// Close 关闭 closed 通道后，第二个 ServeHTTP 经 select 兜底返回，handler goroutine 不泄漏。
func TestCallbackServerDoubleHitDoesNotBlock(t *testing.T) {
	cb, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer cb.Close()

	u := cb.URL() + "?code=c&state=s"
	// 第一次命中：填满 resultCh（缓冲 1）。
	cb.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", u, nil))

	// 第二次命中：channel 满，不消费 → 旧实现永久阻塞在发送；select + closed 兜底后随 Close 返回。
	done := make(chan struct{})
	go func() {
		cb.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", u, nil))
		close(done)
	}()
	cb.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second callback should not block after Close")
	}
}
