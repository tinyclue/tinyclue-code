package types

import "time"

type SystemMessage struct {
	Role      MessageRole `json:"role"`
	Text      string      `json:"text"`
	CreatedAt time.Time   `json:"createdAt"`
}

func (SystemMessage) RoleName() MessageRole { return SystemRole }

func NewSystemMessage(text string) SystemMessage {
	return SystemMessage{
		Role:      SystemRole,
		Text:      text,
		CreatedAt: time.Now(),
	}
}
