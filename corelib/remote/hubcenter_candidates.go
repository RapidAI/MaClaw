package remote

// HubCenter address selection — single source of truth for GUI / TUI / agentservice.
//
// Policy (unique algorithm):
//
//  1. Registered public URLs (remote_hubcenter_url + remote_hubcenter_urls, non-loopback)
//     are the enrollment identity — same set the About panel displays.
//  2. If no public registration exists, configured loopbacks are allowed (local/dev).
//  3. Official DefaultRemoteHubCenterURLs seeds are used ONLY when there is no
//     public registration yet (first-time discovery / onboarding).
//  4. Once the user has a public registered center, never inject official defaults
//     or unregistered discovery peers (e.g. hubs2 when enrolled only on hubs.maclaw.top).
//  5. Candidate lists for runtime ops must be constrained back to that effective
//     seed set so cache / HA advertising cannot override enrollment identity.

// RegisteredPublicHubCenterURLs returns the enrollment identity set: non-loopback
// HubCenter URLs from user config, with non-preferred official defaults stripped.
//
// Official DefaultRemoteHubCenterURLs entries that are not the active preferred
// center are discovery seeds only — they must not become permanent identity even
// if a previous buggy write left them in remote_hubcenter_urls (e.g. hubs2).
// Custom (non-default) public URLs are always kept.
//
// Empty when preferred is unset/loopback (local/dev is not public enrollment) or
// when no public center is configured. Loopback preferred must NOT promote
// leftover public defaults from remote_hubcenter_urls into enrollment identity.
func RegisteredPublicHubCenterURLs(preferred string, configured []string) []string {
	pref := NormalizeHubCenterURL(preferred)
	if pref == "" || IsLoopbackURL(pref) {
		return nil
	}
	out := configuredHubCenterURLs(preferred, configured, false)
	if len(out) == 0 {
		// Preferred is public but missing from configured list — still enroll it.
		return []string{pref}
	}
	if !ContainsHubCenterURL(out, pref) {
		out = append([]string{pref}, out...)
	}
	return filterEnrollmentPublicURLs(pref, out)
}

// ConfiguredHubCenterURLs returns configured HubCenter URLs.
// When allowLoopback is false, loopback/unspecified hosts are omitted (About identity).
// Unlike RegisteredPublicHubCenterURLs, this does not strip official defaults
// (used for local/dev lists and raw config inspection).
func ConfiguredHubCenterURLs(preferred string, configured []string, allowLoopback bool) []string {
	return configuredHubCenterURLs(preferred, configured, allowLoopback)
}

