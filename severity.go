package repomap

import "strings"

type Severity string

const (
	Critical Severity = "critical"
	High     Severity = "high"
	Medium   Severity = "medium"
	Low      Severity = "low"
	Info     Severity = "info"
)

func (s Severity) Value() int {
	switch s {
	case Critical:
		return 5
	case High:
		return 4
	case Medium:
		return 3
	case Low:
		return 2
	case Info:
		return 1
	default:
		return 0
	}
}

func (s Severity) String() string {
	return string(s)
}

func ParseSeverity(s string) Severity {
	switch strings.ToLower(s) {
	case "critical":
		return Critical
	case "high":
		return High
	case "medium":
		return Medium
	case "low":
		return Low
	case "info":
		return Info
	default:
		return Medium
	}
}

func MaxSeverity(a, b Severity) Severity {
	if a.Value() > b.Value() {
		return a
	}
	return b
}

func MaxSeverities(severities []Severity) Severity {
	if len(severities) == 0 {
		return Medium
	}

	max := severities[0]
	for _, s := range severities[1:] {
		if s.Value() > max.Value() {
			max = s
		}
	}
	return max
}

type SeverityDistribution struct {
	Critical int `json:"critical,omitempty"`
	High     int `json:"high,omitempty"`
	Medium   int `json:"medium,omitempty"`
	Low      int `json:"low,omitempty"`
	Info     int `json:"info,omitempty"`
}

func (sd *SeverityDistribution) Add(s Severity) {
	switch s {
	case Critical:
		sd.Critical++
	case High:
		sd.High++
	case Medium:
		sd.Medium++
	case Low:
		sd.Low++
	case Info:
		sd.Info++
	}
}

func (sd *SeverityDistribution) Total() int {
	return sd.Critical + sd.High + sd.Medium + sd.Low + sd.Info
}

func (sd *SeverityDistribution) Max() Severity {
	switch {
	case sd.Critical > 0:
		return Critical
	case sd.High > 0:
		return High
	case sd.Medium > 0:
		return Medium
	case sd.Low > 0:
		return Low
	case sd.Info > 0:
		return Info
	default:
		return Medium
	}
}
