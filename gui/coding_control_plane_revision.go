package main

import "sync"

// This file contains the S3 control-plane compatibility fence for Coding's
// legacy static belt. The revision is callback-local and intentionally has no
// relation to provider response identity, durable grants, or journals.
// Same-surface re-renders keep the current revision so a report accepted on
// one model turn can authorize the following turn's matching edit.

func lockedCodingControlPlaneRevision(mu *sync.RWMutex, surface *map[string]struct{}, revision *uint64) uint64 {
	if mu == nil || surface == nil || revision == nil {
		return 0
	}
	mu.RLock()
	defer mu.RUnlock()
	if *surface == nil {
		// Explicit direct-host/test compatibility path.
		return 0
	}
	return *revision
}

func (c *codingSubAgentCallbacks) currentControlPlaneRevision() uint64 {
	if c == nil {
		return 0
	}
	return lockedCodingControlPlaneRevision(&c.staticCompatibilitySurfaceMu, &c.staticCompatibilitySurface, &c.staticCompatibilityRevision)
}

func (c *codingSubAgentCallbacks) storeLocalizationForCurrentControlPlaneRevision(e CodingSubAgentLocalizationEvidence) uint64 {
	if c == nil {
		return 0
	}
	revision := c.currentControlPlaneRevision()
	c.localization.setForRevision(e, revision)
	return revision
}

func (c *codingSubAgentCallbacks) localizationForCurrentControlPlaneRevision() *CodingSubAgentLocalizationEvidence {
	if c == nil {
		return nil
	}
	return c.localization.snapshotForRevision(c.currentControlPlaneRevision())
}

func (c *remoteCodingCallbacks) currentControlPlaneRevision() uint64 {
	if c == nil {
		return 0
	}
	return lockedCodingControlPlaneRevision(&c.staticCompatibilitySurfaceMu, &c.staticCompatibilitySurface, &c.staticCompatibilityRevision)
}

func (c *remoteCodingCallbacks) storeLocalizationForCurrentControlPlaneRevision(e CodingSubAgentLocalizationEvidence) uint64 {
	if c == nil {
		return 0
	}
	revision := c.currentControlPlaneRevision()
	c.localization.setForRevision(e, revision)
	return revision
}

func (c *remoteCodingCallbacks) localizationForCurrentControlPlaneRevision() *CodingSubAgentLocalizationEvidence {
	if c == nil {
		return nil
	}
	return c.localization.snapshotForRevision(c.currentControlPlaneRevision())
}
