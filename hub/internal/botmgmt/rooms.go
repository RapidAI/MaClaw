package botmgmt

import "strings"

// BotRoom is a group conversation between the person and some of their
// bots. Attention per member: "all" (wake on every message), "mentions"
// (only when @named; the default for sub-bots) or "off".
type BotRoom struct {
	ID        string            `json:"id"`
	Owner     string            `json:"owner"`
	Name      string            `json:"name"`
	Members   []string          `json:"members"`
	Attention map[string]string `json:"attention,omitempty"`
	CreatedAt string            `json:"created_at"`
}

func (r *BotRoom) hasMember(bot string) bool {
	for _, m := range r.Members {
		if m == bot {
			return true
		}
	}
	return false
}

func roomOf(rec record, userID, id string) *BotRoom {
	id = strings.TrimPrefix(strings.TrimSpace(id), "room:")
	if r := rec.Rooms[id]; r != nil && r.Owner == userID {
		return r
	}
	return nil
}
