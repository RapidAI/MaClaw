package agent

import (
	"fmt"
	"sort"
	"strings"
)

// ClientCapabilities describes what a concrete client can accept and emit.
// It is intentionally separate from an IM platform's static capabilities:
// multiple clients using the same third-party gateway may have very different
// screens, speakers, microphones and media decoders.
type ClientCapabilities struct {
	Input    ClientInputCapabilities   `json:"input,omitempty"`
	Output   ClientOutputCapabilities  `json:"output,omitempty"`
	Features ClientFeatureCapabilities `json:"features,omitempty"`
}

type ClientInputCapabilities struct {
	Modalities []string                 `json:"modalities,omitempty"`
	Audio      *ClientAudioCapabilities `json:"audio,omitempty"`
	Image      *ClientImageCapabilities `json:"image,omitempty"`
}

type ClientOutputCapabilities struct {
	Modalities   []string                 `json:"modalities,omitempty"`
	Preferred    []string                 `json:"preferred,omitempty"`
	Combinations [][]string               `json:"combinations,omitempty"`
	Text         *ClientTextCapabilities  `json:"text,omitempty"`
	Audio        *ClientAudioCapabilities `json:"audio,omitempty"`
	Image        *ClientImageCapabilities `json:"image,omitempty"`
	File         *ClientFileCapabilities  `json:"file,omitempty"`
}

type ClientTextCapabilities struct {
	MaxChars int    `json:"maxChars,omitempty"`
	Markdown bool   `json:"markdown,omitempty"`
	Locale   string `json:"locale,omitempty"`
}

type ClientAudioCapabilities struct {
	MimeTypes        []string `json:"mimeTypes,omitempty"`
	SampleRates      []int    `json:"sampleRates,omitempty"`
	Channels         int      `json:"channels,omitempty"`
	Playback         bool     `json:"playback,omitempty"`
	DeliveryModes    []string `json:"deliveryModes,omitempty"`
	MaxInlineBytes   int64    `json:"maxInlineBytes,omitempty"`
	MaxDownloadBytes int64    `json:"maxDownloadBytes,omitempty"`
}

type ClientImageCapabilities struct {
	MimeTypes []string `json:"mimeTypes,omitempty"`
	MaxWidth  int      `json:"maxWidth,omitempty"`
	MaxHeight int      `json:"maxHeight,omitempty"`
	Animated  bool     `json:"animated,omitempty"`
}

type ClientFileCapabilities struct {
	MimeTypes []string `json:"mimeTypes,omitempty"`
	MaxBytes  int64    `json:"maxBytes,omitempty"`
}

type ClientFeatureCapabilities struct {
	PetStates    bool `json:"petStates,omitempty"`
	PetAnimation bool `json:"petAnimation,omitempty"`
	// PetAsset is separate from PetAnimation: a small client may display an
	// exact GUI-rendered frame but choose not to animate it.  Treating the two
	// as one flag made capable ESP clients either receive no asset at all or
	// have to overstate their animation support.
	PetAsset          bool `json:"petAsset,omitempty"`
	PetAssetMaxFrames int  `json:"petAssetMaxFrames,omitempty"`
	AmbientDisplay    bool `json:"ambientDisplay,omitempty"`
	MeetingRecorder   bool `json:"meetingRecorder,omitempty"`
	VolumeControl     bool `json:"volumeControl,omitempty"`
	BrightnessControl bool `json:"brightnessControl,omitempty"`
	// ScreenSleepControl declares that the client accepts an idle screen-off
	// timeout in hardware_config messages. It is separate from brightness: a
	// device may support a backlight level without owning its idle timer.
	ScreenSleepControl bool `json:"screenSleepControl,omitempty"`
}

const (
	maxClientCapabilityItems = 16
	maxClientTextChars       = 100000
	maxClientMediaDimension  = 8192
	maxClientMediaBytes      = int64(100 * 1024 * 1024)
)

var knownClientModalities = map[string]bool{
	"text": true, "audio": true, "image": true, "file": true,
}

