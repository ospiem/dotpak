package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mode    Mode
		verbose bool
	}{
		{"normal mode", ModeNormal, false},
		{"quiet mode", ModeQuiet, false},
		{"json mode", ModeJSON, false},
		{"verbose normal", ModeNormal, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := New(tt.mode, tt.verbose)
			if out == nil {
				t.Fatal("expected non-nil output")
			}
			if out.mode != tt.mode {
				t.Errorf("mode mismatch: got %v, want %v", out.mode, tt.mode)
			}
			if out.verbose != tt.verbose {
				t.Errorf("verbose mismatch: got %v, want %v", out.verbose, tt.verbose)
			}
		})
	}
}

func TestPrint(t *testing.T) {
	t.Parallel()

	t.Run("normal mode prints output", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeNormal, false)
		out.SetWriter(&buf)

		out.Print("Hello %s", "World")

		if !strings.Contains(buf.String(), "Hello World") {
			t.Errorf("expected 'Hello World', got %q", buf.String())
		}
	})

	t.Run("quiet mode suppresses output", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeQuiet, false)
		out.SetWriter(&buf)

		out.Print("This should not appear")

		if buf.Len() != 0 {
			t.Errorf("expected no output in quiet mode, got %q", buf.String())
		}
	})

	t.Run("json mode suppresses print output", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeJSON, false)
		out.SetWriter(&buf)

		out.Print("This should not appear")

		if buf.Len() != 0 {
			t.Errorf("expected no output in JSON mode, got %q", buf.String())
		}
	})
}

func TestPrintln(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	out := New(ModeNormal, false)
	out.SetWriter(&buf)

	out.Println("Test", "message")

	if !strings.Contains(buf.String(), "Test message") {
		t.Errorf("expected 'Test message', got %q", buf.String())
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Error("expected newline at end")
	}
}

func TestVerbose(t *testing.T) {
	t.Parallel()

	t.Run("verbose enabled shows output", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeNormal, true)
		out.SetWriter(&buf)

		out.Verbose("Debug info: %d", 42)

		if !strings.Contains(buf.String(), "Debug info: 42") {
			t.Errorf("expected verbose output, got %q", buf.String())
		}
	})

	t.Run("verbose disabled suppresses output", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeNormal, false)
		out.SetWriter(&buf)

		out.Verbose("Debug info")

		if buf.Len() != 0 {
			t.Errorf("expected no output when verbose disabled, got %q", buf.String())
		}
	})

	t.Run("verbose suppressed in quiet mode", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeQuiet, true)
		out.SetWriter(&buf)

		out.Verbose("Debug info")

		if buf.Len() != 0 {
			t.Errorf("expected no output in quiet mode, got %q", buf.String())
		}
	})
}

func TestError(t *testing.T) {
	t.Parallel()

	t.Run("error always shows in normal mode", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeNormal, false)
		out.SetErrWriter(&buf)

		out.Error("Something went wrong: %v", "disk full")

		if !strings.Contains(buf.String(), "Error:") {
			t.Error("expected 'Error:' prefix")
		}
		if !strings.Contains(buf.String(), "disk full") {
			t.Error("expected error message")
		}
	})

	t.Run("error shows in quiet mode", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeQuiet, false)
		out.SetErrWriter(&buf)

		out.Error("Critical error")

		if !strings.Contains(buf.String(), "Critical error") {
			t.Errorf("expected error in quiet mode, got %q", buf.String())
		}
	})

	t.Run("error suppressed in json mode", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeJSON, false)
		out.SetErrWriter(&buf)

		out.Error("This should not appear")

		if buf.Len() != 0 {
			t.Errorf("expected no error output in JSON mode, got %q", buf.String())
		}
	})
}

func TestWarning(t *testing.T) {
	t.Parallel()

	t.Run("warning shows in normal mode", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeNormal, false)
		out.SetWriter(&buf)

		out.Warning("Proceed with caution")

		if !strings.Contains(buf.String(), "Warning:") {
			t.Error("expected 'Warning:' prefix")
		}
	})

	t.Run("warning suppressed in quiet mode", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeQuiet, false)
		out.SetWriter(&buf)

		out.Warning("This should not appear")

		if buf.Len() != 0 {
			t.Errorf("expected no warning in quiet mode, got %q", buf.String())
		}
	})
}

