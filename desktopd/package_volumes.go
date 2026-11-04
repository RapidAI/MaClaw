package desktopd

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// packageMountsReady reports whether the container already mounts every
// PackageMounts volume. Missing ones hide the image's /usr behind an empty
// volume if we only added them on the next create, so recreateReason forces
// a rebuild that seeds first.
func packageMountsReady(mountText string, spec Spec) bool {
	want, err := desktop.PackageMounts(spec.TenantID, spec.UserID)
	if err != nil || len(want) == 0 {
		return false
	}
	have := map[string]bool{}
	for _, item := range strings.Fields(mountText) {
		have[item] = true
	}
	for _, mount := range want {
		if !have[mount.Volume+"="+mount.Target] {
			return false
		}
	}
	return true
}

func appendUserMountArgs(args []string, mounts []desktop.Mount) []string {
	for _, mount := range mounts {
		args = append(args, "--volume", mount.Volume+":"+mount.Target)
	}
	return args
}

// seedPackageVolumes copies each empty package volume from image. A volume
// that already holds files is left alone, so an apt install in either
// container keeps growing the same tree. image must be the one the
// containers will run (the state image when the user has one).
func (s *Service) seedPackageVolumes(ctx context.Context, spec Spec, image string) error {
	mounts, err := desktop.PackageMounts(spec.TenantID, spec.UserID)
	if err != nil {
		return err
	}
	for _, mount := range mounts {
		if _, err := s.docker(ctx, "volume", "create", mount.Volume); err != nil {
			return err
		}
		out, err := s.docker(ctx, "run", "--rm", "--entrypoint", "sh",
			"--volume", mount.Volume+":/seed:ro",
			image, "-c", `test -z "$(ls -A /seed 2>/dev/null | head -1)" && echo EMPTY || echo USED`)
		if err != nil {
			return err
		}
		if !strings.Contains(out, "EMPTY") {
			continue
		}
		log.Printf("desktopd: seeding package volume %s from %s into %s", mount.Target, image, mount.Volume)
		if _, err := s.docker(ctx, "run", "--rm", "--entrypoint", "sh",
			"--volume", mount.Volume+":/seed",
			image, "-c", fmt.Sprintf("cp -a %s/. /seed/", mount.Target)); err != nil {
			return fmt.Errorf("seed %s: %w", mount.Target, err)
		}
	}
	return nil
}
