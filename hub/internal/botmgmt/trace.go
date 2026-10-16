package botmgmt

import (
	"strconv"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/botlog"
)

// traceBot appends one step of this bot's pipeline to its own log file.
func traceBot(botID, stage string, err error, kv ...string) {
	botlog.Write(botID, "hub."+stage, err, kv...)
}

// traceBotOnce is traceBot, except the same bot, stage, and error stay
// once per repeat window. A run that is still going must not fill the file
// on every poll.
func traceBotOnce(botID, stage string, err error, kv ...string) {
	botlog.WriteOnce(botID, "hub."+stage, err, kv...)
}

func traceMS(d time.Duration) string {
	return strconv.FormatInt(d.Milliseconds(), 10)
}

func traceYes(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}

func botlogHost(raw string) string {
	return botlog.Host(raw)
}
