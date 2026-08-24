package main

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
)

func (h *IMMessageHandler) runHorizonCodingEpisode(ctx context.Context, sess *horizonSession, ep longhorizon.EpisodeContext) string {
	return h.runHorizonEpisode(ctx, sess, ep)
}

func splitHorizonLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
