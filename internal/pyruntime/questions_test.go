package pyruntime

import (
	"os"
	"strings"
	"testing"
)

// TestQuestionsParity proves the Go question list matches the
// embedded Python table exactly so the TUI cannot drift.
func TestQuestionsParity(t *testing.T) {
	// Extract the embedded sources to a temp dir.
	src, err := Extract()
	if err != nil {
		t.Fatalf("extracting sources: %v", err)
	}
	defer os.RemoveAll(src)

	// Find a supported Python interpreter.
	python, err := FindPython()
	if err != nil {
		t.Fatalf("no supported Python: %v", err)
	}

	// Parse the embedded Python QUESTIONS.
	pyQs, err := ParsePythonQuestions(python, src)
	if err != nil {
		t.Fatalf("parsing Python QUESTIONS: %v", err)
	}

	// Compare with Go's Questions.
	if len(Questions) != len(pyQs) {
		t.Fatalf("question count mismatch: Go=%d Python=%d", len(Questions), len(pyQs))
	}
	for i, gq := range Questions {
		pq := pyQs[i]
		if gq.Key != pq.Key {
			t.Errorf("question %d: key mismatch Go=%q Python=%q", i, gq.Key, pq.Key)
		}
		if gq.Question != pq.Question {
			t.Errorf("question %d (%s): question mismatch", i, gq.Key)
		}
		if gq.Help != pq.Help {
			t.Errorf("question %d (%s): help mismatch", i, gq.Key)
		}
	}
	// Also verify that we can find all keys by key (order might differ)
	pyByKey := map[string]pythonQuestion{}
	for _, pq := range pyQs {
		pyByKey[pq.Key] = pq
	}
	for _, gq := range Questions {
		pq, ok := pyByKey[gq.Key]
		if !ok {
			t.Errorf("Go question %q not in Python table", gq.Key)
			continue
		}
		if gq.Key != pq.Key || gq.Question != pq.Question || gq.Help != pq.Help {
			t.Errorf("question %q differs: Go=%+v Python=%+v", gq.Key, gq, pq)
		}
	}
}

// TestQuestionsNonEmpty asserts the table is not empty.
func TestQuestionsNonEmpty(t *testing.T) {
	if len(Questions) == 0 {
		t.Fatal("Questions must not be empty")
	}
	for _, q := range Questions {
		if strings.TrimSpace(q.Key) == "" {
			t.Errorf("empty key")
		}
		if strings.TrimSpace(q.Question) == "" {
			t.Errorf("empty question for %q", q.Key)
		}
	}
}
