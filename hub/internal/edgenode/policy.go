package edgenode

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// Policy is the platform limit for one tenant's own Bot nodes.
// A tenant with no saved policy may create nodes, with no numeric cap.
type Policy struct {
	AllowSelf  bool `json:"allow_self"`
	MaxNodes   int  `json:"max_nodes"`
	MaxStreams int  `json:"max_streams"`
	MaxViews   int  `json:"max_views"`
}

type storedPolicy struct {
	Set bool `json:"set"`
	Policy
}

var (
	ErrSelfNodes   = errors.New("这个租户还不能自建节点")
	ErrNodeLimit   = errors.New("这个租户的节点数已到上限")
	ErrStreamLimit = errors.New("这个租户的隧道连接已到上限")
	ErrViewLimit   = errors.New("这个租户的画面路数已到上限")
)

func policyKey(tenantID string) string {
	tenantID = store.NormalizeTenantID(tenantID)
	if tenantID == "" || tenantID == store.DefaultTenantID {
		return "edge_policy"
	}
	return "tenant:" + tenantID + ":edge_policy"
}

// Policy reads the platform limit. A missing record allows nodes and sets
// every numeric cap to zero, which means no cap.
func (r *Registry) Policy(ctx context.Context, tenantID string) (Policy, error) {
	open := Policy{AllowSelf: true}
	if r == nil || r.System == nil {
		return open, errors.New("edge node store is unavailable")
	}
	raw, err := r.System.Get(ctx, policyKey(tenantID))
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, os.ErrNotExist) {
			return open, nil
		}
		return Policy{}, err
	}
	if strings.TrimSpace(raw) == "" {
		return open, nil
	}
	var stored storedPolicy
	if json.Unmarshal([]byte(raw), &stored) != nil || !stored.Set {
		return open, nil
	}
	return stored.Policy, nil
}

// SetPolicy saves a platform limit. A negative cap is rejected. Zero means
// that dimension is not capped. AllowSelf false blocks new codes and enrollments.
func (r *Registry) SetPolicy(ctx context.Context, tenantID string, p Policy) error {
	if r == nil || r.System == nil {
		return errors.New("edge node store is unavailable")
	}
	if p.MaxNodes < 0 || p.MaxStreams < 0 || p.MaxViews < 0 {
		return errors.New("limit must be zero or greater")
	}
	raw, err := json.Marshal(storedPolicy{Set: true, Policy: p})
	if err != nil {
		return err
	}
	return r.System.Set(ctx, policyKey(tenantID), string(raw))
}

func (r *Registry) admitNewNode(ctx context.Context, tenantID string) error {
	p, err := r.Policy(ctx, tenantID)
	if err != nil {
		return err
	}
	if !p.AllowSelf {
		return ErrSelfNodes
	}
	if p.MaxNodes <= 0 {
		return nil
	}
	current, err := r.load(ctx, tenantID)
	if err != nil {
		return err
	}
	if len(current.Nodes) >= p.MaxNodes {
		return ErrNodeLimit
	}
	return nil
}

// ListAll is the platform view: every tenant's nodes, without secrets.
func (r *Registry) ListAll(ctx context.Context) ([]Node, error) {
	if r == nil || !Enabled() {
		return nil, ErrDisabled
	}
	index, err := r.readIndex(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Node
	for _, tenantID := range index {
		tenantID = store.NormalizeTenantID(tenantID)
		if tenantID == "" || seen[tenantID] {
			continue
		}
		seen[tenantID] = true
		nodes, err := r.List(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		out = append(out, nodes...)
	}
	if out == nil {
		out = []Node{}
	}
	return out, nil
}