// NormalizeClientCapabilities validates and bounds an untrusted capability
// declaration. Missing output capabilities deliberately become text-only so
// old clients remain useful without being sent unsupported media.
func NormalizeClientCapabilities(in *ClientCapabilities) ClientCapabilities {
	var out ClientCapabilities
	if in != nil {
		out = *in
	}
	out.Features.PetAssetMaxFrames = clampInt(out.Features.PetAssetMaxFrames, 0, 8)
	if !out.Features.PetAsset || !out.Features.PetAnimation {
		out.Features.PetAssetMaxFrames = 0
	}
	out.Input.Modalities = normalizeModalities(out.Input.Modalities)
	out.Output.Modalities = normalizeModalities(out.Output.Modalities)
	if len(out.Output.Modalities) == 0 {
		out.Output.Modalities = []string{"text"}
	}
	if containsClientString(out.Output.Modalities, "text") {
		if out.Output.Text == nil {
			out.Output.Text = &ClientTextCapabilities{}
		} else {
			copy := *out.Output.Text
			copy.MaxChars = clampInt(copy.MaxChars, 0, maxClientTextChars)
			copy.Locale = boundedString(copy.Locale, 32)
			out.Output.Text = &copy
		}
	} else {
		out.Output.Text = nil
	}
	out.Input.Audio = normalizeAudio(out.Input.Audio, containsClientString(out.Input.Modalities, "audio"))
	out.Output.Audio = normalizeAudio(out.Output.Audio, containsClientString(out.Output.Modalities, "audio"))
	out.Input.Image = normalizeImage(out.Input.Image, containsClientString(out.Input.Modalities, "image"))
	out.Output.Image = normalizeImage(out.Output.Image, containsClientString(out.Output.Modalities, "image"))
	out.Output.File = normalizeFile(out.Output.File, containsClientString(out.Output.Modalities, "file"))
	out.Output.Preferred = filterModalities(out.Output.Preferred, out.Output.Modalities)
	if len(out.Output.Preferred) == 0 {
		out.Output.Preferred = append([]string(nil), out.Output.Modalities...)
	}
	out.Output.Combinations = normalizeCombinations(out.Output.Combinations, out.Output.Modalities)
	if len(out.Output.Combinations) == 0 {
		for _, modality := range out.Output.Modalities {
			out.Output.Combinations = append(out.Output.Combinations, []string{modality})
		}
	}
	return out
}

func (c ClientCapabilities) SupportsOutput(modality string) bool {
	return containsClientString(c.Output.Modalities, strings.ToLower(strings.TrimSpace(modality)))
}

// SupportsInput reports whether the client can produce the declared inbound
// modality. Gateways can use this after handshake so the declaration remains
// an enforceable contract instead of prompt-only metadata.
func (c ClientCapabilities) SupportsInput(modality string) bool {
	return containsClientString(c.Input.Modalities, strings.ToLower(strings.TrimSpace(modality)))
}

// SupportsInputMIME is the inbound counterpart to SupportsOutputMIME. Text has
// no codec negotiation; media allow-lists accept exact MIME values and type/*
// wildcards. An empty allow-list means the transport may choose the encoding.
func (c ClientCapabilities) SupportsInputMIME(modality, mimeType string) bool {
	modality = strings.ToLower(strings.TrimSpace(modality))
	if !c.SupportsInput(modality) {
		return false
	}
	var allowed []string
	switch modality {
	case "audio":
		if c.Input.Audio != nil {
			allowed = c.Input.Audio.MimeTypes
		}
	case "image":
		if c.Input.Image != nil {
			allowed = c.Input.Image.MimeTypes
		}
	default:
		return modality == "text"
	}
	return mimeTypeAllowed(allowed, mimeType)
}