func TestSuccess(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	out := New(ModeNormal, false)
	out.SetWriter(&buf)

	out.Success("Operation completed!")

	if !strings.Contains(buf.String(), "Operation completed!") {
		t.Errorf("expected success message, got %q", buf.String())
	}
}

func TestInfo(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	out := New(ModeNormal, false)
	out.SetWriter(&buf)

	out.Info("FYI: %d files", 10)

	if !strings.Contains(buf.String(), "FYI: 10 files") {
		t.Errorf("expected info message, got %q", buf.String())
	}
}

func TestProgress(t *testing.T) {
	t.Parallel()

	t.Run("shows progress in normal mode", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeNormal, false)
		out.SetWriter(&buf)

		out.Progress(5, 10, "file.txt")

		if !strings.Contains(buf.String(), "[5/10]") {
			t.Errorf("expected progress indicator, got %q", buf.String())
		}
		if !strings.Contains(buf.String(), "file.txt") {
			t.Errorf("expected file name, got %q", buf.String())
		}
	})

	t.Run("suppressed in quiet mode", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeQuiet, false)
		out.SetWriter(&buf)

		out.Progress(5, 10, "file.txt")

		if buf.Len() != 0 {
			t.Errorf("expected no progress in quiet mode, got %q", buf.String())
		}
	})

	t.Run("truncates long item names", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeNormal, false)
		out.SetWriter(&buf)

		longName := strings.Repeat("a", 100)
		out.Progress(1, 1, longName)

		// should be truncated with "..."
		if len(buf.String()) > 100 {
			t.Errorf("expected truncated output, got length %d", len(buf.String()))
		}
	})
}

func TestClearProgress(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	out := New(ModeNormal, false)
	out.SetWriter(&buf)

	out.ClearProgress()

	// should contain escape sequence for clearing line
	if !strings.Contains(buf.String(), "\r") {
		t.Error("expected carriage return for clearing")
	}
}

func TestJSON(t *testing.T) {
	t.Parallel()

	t.Run("outputs JSON in json mode", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeJSON, false)
		out.SetWriter(&buf)

		data := map[string]any{
			"success": true,
			"count":   42,
		}

		if err := out.JSON(data); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// verify it's valid JSON
		var parsed map[string]any
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Errorf("output is not valid JSON: %v", err)
		}

		if parsed["success"] != true {
			t.Error("expected success=true")
		}
	})

	t.Run("no output in normal mode", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeNormal, false)
		out.SetWriter(&buf)

		data := map[string]string{"key": "value"}
		_ = out.JSON(data)

		if buf.Len() != 0 {
			t.Errorf("expected no JSON output in normal mode, got %q", buf.String())
		}
	})

	t.Run("pretty prints JSON", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeJSON, false)
		out.SetWriter(&buf)

		data := map[string]string{"key": "value"}
		_ = out.JSON(data)

		// should have indentation
		if !strings.Contains(buf.String(), "  ") {
			t.Error("expected indented JSON")
		}
	})
}

func TestJSONCompact(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	out := New(ModeJSON, false)
	out.SetWriter(&buf)

	data := map[string]string{"key": "value"}
	_ = out.JSONCompact(data)

	// should not have indentation
	if strings.Contains(buf.String(), "  ") {
		t.Error("expected compact JSON without indentation")
	}

	// should still be valid JSON
	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v", err)
	}
}

func TestDiffOutput(t *testing.T) {
	t.Parallel()

	t.Run("Added", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeNormal, false)
		out.SetWriter(&buf)
		diff := NewDiffOutput(out)

		diff.Added("+ new file")

		if !strings.Contains(buf.String(), "+ new file") {
			t.Errorf("expected added output, got %q", buf.String())
		}
	})

	t.Run("Header", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeNormal, false)
		out.SetWriter(&buf)
		diff := NewDiffOutput(out)

		diff.Header("=== File comparison ===")

		if !strings.Contains(buf.String(), "File comparison") {
			t.Errorf("expected header output, got %q", buf.String())
		}
	})

	t.Run("suppressed in quiet mode", func(t *testing.T) {
		var buf bytes.Buffer
		out := New(ModeQuiet, false)
		out.SetWriter(&buf)
		diff := NewDiffOutput(out)

		diff.Added("+ new")
		diff.Header("header")

		if buf.Len() != 0 {
			t.Errorf("expected no diff output in quiet mode, got %q", buf.String())
		}
	})
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "short string unchanged",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "exact length unchanged",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "long string truncated",
			input:    "hello world",
			maxLen:   8,
			expected: "hello...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}
