package workflow

import "fmt"

// ValidationError represents a single validation issue in a workflow graph.
// It identifies the affected node (if applicable) and provides a human-readable message.
type ValidationError struct {
	NodeID    string    `json:"node_id,omitempty"`
	NodeLabel string    `json:"node_label,omitempty"`
	Position  *Position `json:"position,omitempty"`
	Message   string    `json:"message"`
}

// ValidateWorkflowGraphDetailed performs comprehensive validation of a workflow graph
// and returns all validation errors found (not just the first one).
// It checks:
//   - Graph must have at least one node
//   - Exactly one Trigger_Node must exist
//   - No disconnected nodes (all nodes reachable from trigger via BFS)
//   - Semantic edge rules: Trigger_Node can only be a source (no incoming edges)
func ValidateWorkflowGraphDetailed(graph WorkflowGraph) []ValidationError {
	var errors []ValidationError

	// 1. Check for empty graph
	if len(graph.Nodes) == 0 {
		errors = append(errors, ValidationError{
			Message: "workflow graph has no nodes",
		})
		return errors // Can't do further validation without nodes
	}

	// Build node lookup maps
	nodeByID := make(map[string]*WorkflowNode, len(graph.Nodes))
	for i := range graph.Nodes {
		nodeByID[graph.Nodes[i].ID] = &graph.Nodes[i]
	}

	// 2. Check exactly one Trigger_Node
	var triggerNodes []*WorkflowNode
	for i := range graph.Nodes {
		if graph.Nodes[i].Type == NodeTrigger {
			triggerNodes = append(triggerNodes, &graph.Nodes[i])
		}
	}

	switch len(triggerNodes) {
	case 0:
		errors = append(errors, ValidationError{
			Message: "workflow must have exactly one Trigger_Node as the entry point",
		})
	case 1:
		// Valid — exactly one trigger node
	default:
		// Multiple trigger nodes — report each extra one
		for _, tn := range triggerNodes[1:] {
			pos := tn.Position
			errors = append(errors, ValidationError{
				NodeID:    tn.ID,
				NodeLabel: tn.Label,
				Position:  &pos,
				Message:   "multiple Trigger_Nodes found; exactly one is required",
			})
		}
	}

	// 3. Semantic edge validation: Trigger_Node can only be a source, not a target
	triggerIDs := make(map[string]bool, len(triggerNodes))
	for _, tn := range triggerNodes {
		triggerIDs[tn.ID] = true
	}

	for _, edge := range graph.Edges {
		if triggerIDs[edge.TargetID] {
			// An edge targets a Trigger_Node — this is invalid
			targetNode := nodeByID[edge.TargetID]
			if targetNode != nil {
				pos := targetNode.Position
				errors = append(errors, ValidationError{
					NodeID:    targetNode.ID,
					NodeLabel: targetNode.Label,
					Position:  &pos,
					Message:   "Trigger_Node cannot have incoming edges; it can only be a start node",
				})
			}
		}
	}

	// 4. Check approval nodes have at least one configured approver or role.
	for i := range graph.Nodes {
		n := &graph.Nodes[i]
		if n.Type != NodeApproval {
			continue
		}
		hasApprover, err := approvalNodeHasApprover(*n)
		if err != nil || !hasApprover {
			pos := n.Position
			errors = append(errors, ValidationError{
				NodeID:    n.ID,
				NodeLabel: n.Label,
				Position:  &pos,
				Message:   "Approval node must have at least one approver or approval role",
			})
		}
	}

	// 5. Check for disconnected nodes (BFS from trigger)
	if len(triggerNodes) == 1 {
		triggerID := triggerNodes[0].ID

		// Build adjacency list (directed: source → targets)
		adj := make(map[string][]string)
		for _, e := range graph.Edges {
			adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
		}

		// BFS from trigger
		visited := make(map[string]bool)
		queue := []string{triggerID}
		visited[triggerID] = true

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			for _, neighbor := range adj[current] {
				if !visited[neighbor] {
					visited[neighbor] = true
					queue = append(queue, neighbor)
				}
			}
		}

		// Report disconnected nodes
		for i := range graph.Nodes {
			n := &graph.Nodes[i]
			if !visited[n.ID] {
				pos := n.Position
				errors = append(errors, ValidationError{
					NodeID:    n.ID,
					NodeLabel: n.Label,
					Position:  &pos,
					Message:   fmt.Sprintf("node %q is not reachable from the Trigger_Node", n.Label),
				})
			}
		}
	}

	return errors
}
