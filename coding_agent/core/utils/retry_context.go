package utils

import (
	protocol "github.com/tinyclue/tinyclue-code/api_provider/types"
	"regexp"

	"github.com/tinyclue/tinyclue-code/api_provider/types"
)

// retryableErrorPattern matches error messages that indicate transient failures
// suitable for automatic retry: overloaded, rate limited, server errors, network
// issues, timeouts, and connection problems.
var retryableErrorPattern = regexp.MustCompile(`(?i)overloaded|provider.?returned.?error|rate.?limit|too many requests|429|500|502|503|504|529|service.?unavailable|server.?error|internal.?error|network.?error|connection.?error|connection.?refused|connection.?lost|websocket.?closed|websocket.?error|other side closed|fetch failed|upstream.?connect|reset before headers|socket hang up|ended without|stream ended before message_stop|http2 request did not get a response|timed? out|timeout|terminated|retry delay`)

// nonRetryableLimitPattern matches error messages that indicate hard provider
// limits (quota, billing, etc.) which cannot be resolved by retrying.
var nonRetryableLimitPattern = regexp.MustCompile(`(?i)GoUsageLimitError|FreeUsageLimitError|Monthly usage limit reached|available balance|insufficient_quota|out of budget|quota exceeded|billing|insufficient_credits|credit_limit_reached|Insufficient Balance`)

// checks whether an assistant message error can be automatically retried.
//
// Returns false when:
//   - The message is not an error (stop reason isn't FinishReasonError)
//   - The error message is empty
//   - The error is a context overflow (handled by compaction instead)
//   - The error is a non-retryable provider limit (quota, billing)
//
// Returns true when the error matches known transient failure patterns:
// overloaded, rate limited, server errors (5xx), network/connection errors, timeouts.
func IsRetryableError(finalResp *types.ChatResponse, contextWindow int) bool {
	if finalResp.Error.IsSuccess {
		return false
	}
	code := finalResp.Error.Code
	//10000以上的自定义的内部可重试错误，直接返回可以重试
	if protocol.IsRetryableCode(code) {
		return true
	}
	//如果不是http错误码，不需要判断，直接返回不可重试
	//如果是http错误码，则继续判断，是否可以进行重试
	if !protocol.IsHttpStatusCode(code) {
		return false
	}
	//如果是上下文超了，直接返回不可重试
	if IsContextOverflow(finalResp, contextWindow) {
		return false
	}

	errMsg := finalResp.Error.RawMessage
	// Hard provider limits are not retryable (quota, billing)
	// 如果是不需要重试的错误，直接返回不可重试
	if errMsg != "" {
		if nonRetryableLimitPattern.MatchString(errMsg) {
			return false
		}
	}
	//继续匹配其余errMsg是否可以进行重试
	if errMsg != "" {
		// body 有内容 → 靠模式匹配决定
		if retryableErrorPattern.MatchString(errMsg) {
			return true
		}
	}
	// body 为空 → HTTP code 兜底
	if code == 429 || (code >= 500 && code <= 599) {
		return true
	}
	return false
}
