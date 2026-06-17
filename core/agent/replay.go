package agent

// ParseLineForReplay exposes the Claude stream-json line normalizer for offline
// replay of recorded fixtures. It yields the same events the live driver lets
// escape Query: the internal block-start sentinel (which blockDedup consumes and
// drops in the live reader) is stripped here too, so a replay consumer never sees
// a driver-private event kind.
func ParseLineForReplay(line string) []AgentEvent {
	evs := parseClaudeLine(line)
	out := evs[:0]
	for _, ev := range evs {
		if ev.Kind == kindBlockStart {
			continue // internal sentinel — never leaves the live driver either
		}
		out = append(out, ev)
	}
	return out
}
