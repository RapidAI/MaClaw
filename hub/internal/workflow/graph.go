package workflow

import "encoding/json"

// NodeType represents the type of a workflow node.
type NodeType string

const (
	NodeTrigger         NodeType = "trigger"
	NodeForm            NodeType = "form"
	NodeApproval        NodeType = "approval"
	NodeConditionBranch NodeType = "condition_branch"
	NodeAction          NodeType = "action"
	NodeNotification    NodeType = "notification"
	NodeSubProcess      NodeType = "sub_process"
	NodeTypeTerminal    NodeType = "terminal"
)

// ApprovalMode represents the mode of an approval node.
type ApprovalMode string

const (
	ModeSingle      ApprovalMode = "single"
	ModeCountersign ApprovalMode = "countersign"
	ModeAnyNofM     ApprovalMode = "any_n_of_m"
	ModeSequential  ApprovalMode = "sequential"
)

// WorkflowGraph is the directed graph of nodes and edges.
type WorkflowGraph struct {
	Nodes []WorkflowNode `json:"nodes"`
	Edges []WorkflowEdge `json:"edges"`
}

// WorkflowNode represents a single node in the workflow graph.
type WorkflowNode struct {
	ID       string          `json:"id"`
	Type     NodeType        `json:"type"`
	Label    string          `json:"label"`
	Position Position        `json:"position"`
	Config   json.RawMessage `json:"config"`
}

// Position represents the canvas x,y coordinates of a node.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// WorkflowEdge represents a directed edge between two nodes.
type WorkflowEdge struct {
	ID       string `json:"id"`
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
	Label    string `json:"label,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

// ApprovalNodeConfig is the configuration for an Approval node.
type ApprovalNodeConfig struct {
	ApproverIDs      []string     `json:"approver_ids"`
	Mode             ApprovalMode `json:"mode"`
	MinApprovals     int          `json:"min_approvals,omitempty"`
	ApproverOrder    []string     `json:"approver_order,omitempty"`
	TimeoutHours     int          `json:"timeout_hours"`
	FallbackApprover string       `json:"fallback_approver,omitempty"`
}

// ConditionBranchConfig is the configuration for a Condition Branch node.
type ConditionBranchConfig struct {
	Branches      []BranchCondition `json:"branches"`
	DefaultBranch string            `json:"default_branch,omitempty"`
}

// BranchCondition defines a single branch with its target and condition.
type BranchCondition struct {
	TargetNodeID string        `json:"target_node_id"`
	Expression   ConditionExpr `json:"expression"`
	Priority     int           `json:"priority"`
}

// ConditionExpr represents a condition expression for branch evaluation.
type ConditionExpr struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value"`
}
