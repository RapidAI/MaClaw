package database

import (
	"context"
	"fmt"
	"strings"
)

func profileUsesReplica(p Profile) bool {
	return strings.TrimSpace(p.ReplicaHost) != ""
}

func replicaProfile(p Profile) Profile {
	out := p
	out.Host = strings.TrimSpace(p.ReplicaHost)
	if p.ReplicaPort > 0 {
		out.Port = p.ReplicaPort
	}
	if id := strings.TrimSpace(p.ReplicaSSHSessionID); id != "" {
		out.SSHSessionID = id
	}
	out.ReadOnly = true
	out.WriteEnabled = false
	out.ReplicaHost = ""
	out.ReplicaPort = 0
	out.ReplicaSSHSessionID = ""
	return out
}

// routedAdapter sends reads to an optional replica and writes to primary.
type routedAdapter struct {
	primary            Adapter
	replica            Adapter
	replicaUnavailable bool
}

func (a *routedAdapter) Ping(ctx context.Context) error {
	if a == nil || a.primary == nil {
		return fmt.Errorf("database unavailable")
	}
	return a.primary.Ping(ctx)
}

func (a *routedAdapter) Close() error {
	var first error
	if a.replica != nil {
		first = a.replica.Close()
	}
	if a.primary != nil {
		if err := a.primary.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (a *routedAdapter) Capabilities() Capabilities {
	if a == nil || a.primary == nil {
		return Capabilities{}
	}
	return a.primary.Capabilities()
}

func (a *routedAdapter) read() Adapter {
	if a != nil && a.replica != nil {
		return a.replica
	}
	if a != nil {
		return a.primary
	}
	return nil
}

func (a *routedAdapter) annotateRead(result QueryResult, usedReplica bool, err error) (QueryResult, error) {
	if err != nil {
		return result, err
	}
	if usedReplica {
		result.Warnings = append(result.Warnings, "routed_to_replica")
		return result, nil
	}
	if a != nil && a.replicaUnavailable {
		result.Warnings = append(result.Warnings, "replica_unavailable")
	}
	return result, nil
}

func (a *routedAdapter) Inspect(ctx context.Context, req InspectRequest) (SchemaInfo, error) {
	reader := a.read()
	if reader == nil {
		return SchemaInfo{}, fmt.Errorf("database unavailable")
	}
	usedReplica := a.replica != nil && reader == a.replica
	info, err := reader.Inspect(ctx, req)
	if err != nil && usedReplica && a.primary != nil {
		info, err = a.primary.Inspect(ctx, req)
		if err != nil {
			return info, err
		}
		info.Capabilities = a.Capabilities()
		info.Warnings = append(info.Warnings, "replica_fallback")
		return info, nil
	}
	if err != nil {
		return info, err
	}
	info.Capabilities = a.Capabilities()
	if usedReplica {
		info.Warnings = append(info.Warnings, "routed_to_replica")
	} else if a != nil && a.replicaUnavailable {
		info.Warnings = append(info.Warnings, "replica_unavailable")
	}
	return info, nil
}

func (a *routedAdapter) Query(ctx context.Context, req QueryRequest) (QueryResult, error) {
	if a == nil || a.primary == nil {
		return QueryResult{}, fmt.Errorf("database unavailable")
	}
	if a.replica != nil {
		out, err := a.replica.Query(ctx, req)
		if err == nil {
			return a.annotateRead(out, true, nil)
		}
		out, err = a.primary.Query(ctx, req)
		if err != nil {
			return out, err
		}
		out.Warnings = append(out.Warnings, "replica_fallback")
		return out, nil
	}
	out, err := a.primary.Query(ctx, req)
	return a.annotateRead(out, false, err)
}

func (a *routedAdapter) Execute(ctx context.Context, req ExecuteRequest) (MutationResult, error) {
	if a == nil || a.primary == nil {
		return MutationResult{}, fmt.Errorf("database unavailable")
	}
	return a.primary.Execute(ctx, req)
}

func (a *routedAdapter) ExecuteBatch(ctx context.Context, req BatchExecuteRequest) (MutationResult, error) {
	if a == nil || a.primary == nil {
		return MutationResult{}, fmt.Errorf("database unavailable")
	}
	batch, ok := a.primary.(BatchAdapter)
	if !ok {
		return MutationResult{}, fmt.Errorf("unsupported_capability: batch transactions are unavailable")
	}
	return batch.ExecuteBatch(ctx, req)
}

func openReplicaAdapter(ctx context.Context, p Profile, resolve SecretResolver, dial TunnelDialer) (Adapter, error) {
	rp := replicaProfile(p)
	if err := authorizeProfileEndpoint(rp); err != nil {
		return nil, err
	}
	adapter, err := openProfileWithTunnel(ctx, rp, resolve, dial)
	if err != nil {
		return nil, err
	}
	if err := adapter.Ping(ctx); err != nil {
		_ = adapter.Close()
		return nil, classify(err)
	}
	return adapter, nil
}
