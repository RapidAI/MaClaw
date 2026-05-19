package chat

import (
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
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
	return s.CreateChannelForTenant(store.DefaultTenantID, creatorID, chType, name, memberIDs)
}

func (s *ChannelService) CreateChannelForTenant(tenantID, creatorID string, chType ChannelType, name string, memberIDs []string) (*Channel, error) {
	ch := &Channel{
		ID:        uuid.NewString(),
		TenantID:  store.NormalizeTenantID(tenantID),
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

// GetChannel returns a channel by ID.
func (s *ChannelService) GetChannel(channelID string) (*Channel, error) {
	return s.store.GetChannel(channelID)
}

// GetUserChannels returns all channels the user belongs to.
func (s *ChannelService) GetUserChannels(userID string) ([]Channel, error) {
	return s.store.GetChannelsForUser(userID)
}

func (s *ChannelService) GetTenantUserChannels(tenantID, userID string) ([]Channel, error) {
	return s.store.GetChannelsForTenantUser(tenantID, userID)
}

// GetMembers returns all members of a channel.
func (s *ChannelService) GetMembers(channelID string) ([]Member, error) {
	return s.store.GetMembers(channelID)
}

// IsMember checks if a user belongs to a channel.
func (s *ChannelService) IsMember(channelID, userID string) (bool, error) {
	return s.store.IsMember(channelID, userID)
}
