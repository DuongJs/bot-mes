package table

import (
	"testing"
)

func TestNonNilFields(t *testing.T) {
	// Create an LSTable with some non-nil fields
	table := &LSTable{
		LSMciTraceLog: []*LSMciTraceLog{{}},
		LSExecuteFirstBlockForSyncTransaction: []*LSExecuteFirstBlockForSyncTransaction{{}},
		LSSetHMPSStatus:                       []*LSSetHMPSStatus{{}},
	}

	fields := table.NonNilFields()
	expected := []string{
		"LSMciTraceLog",
		"LSExecuteFirstBlockForSyncTransaction",
		"LSSetHMPSStatus",
	}

	// The order might depend on reflection order (usually struct definition order),
    // but let's sort to be safe for comparison if needed,
    // although the original implementation iterates over VisibleFields which usually follows definition order.
    // The optimized implementation will also iterate in definition order.

    // Let's just check if all expected fields are present and no others.
	if len(fields) != len(expected) {
		t.Errorf("Expected %d fields, got %d", len(expected), len(fields))
	}

	expectedMap := make(map[string]bool)
	for _, f := range expected {
		expectedMap[f] = true
	}

	for _, f := range fields {
		if !expectedMap[f] {
			t.Errorf("Unexpected field: %s", f)
		}
	}
}

func BenchmarkNonNilFields(b *testing.B) {
	table := &LSTable{
		LSMciTraceLog: []*LSMciTraceLog{{}},
		LSExecuteFirstBlockForSyncTransaction: []*LSExecuteFirstBlockForSyncTransaction{{}},
		LSSetHMPSStatus:                       []*LSSetHMPSStatus{{}},
		LSTruncateMetadataThreads:             []*LSTruncateMetadataThreads{{}},
		LSUpsertSyncGroupThreadsRange:         []*LSUpsertSyncGroupThreadsRange{{}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = table.NonNilFields()
	}
}
