package llmassist

import "context"

type Provider interface {
	Assist(ctx context.Context, input Input) (Assistance, error)
}
