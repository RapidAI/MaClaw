package guiapp

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Multi-bot collaboration views: what bots said to the person, group
// rooms, and the assignment board. All read from / write to the Hub.

// BotCollabFeatures says which collaboration parts the Hub has on.
type BotCollabFeatures struct {
	Assignments bool `json:"assignments"`
	PeerReview  bool `json:"peer_review"`
	Say         bool `json:"say"`
	Rooms       bool `json:"rooms"`
}

// BotSaidItem is one message a bot said (kind say) or the person wrote in
// a room (kind room, from "human").
type BotSaidItem struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	Target    string `json:"target"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

// BotAssignment is one delegated task as the Hub reads it.
type BotAssignment struct {
	ID           string   `json:"id"`
	OwnerBot     string   `json:"owner_bot"`
	TargetBot    string   `json:"target_bot"`
	Task         string   `json:"task"`
	State        string   `json:"state"`
	ParentID     string   `json:"parent_id,omitempty"`
	CreatedAt    string   `json:"created_at"`
	LastReport   string   `json:"last_report,omitempty"`
	Artifacts    []string `json:"artifacts,omitempty"`
	OpenQuestion string   `json:"open_question,omitempty"`
	Review       string   `json:"review,omitempty"`
	ReviewNote   string   `json:"review_note,omitempty"`
	Reviewer     string   `json:"reviewer,omitempty"`
}

// BotRoom is a group room between the person and some bots.
type BotRoom struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Members   []string          `json:"members"`
	Attention map[string]string `json:"attention,omitempty"`
	CreatedAt string            `json:"created_at"`
}

// BotRoomPost is the stored room message and the bots woken by it.
type BotRoomPost struct {
	Message BotSaidItem `json:"message"`
	Woken   []string    `json:"woken"`
}

const botCollabWait = 20 * time.Second

func (a *App) GetBotCollabFeatures() (*BotCollabFeatures, error) {
	var out BotCollabFeatures
	if err := a.desktopBotCall(context.Background(), http.MethodGet, "/api/v1/bots/collab-features", nil, &out, botCollabWait); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListBotSaid lists what bots told the person (room empty) or a room's talk.
func (a *App) ListBotSaid(room string) ([]BotSaidItem, error) {
	var out struct {
		Items []BotSaidItem `json:"items"`
	}
	path := "/api/v1/bots/said"
	if room = strings.TrimSpace(room); room != "" {
		path += "?room=" + url.QueryEscape(room)
	}
	if err := a.desktopBotCall(context.Background(), http.MethodGet, path, nil, &out, botCollabWait); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (a *App) ListBotAssignments() ([]BotAssignment, error) {
	var out struct {
		Items []BotAssignment `json:"items"`
	}
	if err := a.desktopBotCall(context.Background(), http.MethodGet, "/api/v1/bots/assignments", nil, &out, botCollabWait); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (a *App) ListBotRooms() ([]BotRoom, error) {
	var out struct {
		Items []BotRoom `json:"items"`
	}
	if err := a.desktopBotCall(context.Background(), http.MethodGet, "/api/v1/bot-rooms", nil, &out, botCollabWait); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// SaveBotRoom creates (id empty) or updates a room.
func (a *App) SaveBotRoom(id, name string, members []string, attention map[string]string) (*BotRoom, error) {
	var out BotRoom
	body := map[string]any{"name": name, "members": members, "attention": attention}
	method, path := http.MethodPost, "/api/v1/bot-rooms"
	if id = strings.TrimSpace(id); id != "" {
		method, path = http.MethodPut, "/api/v1/bot-rooms/"+url.PathEscape(id)
	}
	if err := a.desktopBotCall(context.Background(), method, path, body, &out, botCollabWait); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a *App) DeleteBotRoom(id string) error {
	return a.desktopBotCall(context.Background(), http.MethodDelete, "/api/v1/bot-rooms/"+url.PathEscape(strings.TrimSpace(id)), nil, nil, botCollabWait)
}

// PostBotRoomMessage is the person writing in a room.
func (a *App) PostBotRoomMessage(id, text string) (*BotRoomPost, error) {
	var out BotRoomPost
	if err := a.desktopBotCall(context.Background(), http.MethodPost, "/api/v1/bot-rooms/"+url.PathEscape(strings.TrimSpace(id))+"/messages", map[string]string{"text": text}, &out, botCollabWait); err != nil {
		return nil, err
	}
	return &out, nil
}