// SupportsOutputMIME reports whether the client accepts a concrete media
// encoding. An empty MIME allow-list means the modality is supported without
// further format restriction; otherwise the value must match an advertised
// type (including a wildcard such as image/*).
func (c ClientCapabilities) SupportsOutputMIME(modality, mimeType string) bool {
	modality = strings.ToLower(strings.TrimSpace(modality))
	if !c.SupportsOutput(modality) {
		return false
	}
	var allowed []string
	switch modality {
	case "audio":
		if c.Output.Audio != nil {
			allowed = c.Output.Audio.MimeTypes
		}
	case "image":
		if c.Output.Image != nil {
			allowed = c.Output.Image.MimeTypes
		}
	case "file":
		if c.Output.File != nil {
			allowed = c.Output.File.MimeTypes
		}
	default:
		return modality == "text"
	}
	return mimeTypeAllowed(allowed, mimeType)
}

// SupportsOutputBytes applies the declared file-size ceiling. Zero means the
// client did not advertise a tighter limit, and unknown sizes remain allowed.
func (c ClientCapabilities) SupportsOutputBytes(modality string, sizeBytes int64) bool {
	if !c.SupportsOutput(modality) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(modality), "file") && c.Output.File != nil &&
		c.Output.File.MaxBytes > 0 && sizeBytes > 0 {
		return sizeBytes <= c.Output.File.MaxBytes
	}
	return true
}

// SupportsOutputCombination reports whether all modalities may be delivered
// together. A single modality only needs to be declared in Output.Modalities.
func (c ClientCapabilities) SupportsOutputCombination(modalities ...string) bool {
	wanted := normalizeModalities(modalities)
	if len(wanted) == 0 {
		return false
	}
	if len(wanted) == 1 {
		return c.SupportsOutput(wanted[0])
	}
	for _, combination := range c.Output.Combinations {
		if sameStringSet(combination, wanted) {
			return true
		}
	}
	return false
}

// SelectClientOutputCombination chooses the richest declared subset of the
// modalities currently available in one logical Agent response. Preferred
// order breaks equal-size ties. Callers must drop every response part whose
// modality is absent from the result.
func SelectClientOutputCombination(capabilities ClientCapabilities, present ...string) []string {
	capabilities = NormalizeClientCapabilities(&capabilities)
	available := filterModalities(present, capabilities.Output.Modalities)
	if len(available) == 0 {
		return nil
	}
	best := []string{available[0]}
	bestRank := clientModalityPreferenceRank(capabilities.Output.Preferred, best[0])
	for _, modality := range available[1:] {
		rank := clientModalityPreferenceRank(capabilities.Output.Preferred, modality)
		if rank < bestRank {
			best = []string{modality}
			bestRank = rank
		}
	}
	for _, candidate := range capabilities.Output.Combinations {
		if len(candidate) == 0 || !clientModalitiesAreSubset(candidate, available) {
			continue
		}
		rank := len(capabilities.Output.Preferred)
		for _, modality := range candidate {
			if candidateRank := clientModalityPreferenceRank(capabilities.Output.Preferred, modality); candidateRank < rank {
				rank = candidateRank
			}
		}
		if len(candidate) > len(best) || (len(candidate) == len(best) && rank < bestRank) {
			best = append([]string(nil), candidate...)
			bestRank = rank
		}
	}
	return best
}

