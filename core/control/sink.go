package control

import "github.com/lml2468/xclaw/core/agent"

// EventSink adapts the control Server to gateway.Sink: it projects normalized
// AgentEvents onto the control-bus event vocabulary (proto/README.md) and
// broadcasts them to all connected clients. This is the join point between the
// agent-driving core and the GUI control plane.
type EventSink struct {
	srv *Server
}

// NewEventSink wraps a Server as a gateway.Sink.
func NewEventSink(srv *Server) *EventSink { return &EventSink{srv: srv} }

// OnEvent projects one AgentEvent to its control-bus event.
func (s *EventSink) OnEvent(sessionKey string, ev agent.AgentEvent) {
	switch ev.Kind {
	case agent.KindSessionStarted:
		s.srv.Broadcast("session.activity", map[string]string{
			"sessionKey": sessionKey, "kind": "turnStart",
		})
	case agent.KindTextDelta:
		s.srv.Broadcast("session.text", SessionTextBody{SessionKey: sessionKey, Delta: ev.Text})
	case agent.KindThinking:
		s.srv.Broadcast("session.activity", map[string]string{
			"sessionKey": sessionKey, "kind": "thinking",
		})
	case agent.KindToolUse:
		s.srv.Broadcast("session.tool", SessionToolBody{
			SessionKey: sessionKey, Name: ev.ToolName, Params: ev.ToolParams,
		})
	case agent.KindToolResult:
		s.srv.Broadcast("session.activity", map[string]string{
			"sessionKey": sessionKey, "kind": "toolResult",
		})
	case agent.KindTurnDone:
		if ev.Usage != nil {
			s.srv.Broadcast("session.usage", SessionUsageBody{
				SessionKey:   sessionKey,
				InputTokens:  ev.Usage.InputTokens,
				OutputTokens: ev.Usage.OutputTokens,
			})
		}
		s.srv.Broadcast("session.activity", map[string]string{
			"sessionKey": sessionKey, "kind": "turnDone",
		})
	case agent.KindError:
		s.srv.Broadcast("error", ErrorBody{
			Scope: "agent", Message: ev.Err, Recoverable: ev.Recoverable,
		})
	}
}

// OnReply broadcasts the assembled assistant reply for a completed turn.
func (s *EventSink) OnReply(sessionKey string, text string) {
	s.srv.Broadcast("session.reply", SessionReplyBody{SessionKey: sessionKey, Text: text})
}
