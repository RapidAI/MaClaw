package corelib

import "testing"

func TestCodingQualityGateConfigDefaultsOff(t *testing.T) {
	var c AppConfig
	if c.IsCodingQualityGateEnabled() {
		t.Fatal("quality gate must default off")
	}
	c.SetCodingQualityGateEnabled(true)
	if !c.IsCodingQualityGateEnabled() {
		t.Fatal("expected quality gate on")
	}
	c.SetCodingQualityGateEnabled(false)
	if c.IsCodingQualityGateEnabled() {
		t.Fatal("explicit false must stay off")
	}
}

func TestAcpHostConfigDefaults(t *testing.T) {
	var c AppConfig
	if !c.IsAcpHostEnabled() {
		t.Fatal("default AcpHost should be enabled")
	}
	if !c.IsAcpHostMirrorUI() {
		t.Fatal("default mirror UI should be enabled")
	}
	if c.PreferredAcpHostPort() != 0 {
		t.Fatalf("preferred port default = %d", c.PreferredAcpHostPort())
	}
	c.SetAcpHostEnabled(false)
	if c.IsAcpHostEnabled() {
		t.Fatal("expected disabled")
	}
	c.AcpHostPort = 18789
	if c.PreferredAcpHostPort() != 18789 {
		t.Fatalf("port = %d", c.PreferredAcpHostPort())
	}
	c.SetAcpHostMirrorUI(false)
	if c.IsAcpHostMirrorUI() {
		t.Fatal("expected mirror off")
	}
}
