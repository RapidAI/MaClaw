package main

// This file contains the S3 control-plane compatibility fence for Coding's
// legacy static belt. The revision is callback-local and intentionally has no
// relation to provider response identity, durable grants, or journals.

func (c *codingSubAgentCallbacks) currentControlPlaneRevision() uint64 {
	if c == nil {
		return 0
	}
	c.staticCompatibilitySurfaceMu.RLock()
	defer c.staticCompatibilitySurfaceMu.RUnlock()
	if c.staticCompatibilitySurface == nil {
		// Explicit direct-host/test compatibility path.
		return 0
	}
	return c.staticCompatibilityRevision
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
	c.staticCompatibilitySurfaceMu.RLock()
	defer c.staticCompatibilitySurfaceMu.RUnlock()
	if c.staticCompatibilitySurface == nil {
		return 0
	}
	return c.staticCompatibilityRevision
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
