package main

type toolOutcome int

const (
	toolOutcomeUncertain toolOutcome = iota
	toolOutcomeSucceeded
	toolOutcomeFailed
)

func (o toolOutcome) String() string {
	switch o {
	case toolOutcomeSucceeded:
		return "succeeded"
	case toolOutcomeFailed:
		return "failed"
	default:
		return "uncertain"
	}
}

func normalizeToolOutcome(value string) toolOutcome {
	switch value {
	case toolOutcomeSucceeded.String():
		return toolOutcomeSucceeded
	case toolOutcomeFailed.String():
		return toolOutcomeFailed
	case toolOutcomeUncertain.String(), "":
		return toolOutcomeUncertain
	default:
		return toolOutcomeUncertain
	}
}
