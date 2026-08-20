package types

import (
	"encoding/json"
	"fmt"
	"time"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
)

// SessionEntry 会话条目接口。
type SessionEntry interface {
	Kind() string
	GetId() string
	GetParentId() string
}

// SessionEntryBase 会话条目基类。
type SessionEntryBase struct {
	Type      EntryType `json:"type"`
	Id        string    `json:"id"`
	ParentId  string    `json:"parentId,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

func (s SessionEntryBase) Kind() string        { return string(s.Type) }
func (s SessionEntryBase) GetId() string       { return s.Id }
func (s SessionEntryBase) GetParentId() string { return s.ParentId }

// HeaderEntry 会话头条目。
type HeaderEntry struct {
	SessionEntryBase
	SessionId string `json:"sessionId"`
	Cwd       string `json:"cwd"`
}

// MessageEntry 消息条目。
type MessageEntry struct {
	SessionEntryBase
	Message apitypes.Message `json:"message"`
	Error   apitypes.Error   `json:"-"` // 同上
}

// MarshalJSON 实现 json.Marshaler。只有 assistant 角色的消息才输出
// 只有 assistant 角色的消息才输出 error 字段，其余角色跳过以减少冗余。
func (e *MessageEntry) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"type":      e.Type,
		"id":        e.Id,
		"parentId":  e.ParentId,
		"timestamp": e.Timestamp,
		"message":   e.Message,
	}
	if e.Message != nil && e.Message.RoleName() == apitypes.AssistantRole {
		m["error"] = e.Error
	}
	return json.Marshal(m)
}

// CompactionEntry 压缩条目。
type CompactionEntry struct {
	SessionEntryBase
	Summary          string   `json:"summary"`
	FirstKeptEntryId string   `json:"firstKeptEntryId"`
	Tokens           int      `json:"tokens"`
	ReadFiles        []string `json:"readFiles"`
	ModifyFiles      []string `json:"modifyFiles"`
	DiscoveredTools  []string `json:"discoveredTools"`
	Mode             int      `json:"mode"`
}

// SubAgentEntry 子 agent 结果条目，用于持久化子 agent 花费。
type SubAgentEntry struct {
	SessionEntryBase
	Content SubAgentContent `json:"content"`
}

// PlanEntry 记录一次 plan mode 状态变化（进入/退出），供会话恢复时重建 PlanState。
type PlanEntry struct {
	SessionEntryBase
	PlanState PlanState `json:"planState"`
}

// EntryType 会话条目类型。
type EntryType string

const (
	EntryTypeSession    EntryType = "session"
	EntryTypeMessage    EntryType = "message"
	EntryTypeCompaction EntryType = "compaction"
	EntryTypeSubAgent   EntryType = "sub_agent"
	EntryTypePlan       EntryType = "plan"
)

// MarshalSessionEntry 序列化 SessionEntry 为 JSON。
func MarshalSessionEntry(entry SessionEntry) ([]byte, error) {
	return json.Marshal(entry)
}

// UnmarshalSessionEntry 从 JSON 反序列化 SessionEntry，按 type 字段分派。
func UnmarshalSessionEntry(data []byte) (SessionEntry, error) {
	var t struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	switch t.Type {
	case string(EntryTypeSession):
		var e HeaderEntry
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, err
		}
		return &e, nil
	case string(EntryTypeMessage):
		return unmarshalMessageEntry(data)
	case string(EntryTypeCompaction):
		var e CompactionEntry
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, err
		}
		return &e, nil
	case string(EntryTypeSubAgent):
		var e SubAgentEntry
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, err
		}
		return &e, nil
	case string(EntryTypePlan):
		var e PlanEntry
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, err
		}
		return &e, nil
	default:
		return nil, fmt.Errorf("unknown session entry type: %s", t.Type)
	}
}

// unmarshalMessageEntry 反序列化 MessageEntry，处理 Message 接口分派。
func unmarshalMessageEntry(data []byte) (*MessageEntry, error) {
	var raw struct {
		Id        string          `json:"id"`
		ParentId  string          `json:"parentId"`
		Timestamp time.Time       `json:"timestamp"`
		Message   json.RawMessage `json:"message"`
		Err       json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	msg, err := unmarshalMessage(raw.Message)
	if err != nil {
		return nil, err
	}
	var errVal apitypes.Error
	if raw.Err != nil {
		json.Unmarshal(raw.Err, &errVal)
	}
	return &MessageEntry{
		SessionEntryBase: SessionEntryBase{
			Type:      EntryTypeMessage,
			Id:        raw.Id,
			ParentId:  raw.ParentId,
			Timestamp: raw.Timestamp,
		},
		Message: msg,
		Error:   errVal,
	}, nil
}

// unmarshalMessage 按 role 字段反序列化 apitypes.Message。
func unmarshalMessage(data []byte) (apitypes.Message, error) {
	var role struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(data, &role); err != nil {
		return nil, err
	}
	switch role.Role {
	case string(apitypes.UserRole):
		var m apitypes.UserMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	case string(apitypes.AssistantRole):
		var m apitypes.AssistantMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	case string(apitypes.SystemRole):
		var m apitypes.SystemMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	case string(apitypes.ToolRole):
		var m apitypes.ToolResultMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	default:
		return nil, fmt.Errorf("unknown message role: %s", role.Role)
	}
}

type AgentMessage struct {
	Message  apitypes.Message
	Response *apitypes.ChatResponse
}

// SessionUsage 累加整个会话中 assistant 消息的用量数据。
type SessionUsage struct {
	Input       int
	Output      int
	CacheRead   int
	CacheWrite  int
	TotalTokens int
	CostTotal   float64

	ContextUsage   int     // 当前上下文已用 token（估算）
	ContextWindow  int     // 模型上下文窗口上限
	ContextPercent float64 // 上下文占用百分比，保留 2 位小数
}
