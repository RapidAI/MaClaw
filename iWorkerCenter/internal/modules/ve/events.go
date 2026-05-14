package ve

// WebSocket event type constants for VE real-time communication.
// These events are pushed from Hub to connected Maclaw Clients via WebSocket
// when VE-related state changes occur.
const (
	// VEEventListUpdate is pushed when the VE list changes (new registration,
	// removal, or approval). Clients should refresh their VE list upon receipt.
	VEEventListUpdate = "ve:list_update"

	// VEEventStatusChange is pushed when a VE's online/offline status changes.
	// Clients should update the corresponding status indicator in the VE list.
	VEEventStatusChange = "ve:status_change"

	// VEEventAuthRequest is pushed to the VE owner when another user requests
	// access to a per_request VE. The owner should display an authorization dialog.
	VEEventAuthRequest = "ve:auth_request"

	// VEEventApproved is pushed to the registering client when a Hub admin
	// approves their VE registration request.
	VEEventApproved = "ve:approved"

	// VEEventRejected is pushed to the registering client when a Hub admin
	// rejects their VE registration request.
	VEEventRejected = "ve:rejected"

	// VEEventDisabled is pushed to the VE owner when a Hub admin disables
	// their virtual employee.
	VEEventDisabled = "ve:disabled"

	// VEEventGroupConfig is pushed to all connected clients when the Hub admin
	// changes group chat configuration (e.g., max_group_participants limit).
	VEEventGroupConfig = "ve:group_config"
)
