package dashboard

import (
	"encoding/json"
)

// LogWriter implements io.Writer for zerolog, parsing JSON log entries and
// adding them to the dashboard LogBuffer so they appear in the UI.
type LogWriter struct {
	Buffer *LogBuffer
}

// Write parses a zerolog JSON log line and stores it in the LogBuffer.
func (w *LogWriter) Write(p []byte) (n int, err error) {
	var entry struct {
		Level   string `json:"level"`
		Message string `json:"message"`
		Time    string `json:"time"`
	}
	if err := json.Unmarshal(p, &entry); err != nil {
		// If we can't parse as JSON, store the raw text as an info log.
		w.Buffer.Add("info", string(p))
		return len(p), nil
	}
	if entry.Level == "" {
		entry.Level = "info"
	}
	w.Buffer.Add(entry.Level, entry.Message)
	return len(p), nil
}
