package orchestrator

func capBytes(data []byte, max int) []byte {
	if max <= 0 || len(data) <= max {
		return data
	}
	out := make([]byte, max)
	copy(out, data[:max])
	return out
}