// BuildClientCapabilityPrompt converts a normalized device contract into a
// mandatory agent instruction. It lives in corelib so GUI and MaClawSrv use
// exactly the same semantics instead of drifting prompt implementations.
func BuildClientCapabilityPrompt(capabilities *ClientCapabilities) string {
	if capabilities == nil {
		return ""
	}
	normalized := NormalizeClientCapabilities(capabilities)
	var b strings.Builder
	b.WriteString("Target client capability contract (mandatory):\n")
	b.WriteString("- Input modalities: ")
	if len(normalized.Input.Modalities) == 0 {
		b.WriteString("none declared")
	} else {
		b.WriteString(strings.Join(normalized.Input.Modalities, ", "))
	}
	b.WriteString(".\n")
	b.WriteString("- Output modalities: ")
	b.WriteString(strings.Join(normalized.Output.Modalities, ", "))
	b.WriteString(". Preferred order: ")
	b.WriteString(strings.Join(normalized.Output.Preferred, ", "))
	b.WriteString(".\n")
	b.WriteString("- Allowed output combinations: ")
	for index, combination := range normalized.Output.Combinations {
		if index > 0 {
			b.WriteString("; ")
		}
		b.WriteString("[")
		b.WriteString(strings.Join(combination, "+"))
		b.WriteString("]")
	}
	b.WriteString(".\n")
	if normalized.Output.Text != nil {
		fmt.Fprintf(&b, "- Text: max %d Unicode characters (0 means transport default), markdown=%t, locale=%s.\n", normalized.Output.Text.MaxChars, normalized.Output.Text.Markdown, normalized.Output.Text.Locale)
	}
	if normalized.Output.Image != nil {
		fmt.Fprintf(&b, "- Image: max %dx%d, animated=%t, MIME=%s.\n", normalized.Output.Image.MaxWidth, normalized.Output.Image.MaxHeight, normalized.Output.Image.Animated, strings.Join(normalized.Output.Image.MimeTypes, ","))
	}
	if normalized.Output.Audio != nil {
		fmt.Fprintf(&b, "- Audio: playback=%t, MIME=%s, sampleRates=%s, channels=%d.\n", normalized.Output.Audio.Playback, strings.Join(normalized.Output.Audio.MimeTypes, ","), joinClientInts(normalized.Output.Audio.SampleRates), normalized.Output.Audio.Channels)
	}
	if normalized.Output.File != nil {
		fmt.Fprintf(&b, "- File: MIME=%s, maxBytes=%d (0 means transport default).\n", strings.Join(normalized.Output.File.MimeTypes, ","), normalized.Output.File.MaxBytes)
	}
	b.WriteString("Reply only in a declared output modality and combination. Do not create or attach image, audio, video, or file output unless declared. Always keep the useful answer in concise plain text when text is available.")
	return b.String()
}

func joinClientInts(values []int) string {
	if len(values) == 0 {
		return "any declared by transport"
	}
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = fmt.Sprintf("%d", value)
	}
	return strings.Join(parts, ",")
}

func normalizeAudio(in *ClientAudioCapabilities, enabled bool) *ClientAudioCapabilities {
	if !enabled || in == nil {
		return nil
	}
	out := *in
	out.MimeTypes = normalizeStrings(out.MimeTypes, 64)
	seen := map[int]bool{}
	out.SampleRates = make([]int, 0, len(in.SampleRates))
	for _, rate := range in.SampleRates {
		if rate >= 8000 && rate <= 192000 && !seen[rate] && len(out.SampleRates) < maxClientCapabilityItems {
			seen[rate] = true
			out.SampleRates = append(out.SampleRates, rate)
		}
	}
	out.Channels = clampInt(out.Channels, 0, 8)
	out.DeliveryModes = normalizeAudioDeliveryModes(out.DeliveryModes)
	if len(out.DeliveryModes) == 0 {
		// Backwards-compatible default: existing clients only understood media
		// embedded directly in an outgoing JSON message.
		out.DeliveryModes = []string{"inline"}
	}
	// A zero inline limit keeps the legacy unbounded meaning. URL delivery is
	// opt-in and therefore requires an explicit positive download bound.
	if containsClientString(out.DeliveryModes, "url") && out.MaxDownloadBytes == 0 {
		out.DeliveryModes = removeClientString(out.DeliveryModes, "url")
	}
	if out.MaxInlineBytes < 0 {
		out.MaxInlineBytes = 0
	} else if out.MaxInlineBytes > maxClientMediaBytes {
		out.MaxInlineBytes = maxClientMediaBytes
	}
	if out.MaxDownloadBytes < 0 {
		out.MaxDownloadBytes = 0
	} else if out.MaxDownloadBytes > maxClientMediaBytes {
		out.MaxDownloadBytes = maxClientMediaBytes
	}
	return &out
}

