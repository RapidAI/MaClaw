package guiapp

// darwinTapPlan is the macOS 14.2 process-tap contract. companionStartProcessTap
// applies it. The values live in ordinary Go so a Windows test can execute them.
type darwinTapPlan struct {
	InitSelector      string
	TranslatePID      uint32
	ProcessPID        uint32
	ProcessList       uint32
	Exclusive         bool
	RefuseWhenEmpty   bool
	AllowUnixPID      bool
	ForbiddenSelector string
}

// darwinProcessTapPlan returns the initializer and process-object rules for one
// include-list tap. Processes are HAL AudioObjectIDs, not Unix PIDs.
func darwinProcessTapPlan() darwinTapPlan {
	return darwinTapPlan{
		InitSelector:      "initMonoMixdownOfProcesses:",
		TranslatePID:      0x69643270, // 'id2p'
		ProcessPID:        0x70706964, // 'ppid'
		ProcessList:       0x70727323, // 'prs#'
		Exclusive:         false,
		RefuseWhenEmpty:   true,
		AllowUnixPID:      false,
		ForbiddenSelector: "initStereoGlobalTapButExcludeProcesses",
	}
}
