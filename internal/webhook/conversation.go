package webhook

// ConvState represents a step in the /addbot multi-step conversation FSM.
type ConvState string

const (
	ConvStateIdle       ConvState = "IDLE"
	ConvStateAwaitToken ConvState = "AWAIT_TOKEN"
	ConvStateAwaitName  ConvState = "AWAIT_NAME"
	ConvStateDone       ConvState = "DONE"
)

// ConversationData holds the accumulated FSM state for one admin user.
type ConversationData struct {
	State ConvState `json:"state"`
	Token string    `json:"token"`
	Name  string    `json:"name"`
}
