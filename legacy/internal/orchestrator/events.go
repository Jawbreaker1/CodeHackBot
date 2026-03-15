package orchestrator

func (m *Manager) EmitEvent(runID, workerID, taskID, eventType string, payload map[string]any) error {
	seq, err := m.nextSeq(runID, workerID)
	if err != nil {
		return err
	}
	return AppendEventJSONL(m.eventPath(runID), EventEnvelope{
		EventID:  NewEventID(),
		RunID:    runID,
		WorkerID: workerID,
		TaskID:   taskID,
		Seq:      seq,
		TS:       m.Now(),
		Type:     eventType,
		Payload:  mustJSONRaw(payload),
	})
}

func (m *Manager) EmitProgress(runID, workerID, taskID, message string) error {
	return m.EmitEvent(runID, workerID, taskID, EventTypeTaskProgress, map[string]any{
		"message": message,
		"at":      m.Now(),
	})
}
