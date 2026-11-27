package botmgmt

// Cloud-agent task kinds a bot message may route to. These mirror
// cloudagent.KindSession / KindCoding; they are redeclared here because
// cloudagent already imports botmgmt (for SettingsKey) and importing it back
// would create a cycle. The strings must stay identical — cloudagent.Create
// validates them and they are persisted in cloud_agents.kind.
const (
	KindSession = "session"
	KindCoding  = "coding"
)
