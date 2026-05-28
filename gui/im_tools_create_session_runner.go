package main

func (h *IMMessageHandler) runCreateSessionTool(args map[string]interface{}) string {
	return "[system rejected] create_session is disabled. Coding tasks must run through the internal CodingSubAgent, not external coding sessions."
}
