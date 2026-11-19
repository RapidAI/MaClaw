package desktoppool

import (
	"context"
	"errors"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// ErrDesktopNotRecorded means this node has no installed desktop to resize.
var ErrDesktopNotRecorded = errors.New("desktop was not recorded")

// PrepareOwnedResources sizes a desktop that is about to be recorded for the
// first time. A missing measurement leaves the defaults in place. A machine
// under about 8GB is refused.
func PrepareOwnedResources(server Server, ramBytes uint64, logicalCPUs int) (Server, error) {
	if ramBytes == 0 {
		return server, nil
	}
	fitted, err := desktop.FitHostResources(ramBytes, logicalCPUs)
	if err != nil {
		return Server{}, err
	}
	server.Memory = fitted.Memory
	server.CPUs = fitted.CPUs
	server.ShmSize = fitted.ShmSize
	server.ResourcesAuto = true
	return server, nil
}

// FitNodeResources updates the desktop installed on nodeID from a later
// measurement. Cloud desktops and sizes an admin set by hand are left as
// they are. A machine under about 8GB returns an error and changes nothing.
func (p *Pool) FitNodeResources(ctx context.Context, tenantID, nodeID string, ramBytes uint64, logicalCPUs int) error {
	nodeID = strings.TrimSpace(nodeID)
	if p == nil || nodeID == "" || ramBytes == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return err
	}
	if !nodeNeedsRefit(rec.Servers, nodeID) {
		return nil
	}
	fitted, err := desktop.FitHostResources(ramBytes, logicalCPUs)
	if err != nil {
		return err
	}
	changed := false
	for i := range rec.Servers {
		current := rec.Servers[i]
		if current.NodeID != nodeID || !serverCanRefit(current) {
			continue
		}
		if current.Memory == fitted.Memory && current.CPUs == fitted.CPUs && current.ShmSize == fitted.ShmSize && current.ResourcesAuto {
			continue
		}
		current.Memory = fitted.Memory
		current.CPUs = fitted.CPUs
		current.ShmSize = fitted.ShmSize
		current.ResourcesAuto = true
		rec.Servers[i] = current
		changed = true
	}
	if !changed {
		return nil
	}
	return p.save(ctx, tenantID, rec)
}

// SetNodeResources stores a size an operator typed. Later measurements do
// not replace it. ErrDesktopNotRecorded means this node has no installed
// desktop, so nothing was stored.
func (p *Pool) SetNodeResources(ctx context.Context, tenantID, nodeID, memory, cpus, shm string) error {
	nodeID = strings.TrimSpace(nodeID)
	if p == nil || nodeID == "" {
		return nil
	}
	fitted, err := desktop.NormalizeResources("", memory, cpus, shm)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return err
	}
	changed := false
	for i := range rec.Servers {
		if rec.Servers[i].NodeID != nodeID || !serverIntranet(rec.Servers[i]) {
			continue
		}
		rec.Servers[i].Memory = fitted.Memory
		rec.Servers[i].CPUs = fitted.CPUs
		rec.Servers[i].ShmSize = fitted.ShmSize
		rec.Servers[i].ResourcesAuto = false
		rec.Servers[i].ResourcesPinned = true
		changed = true
	}
	if !changed {
		return ErrDesktopNotRecorded
	}
	return p.save(ctx, tenantID, rec)
}

// AllowNodeRefit lets the next measurement replace a size an operator typed.
func (p *Pool) AllowNodeRefit(ctx context.Context, tenantID, nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	if p == nil || nodeID == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return err
	}
	changed := false
	for i := range rec.Servers {
		if rec.Servers[i].NodeID != nodeID || !serverIntranet(rec.Servers[i]) {
			continue
		}
		if rec.Servers[i].ResourcesAuto && !rec.Servers[i].ResourcesPinned {
			continue
		}
		rec.Servers[i].ResourcesAuto = true
		rec.Servers[i].ResourcesPinned = false
		changed = true
	}
	if !changed {
		return nil
	}
	return p.save(ctx, tenantID, rec)
}

func nodeNeedsRefit(servers []Server, nodeID string) bool {
	for _, server := range servers {
		if server.NodeID == nodeID && serverCanRefit(server) {
			return true
		}
	}
	return false
}

func serverCanRefit(server Server) bool {
	if !serverIntranet(server) || server.ResourcesPinned {
		return false
	}
	return server.ResourcesAuto || defaultDesktopResources(server)
}

func defaultDesktopResources(server Server) bool {
	got, err := desktop.NormalizeResources("", server.Memory, server.CPUs, server.ShmSize)
	if err != nil {
		return false
	}
	defaults, err := desktop.NormalizeResources("", "", "", "")
	if err != nil {
		return false
	}
	return got.Memory == defaults.Memory && got.CPUs == defaults.CPUs && got.ShmSize == defaults.ShmSize
}