func normalizeAudioDeliveryModes(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if (value != "inline" && value != "url") || seen[value] || len(out) >= maxClientCapabilityItems {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func removeClientString(values []string, remove string) []string {
	out := values[:0]
	for _, value := range values {
		if value != remove {
			out = append(out, value)
		}
	}
	return out
}

// SupportsOutputAudioDelivery reports whether a client accepts the requested
// audio transport and whether the declared size fits that transport's bound.
func (c ClientCapabilities) SupportsOutputAudioDelivery(mode string, sizeBytes int64) bool {
	normalized := NormalizeClientCapabilities(&c)
	audio := normalized.Output.Audio
	if audio == nil || !audio.Playback || sizeBytes < 0 {
		return false
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if !containsClientString(audio.DeliveryModes, mode) {
		return false
	}
	limit := audio.MaxInlineBytes
	if mode == "url" {
		limit = audio.MaxDownloadBytes
	}
	return limit <= 0 || sizeBytes <= limit
}

func normalizeImage(in *ClientImageCapabilities, enabled bool) *ClientImageCapabilities {
	if !enabled || in == nil {
		return nil
	}
	out := *in
	out.MimeTypes = normalizeStrings(out.MimeTypes, 64)
	out.MaxWidth = clampInt(out.MaxWidth, 0, maxClientMediaDimension)
	out.MaxHeight = clampInt(out.MaxHeight, 0, maxClientMediaDimension)
	return &out
}

func normalizeFile(in *ClientFileCapabilities, enabled bool) *ClientFileCapabilities {
	if !enabled || in == nil {
		return nil
	}
	out := *in
	out.MimeTypes = normalizeStrings(out.MimeTypes, 64)
	if out.MaxBytes < 0 {
		out.MaxBytes = 0
	} else if out.MaxBytes > maxClientMediaBytes {
		out.MaxBytes = maxClientMediaBytes
	}
	return &out
}

func normalizeModalities(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if knownClientModalities[value] && !seen[value] && len(out) < maxClientCapabilityItems {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func filterModalities(values, allowed []string) []string {
	filtered := normalizeModalities(values)
	out := filtered[:0]
	for _, value := range filtered {
		if containsClientString(allowed, value) {
			out = append(out, value)
		}
	}
	return out
}

func normalizeCombinations(values [][]string, allowed []string) [][]string {
	out := make([][]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		combination := filterModalities(value, allowed)
		if len(combination) == 0 || len(combination) != len(normalizeModalities(value)) {
			continue
		}
		sorted := append([]string(nil), combination...)
		sort.Strings(sorted)
		key := strings.Join(sorted, "+")
		if !seen[key] && len(out) < maxClientCapabilityItems {
			seen[key] = true
			out = append(out, combination)
		}
	}
	return out
}

func normalizeStrings(values []string, maxLen int) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && len(value) <= maxLen && !seen[value] && len(out) < maxClientCapabilityItems {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func containsClientString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func clientModalitiesAreSubset(candidate, available []string) bool {
	for _, modality := range candidate {
		if !containsClientString(available, modality) {
			return false
		}
	}
	return true
}

func clientModalityPreferenceRank(preferred []string, wanted string) int {
	for index, modality := range preferred {
		if modality == wanted {
			return index
		}
	}
	return len(preferred)
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, value := range a {
		if !containsClientString(b, value) {
			return false
		}
	}
	return true
}

func mimeTypeAllowed(allowed []string, mimeType string) bool {
	if len(allowed) == 0 {
		return true
	}
	mimeType = strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0]))
	if mimeType == "" {
		return false
	}
	for _, candidate := range allowed {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "*/*" || candidate == mimeType {
			return true
		}
		if strings.HasSuffix(candidate, "/*") && strings.HasPrefix(mimeType, strings.TrimSuffix(candidate, "*")) {
			return true
		}
	}
	return false
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func boundedString(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return value
}
