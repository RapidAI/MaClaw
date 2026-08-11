package codingruntime

import "testing"

func TestRemoteTargetIdentityNormalizesAndRequiresPinnedCanonicalTarget(t *testing.T) {
	target := RemoteTarget{Host: " Build.EXAMPLE.test ", User: " deploy ", WorkDir: "/srv//app/../app/", HostKeyFingerprint: "SHA256:pin"}
	id, err := target.Identity()
	if err != nil || id == "" {
		t.Fatalf("Identity() = %q, %v", id, err)
	}
	normalized, err := NormalizeRemoteTarget(target)
	if err != nil || normalized.Port != 22 || normalized.Host != "build.example.test" || normalized.WorkDir != "/srv/app" {
		t.Fatalf("NormalizeRemoteTarget() = %#v, %v", normalized, err)
	}
	for _, invalid := range []RemoteTarget{
		{Host: "host", User: "user", WorkDir: "/srv/app"},
		{Host: "host", User: "user", WorkDir: "relative", HostKeyFingerprint: "SHA256:pin"},
		{Host: "host", User: "user", WorkDir: "/", HostKeyFingerprint: "SHA256:pin"},
	} {
		if _, err := invalid.Identity(); err == nil {
			t.Fatalf("Identity() accepted invalid target %#v", invalid)
		}
	}
}
