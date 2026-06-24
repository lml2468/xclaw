package trigger

// AIBroadcastPolicy governs the pure-@AI broadcast trigger path.
// Default is Deny — a follow-up message with AIs=truthy but no explicit
// re-@ no longer fires the bot. Operators who want the legacy behavior
// can set Allow, or scope it with Allowlist.
type AIBroadcastPolicy string

const (
	// AIBroadcastDeny refuses the pure-@AI path entirely.
	AIBroadcastDeny AIBroadcastPolicy = "deny"
	// AIBroadcastAllowlist accepts only channels in AIBroadcastAllowlist.
	AIBroadcastAllowlist AIBroadcastPolicy = "allowlist"
	// AIBroadcastAllow keeps the legacy behavior. Opt-in only.
	AIBroadcastAllow AIBroadcastPolicy = "allow"
)

// Valid reports whether p is a recognized constant. Invalid values fall
// back to Deny rather than panicking.
func (p AIBroadcastPolicy) Valid() bool {
	switch p {
	case AIBroadcastDeny, AIBroadcastAllowlist, AIBroadcastAllow:
		return true
	}
	return false
}

// Policy is the runtime configuration consumed by a Classifier. Pure
// value type; daemon constructs it once per bot at startup. Hot-reload
// would publish a new Policy under the connector's lock.
type Policy struct {
	BotUID  string        // bot's own server uid (matches explicit @bot and reply-to-me quotes).
	Grantor PolicyGrantor // persona-clone grantor; empty = regular bot.

	MentionFreeGroups    map[string]bool   // channel ids that respond without an @mention.
	AIBroadcast          AIBroadcastPolicy // pure-@AI policy.
	AIBroadcastAllowlist map[string]bool   // scopes AIBroadcast=allowlist.
	ReplyToBotEnabled    bool              // a quote-reply to the bot triggers; default true.
}

func (p Policy) effectiveAIBroadcast() AIBroadcastPolicy {
	if p.AIBroadcast.Valid() {
		return p.AIBroadcast
	}
	return AIBroadcastDeny
}
