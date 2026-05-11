package main

type taskIntentSource string

const (
	taskIntentSourceUIC                 taskIntentSource = "uic"
	taskIntentSourceLLM                 taskIntentSource = "llm"
	taskIntentSourceSemanticUnavailable taskIntentSource = "semantic-unavailable"
)

func (source taskIntentSource) String() string {
	return string(source)
}
