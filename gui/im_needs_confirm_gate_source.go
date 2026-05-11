package main

type needsConfirmGateSource string

const (
	needsConfirmGateSourceWorkflow needsConfirmGateSource = "workflow"
	needsConfirmGateSourceSteering needsConfirmGateSource = "steering"
)

func (s needsConfirmGateSource) String() string {
	return string(s)
}
