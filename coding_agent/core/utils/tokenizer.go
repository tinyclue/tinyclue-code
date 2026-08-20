package utils

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"sync"

	"github.com/pkoukk/tiktoken-go"

	"github.com/tinyclue/tinyclue-code/api_provider/types"
)

var (
	tkeOnce sync.Once
	tke     *tiktoken.Tiktoken
	tkeErr  error
)

func getTiktoken() (*tiktoken.Tiktoken, error) {
	tkeOnce.Do(func() {
		tke, tkeErr = tiktoken.GetEncoding("cl100k_base")
	})
	return tke, tkeErr
}

// InitTiktoken 预初始化 tiktoken 编码器（cl100k_base）。
// 在启动时调用，避免首次用户消息触发 BPE 文件下载导致的延迟。
// 初始化失败会自动回退到 len/4 估算，不影响后续使用。
func InitTiktoken() {
	getTiktoken()
}

// EstimateTextTokens estimates the token count of a plain text string using
// tiktoken (cl100k_base). Falls back to len/4 heuristic on error.
func EstimateTextTokens(text string) int {
	enc, err := getTiktoken()
	if err != nil {
		return int(math.Ceil(float64(len(text)) / 4))
	}
	return len(enc.Encode(text, nil, nil))
}

// EstimateImageTokens estimates the token count of a base64-encoded image
// using Anthropic's formula: tokens = width * height / 750.
// Returns 0 if the image cannot be decoded.
func EstimateImageTokens(base64Str string) int {
	data, err := base64.StdEncoding.DecodeString(base64Str)
	if err != nil {
		return 0
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0
	}
	return int(math.Ceil(float64(cfg.Width*cfg.Height) / 750))
}

// EstimateTokens estimates the token count of a message using tiktoken.
// Falls back to len/4 heuristic on error.
func EstimateTokens(msg types.Message) int {
	switch v := msg.(type) {
	case types.UserMessage:
		return EstimateTextTokens(v.Text)

	case types.AssistantMessage:
		total := EstimateTextTokens(v.TextContent.Text) +
			EstimateTextTokens(v.ThinkingContent.Thinking)
		for _, tc := range v.ToolCalls {
			total += EstimateTextTokens(tc.Name)
			if tc.Arguments != nil {
				b, _ := json.Marshal(tc.Arguments)
				total += EstimateTextTokens(string(b))
			}
		}
		return total

	case types.SystemMessage:
		return EstimateTextTokens(v.Text)

	case types.ToolResultMessage:
		total := 0
		for _, c := range v.Contents {
			switch c.Type {
			case "text":
				total += EstimateTextTokens(c.Text)
			case "image":
				total += EstimateImageTokens(c.ImageData)
			}
		}
		return total

	default:
		return 0
	}
}