func configuredHubCenterURLs(preferred string, configured []string, allowLoopback bool) []string {
	raw := append([]string{preferred}, configured...)
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, value := range raw {
		value = NormalizeHubCenterURL(value)
		if value == "" {
			continue
		}
		if !allowLoopback && IsLoopbackURL(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// filterEnrollmentPublicURLs keeps preferred first, drops non-preferred official
// defaults, and preserves custom public centers.
func filterEnrollmentPublicURLs(preferred string, urls []string) []string {
	preferred = NormalizeHubCenterURL(preferred)
	urls = NormalizeHubCenterURLs(urls)
	if len(urls) == 0 {
		return nil
	}
	defaultSet := officialDefaultHubCenterSet()
	out := make([]string, 0, len(urls))
	seen := make(map[string]struct{}, len(urls))
	add := func(u string) {
		u = NormalizeHubCenterURL(u)
		if u == "" {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		if _, isDefault := defaultSet[u]; isDefault && u != preferred {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	if preferred != "" {
		add(preferred)
	}
	for _, u := range urls {
		add(u)
	}
	return out
}

// EffectiveHubCenterSeeds builds the seed list used to resolve HubCenter addresses.
// This is the unique algorithm shared by AppConfig.HubCenterBaseURLs and all clients.
//
// defaults should typically be DefaultRemoteHubCenterURLs (or a test override).
func EffectiveHubCenterSeeds(preferred string, configured, defaults []string) []string {
	public := RegisteredPublicHubCenterURLs(preferred, configured)
	if len(public) > 0 {
		// Enrollment identity wins: do not merge official HA defaults.
		return public
	}
	// No public enrollment — local/dev loopbacks only, then official discovery seeds.
	// Do NOT re-add leftover public URLs from remote_hubcenter_urls (pollution /
	// previous HA writes); those are not enrollment and must not be probed first.
	out := loopbackHubCenterURLs(preferred, configured)
	if len(defaults) == 0 {
		return out
	}
	return NormalizeHubCenterURLs(append(out, defaults...))
}

// loopbackHubCenterURLs returns only loopback/unspecified configured centers
// (local/dev). Public leftovers are intentionally excluded.
func loopbackHubCenterURLs(preferred string, configured []string) []string {
	raw := configuredHubCenterURLs(preferred, configured, true)
	out := make([]string, 0, len(raw))
	for _, u := range raw {
		if IsLoopbackURL(u) {
			out = append(out, u)
		}
	}
	return out
}

// AlignHubCenterCandidates constrains a resolved/discovered candidate list to
// valid addresses under the unique enrollment policy.
//
// registeredPublic is the About/enrollment identity from
// RegisteredPublicHubCenterURLs (empty when unregistered). It must NOT include
// official DefaultRemoteHubCenterURLs — defaults are discovery seeds only.
//
//   - When registeredPublic is non-empty: only those URLs remain (resolved order
//     first, then any missing registered entries).
//   - When unregistered: keep resolved + seeds (local/dev + first-time defaults).
func AlignHubCenterCandidates(registeredPublic, seeds, resolved []string) []string {
	registeredPublic = NormalizeHubCenterURLs(registeredPublic)
	// Drop loopbacks from registration identity if a caller passed mixed input.
	publicIdentity := make([]string, 0, len(registeredPublic))
	for _, u := range registeredPublic {
		if !IsLoopbackURL(u) {
			publicIdentity = append(publicIdentity, u)
		}
	}
	seeds = NormalizeHubCenterURLs(seeds)
	resolved = NormalizeHubCenterURLs(resolved)

	if len(publicIdentity) == 0 {
		// Unregistered / local-only: accept full discovery + seed pool.
		return NormalizeHubCenterURLs(append(append([]string{}, resolved...), seeds...))
	}

	seedSet := make(map[string]struct{}, len(publicIdentity))
	for _, s := range publicIdentity {
		seedSet[s] = struct{}{}
	}

	out := make([]string, 0, len(publicIdentity))
	seen := make(map[string]struct{}, len(publicIdentity))
	add := func(u string) {
		u = NormalizeHubCenterURL(u)
		if u == "" {
			return
		}
		if _, ok := seedSet[u]; !ok {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	// Prefer order from live resolution when those URLs are enrolled.
	for _, u := range resolved {
		add(u)
	}
	// Ensure every registered public identity URL is present.
	for _, u := range publicIdentity {
		add(u)
	}
	return out
}

// PreferRegisteredHubCenterBase picks the preferred base for display / identity.
// Returns the first public registered URL, or empty when none is configured.
func PreferRegisteredHubCenterBase(preferred string, configured []string) string {
	public := RegisteredPublicHubCenterURLs(preferred, configured)
	if len(public) == 0 {
		return ""
	}
	return public[0]
}

// ConstrainHubCenterPersistence prepares preferred+URLs for config write-back.
// Unlike Align with seeds, this never injects official defaults into config:
//
//   - public preferred: enrollment filter (preferred + customs; drop other defaults)
//   - loopback preferred: keep loopbacks + custom discovery peers; drop official defaults
//   - empty preferred: keep non-default customs from discovered, or empty
func ConstrainHubCenterPersistence(registeredPublic []string, base string, discovered []string) (string, []string) {
	base = NormalizeHubCenterURL(base)
	merged := NormalizeHubCenterURLs(append(append([]string{base}, registeredPublic...), discovered...))
	if base == "" && len(merged) == 0 {
		return "", nil
	}

	// Public successful/preferred center → enrollment identity write-back.
	if base != "" && !IsLoopbackURL(base) {
		pref := base
		identity := NormalizeHubCenterURLs(registeredPublic)
		// If enrollment is already known and base is a foreign HA peer (e.g. hubs2),
		// do not promote it over the registered preferred center.
		if len(identity) > 0 && !ContainsHubCenterURL(identity, base) {
			pref = identity[0]
		}
		out := filterEnrollmentPublicURLs(pref, append(append([]string{}, identity...), merged...))
		if len(out) == 0 {
			return pref, []string{pref}
		}
		return pref, out
	}

	// Loopback / local-dev: keep loopbacks + custom public peers from discovery.
	// Drop official defaults only (they are global seeds, not this machine's chain).
	if base != "" && IsLoopbackURL(base) {
		defaultSet := officialDefaultHubCenterSet()
		out := make([]string, 0, len(merged))
		seen := make(map[string]struct{}, len(merged))
		add := func(u string) {
			u = NormalizeHubCenterURL(u)
			if u == "" {
				return
			}
			if _, ok := seen[u]; ok {
				return
			}
			if _, isDefault := defaultSet[u]; isDefault {
				return
			}
			// Loopbacks and non-default public customs (local HA / test peers) are kept.
			seen[u] = struct{}{}
			out = append(out, u)
		}
		add(base)
		for _, u := range merged {
			add(u)
		}
		if len(out) == 0 {
			return base, []string{base}
		}
		return base, out
	}

	// No preferred: keep customs only (no official defaults).
	defaultSet := officialDefaultHubCenterSet()
	out := make([]string, 0, len(merged))
	for _, u := range merged {
		if _, isDefault := defaultSet[u]; isDefault {
			continue
		}
		out = append(out, u)
	}
	if len(out) == 0 {
		return "", nil
	}
	return out[0], out
}

func officialDefaultHubCenterSet() map[string]struct{} {
	set := make(map[string]struct{}, len(DefaultRemoteHubCenterURLs))
	for _, u := range DefaultRemoteHubCenterURLs {
		u = NormalizeHubCenterURL(u)
		if u != "" {
			set[u] = struct{}{}
		}
	}
	return set
}

// ContainsHubCenterURL reports whether want is present in list after normalization.
func ContainsHubCenterURL(list []string, want string) bool {
	want = NormalizeHubCenterURL(want)
	if want == "" {
		return false
	}
	for _, u := range list {
		if NormalizeHubCenterURL(u) == want {
			return true
		}
	}
	return false
}

// PickAlignedHubCenterBase returns base if it remains in aligned candidates,
// otherwise the first aligned candidate (or empty).
func PickAlignedHubCenterBase(base string, aligned []string) string {
	base = NormalizeHubCenterURL(base)
	if base != "" && ContainsHubCenterURL(aligned, base) {
		return base
	}
	if len(aligned) == 0 {
		return ""
	}
	return aligned[0]
}

// DeprioritizeRecentlyFailedHubCenters stable-partitions bases so hosts with
// recent probe failures are tried after clean hosts, without dropping any candidate.
func DeprioritizeRecentlyFailedHubCenters(bases []string) []string {
	bases = NormalizeHubCenterURLs(bases)
	if len(bases) <= 1 {
		return bases
	}
	healthy := make([]string, 0, len(bases))
	failed := make([]string, 0, len(bases))
	for _, base := range bases {
		if HasRecentFailures(base) {
			failed = append(failed, base)
			continue
		}
		healthy = append(healthy, base)
	}
	if len(failed) == 0 || len(healthy) == 0 {
		return bases
	}
	return append(healthy, failed...)
}
