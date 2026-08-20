package types

import "time"

type UserMessage struct {
	Role      MessageRole `json:"role"`
	Text      string      `json:"text"`
	CreatedAt time.Time   `json:"createdAt"`
	IsMeta    bool
}

func (UserMessage) RoleName() MessageRole { return UserRole }

func NewUserMessage(text string) UserMessage {
	return UserMessage{
		Role:      UserRole,
		Text:      text,
		CreatedAt: time.Now(),
	}
}

func NewUserMetaMessage(text string) UserMessage {
	return UserMessage{
		Role:      UserRole,
		Text:      text,
		CreatedAt: time.Now(),
		IsMeta:    true,
	}
}
