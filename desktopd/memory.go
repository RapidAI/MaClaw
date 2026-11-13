package desktopd

import (
	"context"
	"strconv"
	"strings"
)

// MemoryReport is one container's current memory. Used and Limit are bytes.
// Percent is used/limit, or 0 when the limit is unknown.
type MemoryReport struct {
	UsedBytes  int64   `json:"used_bytes"`
	LimitBytes int64   `json:"limit_bytes"`
	Percent    float64 `json:"percent"`
}

// Memory reads the running container's memory from docker stats. desktopd
// is the process that owns the container, so callers do not inspect cgroup
// files themselves.
func (s *Service) Memory(ctx context.Context, tenantID, userID string) (MemoryReport, error) {
	if s == nil {
		return MemoryReport{}, ErrDocker
	}
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return MemoryReport{}, err
	}
	// Bots share the owner's container. The stats are that container's.
	name := containerName(spec.TenantID, spec.UserID)
	out, err := s.docker(ctx, "stats", "--no-stream", "--format", "{{.MemUsage}}", name)
	if err != nil {
		return MemoryReport{}, err
	}
	rep, ok := ParseMemUsage(out)
	if !ok {
		return MemoryReport{}, ErrDocker
	}
	return rep, nil
}

// ParseMemUsage reads docker's "1.5GiB / 3GiB" memory line.
func ParseMemUsage(raw string) (MemoryReport, bool) {
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return MemoryReport{}, false
	}
	used, ok := parseMemQuantity(parts[0])
	if !ok {
		return MemoryReport{}, false
	}
	limit, ok := parseMemQuantity(parts[1])
	if !ok || limit <= 0 {
		return MemoryReport{}, false
	}
	return MemoryReport{UsedBytes: used, LimitBytes: limit, Percent: float64(used) / float64(limit)}, true
}

// parseMemQuantity reads a docker stats size, including a fractional GiB.
func parseMemQuantity(raw string) (int64, bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return 0, false
	}
	i := 0
	for i < len(raw) && (raw[i] == '.' || (raw[i] >= '0' && raw[i] <= '9')) {
		i++
	}
	if i == 0 {
		return 0, false
	}
	num, err := strconv.ParseFloat(raw[:i], 64)
	if err != nil || num < 0 {
		return 0, false
	}
	unit := strings.TrimSpace(raw[i:])
	var mul float64
	switch unit {
	case "", "b":
		mul = 1
	case "kb", "kib", "k":
		mul = 1024
	case "mb", "mib", "m":
		mul = 1024 * 1024
	case "gb", "gib", "g":
		mul = 1024 * 1024 * 1024
	default:
		return 0, false
	}
	return int64(num * mul), true
}
