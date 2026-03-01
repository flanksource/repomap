package repomap

import "testing"

func TestSeverityValue(t *testing.T) {
	tests := []struct {
		severity Severity
		expected int
	}{
		{Critical, 5},
		{High, 4},
		{Medium, 3},
		{Low, 2},
		{Info, 1},
		{Severity("unknown"), 0},
	}

	for _, tt := range tests {
		if got := tt.severity.Value(); got != tt.expected {
			t.Errorf("Severity(%q).Value() = %d, want %d", tt.severity, got, tt.expected)
		}
	}
}

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		input    string
		expected Severity
	}{
		{"critical", Critical},
		{"CRITICAL", Critical},
		{"High", High},
		{"medium", Medium},
		{"low", Low},
		{"info", Info},
		{"garbage", Medium},
	}

	for _, tt := range tests {
		if got := ParseSeverity(tt.input); got != tt.expected {
			t.Errorf("ParseSeverity(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestMaxSeverity(t *testing.T) {
	if got := MaxSeverity(High, Low); got != High {
		t.Errorf("MaxSeverity(High, Low) = %q, want High", got)
	}
	if got := MaxSeverity(Info, Critical); got != Critical {
		t.Errorf("MaxSeverity(Info, Critical) = %q, want Critical", got)
	}
}

func TestMaxSeverities(t *testing.T) {
	if got := MaxSeverities([]Severity{Low, High, Medium}); got != High {
		t.Errorf("MaxSeverities([Low, High, Medium]) = %q, want High", got)
	}
	if got := MaxSeverities(nil); got != Medium {
		t.Errorf("MaxSeverities(nil) = %q, want Medium", got)
	}
}

func TestSeverityDistribution(t *testing.T) {
	sd := &SeverityDistribution{}
	sd.Add(Critical)
	sd.Add(High)
	sd.Add(High)
	sd.Add(Low)

	if sd.Total() != 4 {
		t.Errorf("Total() = %d, want 4", sd.Total())
	}
	if sd.Max() != Critical {
		t.Errorf("Max() = %q, want Critical", sd.Max())
	}
	if sd.Critical != 1 || sd.High != 2 || sd.Low != 1 {
		t.Errorf("Distribution = {C:%d H:%d L:%d}, want {C:1 H:2 L:1}", sd.Critical, sd.High, sd.Low)
	}
}
