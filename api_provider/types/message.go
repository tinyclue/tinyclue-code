package types

// Message 是对话消息的通用接口。
type Message interface {
	RoleName() MessageRole
}
