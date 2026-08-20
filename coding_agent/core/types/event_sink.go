package types

type EventSink func(eventType AgentEventType, message interface{}, err error)

func (fn EventSink) Emit(eventType AgentEventType, message interface{}, err error) {
	if fn != nil {
		fn(eventType, message, err)
	}
}
