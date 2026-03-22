package chat

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ChannelService handles channel CRUD and membership.
type ChannelService struct {
	store *Store
}

// NewChannelService creates a ChannelService.
func NewChannelService(store *Store) *ChannelService {
	return &ChannelService{store: store}
}

// CreateChannel creates a new channel and adds the creator + members.
func (s *ChannelService) CreateChannel(creatorID string, chType ChannelType, name string, memberIDs []string) (*Channel, error) {
	ch := &Channel{
		ID:        uuid.NewString(),
		Type:      chType,
		Name:      name,
		CreatedBy: creatorID,
		CreatedAt: time.Now(),
	}
	if err := s.store.CreateChannel(ch); err != nil {
		return nil, fmt.Errorf("create channel: %w", err)
	}

	// Add creator as owner.
	if err := s.store.AddMember(&Member{
		ChannelID: ch.ID,
		UserID:    creatorID,
		Role:      RoleOwner,
		JoinedAt:  time.Now(),
	}); err != nil {
		return nil, fmt.Errorf("add creator: %w", err)
	}

	// Add other members.
	for _, uid := range memberIDs {
		if uid == creatorID {
			continue
		}
		if err := s.store.AddMember(&Member{
			ChannelID: ch.ID,
			UserID:    uid,
			Role:      RoleMember,
			JoinedAt:  time.Now(),
		}); err != nil {
			return nil, fmt.Errorf("add member %s: %w", uid, err)
		}
	}

	return ch, nil
}

// GetUserChannels returns all channels the user belongs to.
func (s *ChannelService) GetUserChannels(userID string) ([]Channel, error) {
	return s.store.GetChannelsForUser(userID)
}

// GetMembers returns all members of a channel.
func (s *ChannelService) GetMembers(channelID string) ([]Member, error) {
	return s.store.GetMembers(channelID)
}

// IsMember checks if a user belongs to a channel.
func (s *ChannelService) IsMember(channelID, userID string) (bool, error) {
	return s.store.IsMember(channelID, userID)
}
