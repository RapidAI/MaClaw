package im

import (
	"fmt"
	"sync"
)

// SpaceStateType represents the user's current interaction space.
type SpaceStateType string

const (
	SpaceLobby    SpaceStateType = "lobby"
	SpacePrivate  SpaceStateType = "private"
	SpaceMeeting  SpaceStateType = "meeting"
	SpaceWorkflow SpaceStateType = "workflow"
)

// SpaceState holds the user's current space state and associated metadata.
type SpaceState struct {
	State         SpaceStateType
	PrivateTarget string   // machineID of private chat target
	PrivateName   string   // display name of private chat target
	MeetingTopic  string   // meeting topic
	Participants  []string // meeting participant machineIDs
	MessageCount  int      // message count in private mode (for periodic reminders)
	WorkflowID    string   // active workflow ID (when State == SpaceWorkflow)
	WorkflowType  string   // active workflow type (when State == SpaceWorkflow)
}

// spaceStateStore manages per-user space states (in-memory).
type spaceStateStore struct {
	mu   sync.RWMutex
	data map[string]*SpaceState
}

func newSpaceStateStore() *spaceStateStore {
	return &spaceStateStore{data: make(map[string]*SpaceState)}
}

// GetOrCreate returns the user's space state, creating a default lobby state if absent.
func (s *spaceStateStore) GetOrCreate(userID string) *SpaceState {
	s.mu.RLock()
	st := s.data[userID]
	s.mu.RUnlock()
	if st != nil {
		return st
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if st = s.data[userID]; st != nil {
		return st
	}
	st = &SpaceState{State: SpaceLobby}
	s.data[userID] = st
	return st
}

// EnterPrivate transitions from lobby to private. Returns error if not in lobby.
func (s *spaceStateStore) EnterPrivate(userID, machineID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.data[userID]
	if st == nil {
		st = &SpaceState{State: SpaceLobby}
		s.data[userID] = st
	}
	if st.State == SpaceMeeting {
		return fmt.Errorf("会议进行中，无法切换私聊。使用 /ask <设备名> <消息> 临时交互，或 /stop 结束会议。")
	}
	if st.State == SpacePrivate {
		// Already in private — just switch target.
	}
	st.State = SpacePrivate
	st.PrivateTarget = machineID
	st.PrivateName = name
	st.MessageCount = 0
	return nil
}

// ExitPrivate transitions from private back to lobby.
func (s *spaceStateStore) ExitPrivate(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.data[userID]
	if st == nil || st.State != SpacePrivate {
		return fmt.Errorf("当前不在私聊模式")
	}
	st.State = SpaceLobby
	st.PrivateTarget = ""
	st.PrivateName = ""
	st.MessageCount = 0
	return nil
}

// EnterMeeting transitions from lobby to meeting. Returns error if not in lobby.
func (s *spaceStateStore) EnterMeeting(userID, topic string, participants []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.data[userID]
	if st == nil {
		st = &SpaceState{State: SpaceLobby}
		s.data[userID] = st
	}
	if st.State == SpacePrivate {
		return fmt.Errorf("私聊模式中，无法发起会议。发送 /call all 返回大厅后再发起。")
	}
	if st.State == SpaceMeeting {
		return fmt.Errorf("已有会议进行中，请先 /stop 结束当前会议。")
	}
	st.State = SpaceMeeting
	st.MeetingTopic = topic
	st.Participants = participants
	return nil
}

// ExitMeeting transitions from meeting back to lobby.
func (s *spaceStateStore) ExitMeeting(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.data[userID]
	if st == nil || st.State != SpaceMeeting {
		return fmt.Errorf("当前不在会议模式")
	}
	st.State = SpaceLobby
	st.MeetingTopic = ""
	st.Participants = nil
	return nil
}

// RemoveParticipant removes a device from the meeting participant list.
// Returns the number of remaining participants.
func (s *spaceStateStore) RemoveParticipant(userID, machineID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.data[userID]
	if st == nil || st.State != SpaceMeeting {
		return 0
	}
	var remaining []string
	for _, p := range st.Participants {
		if p != machineID {
			remaining = append(remaining, p)
		}
	}
	st.Participants = remaining
	return len(remaining)
}

// Reset resets the user's space state to lobby.
func (s *spaceStateStore) Reset(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[userID] = &SpaceState{State: SpaceLobby}
}

// EnterWorkflow transitions from lobby to workflow mode.
func (s *spaceStateStore) EnterWorkflow(userID, workflowID, workflowType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.data[userID]
	if st == nil {
		st = &SpaceState{State: SpaceLobby}
		s.data[userID] = st
	}
	if st.State == SpacePrivate {
		return fmt.Errorf("私聊模式中，无法进入工作流。发送 /call all 返回大厅后再开始。")
	}
	if st.State == SpaceMeeting {
		return fmt.Errorf("会议进行中，无法进入工作流。使用 /stop 结束会议后再开始。")
	}
	if st.State == SpaceWorkflow {
		return fmt.Errorf("已有活跃工作流，请先完成或取消。")
	}
	st.State = SpaceWorkflow
	st.WorkflowID = workflowID
	st.WorkflowType = workflowType
	return nil
}

// ExitWorkflow transitions from workflow back to lobby.
func (s *spaceStateStore) ExitWorkflow(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.data[userID]
	if st == nil || st.State != SpaceWorkflow {
		return fmt.Errorf("当前不在工作流模式")
	}
	st.State = SpaceLobby
	st.WorkflowID = ""
	st.WorkflowType = ""
	return nil
}

// IncrementMessageCount increments the private mode message counter and
// returns the new count.
func (s *spaceStateStore) IncrementMessageCount(userID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.data[userID]
	if st == nil {
		return 0
	}
	st.MessageCount++
	return st.MessageCount
}
