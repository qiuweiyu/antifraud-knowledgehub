package llmassist

import "github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/riskengine"

type Status string

const (
	StatusAvailable   Status = "available"
	StatusUnavailable Status = "unavailable"
)

type Assistance struct {
	Summary      string   `json:"summary"`
	Observations []string `json:"observations"`
	Limitations  []string `json:"limitations"`
}

type Input struct {
	Text       string
	RuleResult riskengine.Result
}

type Outcome struct {
	Status     Status     `json:"status"`
	Assistance Assistance `json:"assistance,omitempty"`
}
