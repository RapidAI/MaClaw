package main

type agentLoopStage string

const (
	agentStageOrient   agentLoopStage = "orient"
	agentStageExecute  agentLoopStage = "execute"
	agentStageRecover  agentLoopStage = "recover"
	agentStageConverge agentLoopStage = "converge"
	agentStageFinalize agentLoopStage = "finalize"
)

func (s agentLoopStage) String() string {
	return string(s)
}
