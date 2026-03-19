package agent

import "testing"

func TestStripThinkingTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no tags", "hello world", "hello world"},
		{"single block", "<think>reasoning</think>answer", "answer"},
		{"multiline block", "<think>\nstep1\nstep2\n</think>answer", "answer"},
		{"block with surrounding text", "prefix\n<think>hidden</think>\nsuffix", "prefix\n\nsuffix"},
		{"multiple blocks", "<think>a</think>result<think>b</think>", "result"},
		{"uppercase tag", "<THINK>hidden</THINK>answer", "answer"},
		{"mixed case tag", "<Think>hidden</Think>answer", "answer"},
		{"empty string", "", ""},
		{"only tag", "<think>hidden</think>", ""},
		{"whitespace trimmed", "  <think>x</think>  answer  ", "answer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripThinkingTags(tt.input); got != tt.want {
				t.Errorf("stripThinkingTags(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseThinkingLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  ThinkingLevel
	}{
		{"off", "off", ThinkingOff},
		{"empty", "", ThinkingOff},
		{"low", "low", ThinkingLow},
		{"medium", "medium", ThinkingMedium},
		{"high", "high", ThinkingHigh},
		{"xhigh", "xhigh", ThinkingXHigh},
		{"adaptive", "adaptive", ThinkingAdaptive},
		{"unknown", "unknown", ThinkingOff},
		// Case-insensitive and whitespace-tolerant
		{"upper_Medium", "Medium", ThinkingMedium},
		{"upper_HIGH", "HIGH", ThinkingHigh},
		{"mixed_Adaptive", "Adaptive", ThinkingAdaptive},
		{"leading_space", " high", ThinkingHigh},
		{"trailing_space", "low ", ThinkingLow},
		{"both_spaces", " medium ", ThinkingMedium},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseThinkingLevel(tt.input); got != tt.want {
				t.Errorf("parseThinkingLevel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
