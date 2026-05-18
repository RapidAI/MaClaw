package needleruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/needledata"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const ArtifactVersion = 1

type Request struct {
	Task    string
	Text    string
	Choices []string
}

type EncodedInput struct {
	Prompt   string `json:"prompt"`
	TokenIDs []int  `json:"token_ids"`
}

type Options struct {
	Enabled   bool
	ModelPath string
	MinConf   float64
}

type Predictor interface {
	Predict(ctx context.Context, req Request) (needledata.Decision, error)
}

const (
	RejectReasonRuntimeDisabled = "runtime_disabled"
	RejectReasonUnsupportedTask = "unsupported_task"
	RejectReasonEmptyDecision   = "empty_decision"
	RejectReasonOutsideChoices  = "outside_choices"
	RejectReasonBelowMinConf    = "below_min_confidence"
)

type Runtime struct {
	enabled        bool
	minConf        float64
	model          Predictor
	manifest       *Manifest
	collection     *CollectionManifest
	tokenizer      *SimpleTokenizer
	labels         []string
	tasks          map[string]bool
	collectionRoot string
	taskMu         sync.RWMutex
	taskRuntimes   map[string]*Runtime
}

type CollectionManifest struct {
	Format  string                         `json:"format"`
	Version int                            `json:"version"`
	Dim     int                            `json:"dim,omitempty"`
	Tasks   map[string]CollectionTaskEntry `json:"tasks"`
}

type CollectionTaskEntry struct {
	Path    string   `json:"path"`
	Records int      `json:"records,omitempty"`
	Labels  []string `json:"labels,omitempty"`
	MinConf float64  `json:"min_conf,omitempty"`
}

type Manifest struct {
	Format       string        `json:"format"`
	Version      int           `json:"version"`
	Runtime      string        `json:"runtime"`
	Tasks        []string      `json:"tasks"`
	WeightPath   string        `json:"weight_path,omitempty"`
	WeightSHA256 string        `json:"weight_sha256,omitempty"`
	WeightHeader *WeightHeader `json:"weight_header,omitempty"`
	Tokenizer    string        `json:"tokenizer,omitempty"`
	Labels       string        `json:"labels,omitempty"`
	Quant        string        `json:"quant,omitempty"`
	Notes        string        `json:"notes,omitempty"`
}

type InspectResult struct {
	Enabled      bool            `json:"enabled"`
	ModelPath    string          `json:"model_path,omitempty"`
	Mode         string          `json:"mode"`
	Usable       bool            `json:"usable"`
	Manifest     *Manifest       `json:"manifest,omitempty"`
	Collection   *CollectionInfo `json:"collection,omitempty"`
	Fallback     string          `json:"fallback,omitempty"`
	Error        string          `json:"error,omitempty"`
	Warnings     []string        `json:"warnings,omitempty"`
	Weight       *WeightInfo     `json:"weight,omitempty"`
	Tokenizer    *TokenizerInfo  `json:"tokenizer,omitempty"`
	Labels       *LabelInfo      `json:"labels,omitempty"`
	MinConf      float64         `json:"min_conf"`
	ArtifactPath string          `json:"artifact_path,omitempty"`
}

type CollectionInfo struct {
	Format    string                        `json:"format"`
	Version   int                           `json:"version"`
	Dim       int                           `json:"dim,omitempty"`
	ModelPath string                        `json:"model_path,omitempty"`
	Tasks     map[string]CollectionTaskInfo `json:"tasks"`
}

type CollectionTaskInfo struct {
	Path           string         `json:"path"`
	ResolvedPath   string         `json:"resolved_path,omitempty"`
	Records        int            `json:"records,omitempty"`
	Labels         []string       `json:"labels,omitempty"`
	MinConf        float64        `json:"min_conf,omitempty"`
	RuntimeInspect *InspectResult `json:"runtime_inspect,omitempty"`
}

type WeightInfo struct {
	Path              string        `json:"path"`
	Header            *WeightHeader `json:"header,omitempty"`
	SHA256            string        `json:"sha256,omitempty"`
	Size              int64         `json:"size,omitempty"`
	ExpectedSize      int64         `json:"expected_size,omitempty"`
	EmbeddingBytes    int           `json:"embedding_bytes,omitempty"`
	HeadBytes         int           `json:"head_bytes,omitempty"`
	BiasBytes         int           `json:"bias_bytes,omitempty"`
	SparseHashHead    bool          `json:"sparse_hash_head,omitempty"`
	IdentityEmbedding bool          `json:"identity_embedding,omitempty"`
}

func New(opts Options) (*Runtime, error) {
	minConf := opts.MinConf
	if minConf <= 0 {
		minConf = 0.78
	}
	r := &Runtime{enabled: opts.Enabled, minConf: minConf}
	if strings.TrimSpace(opts.ModelPath) != "" {
		if collection, ok, err := LoadCollection(opts.ModelPath); err != nil {
			return nil, err
		} else if ok {
			r.collection = collection
			r.collectionRoot = opts.ModelPath
			r.tasks = collectionTaskSet(collection.Tasks)
			r.taskRuntimes = make(map[string]*Runtime, len(collection.Tasks))
			for _, task := range sortedTaskKeys(r.tasks) {
				path := collectionTaskArtifactPath(opts.ModelPath, collection.Tasks[task].Path)
				if path == "" {
					return nil, fmt.Errorf("Needle collection task %s has invalid path %q", task, collection.Tasks[task].Path)
				}
			}
			return r, nil
		}
		manifest, err := LoadManifest(opts.ModelPath)
		if err != nil {
			return nil, err
		}
		r.manifest = manifest
		r.tasks = manifestTaskSet(manifest.Tasks)
		if strings.TrimSpace(manifest.Tokenizer) == "" {
			return nil, fmt.Errorf("Needle manifest missing tokenizer")
		}
		tok, err := LoadSimpleTokenizer(artifactFilePath(opts.ModelPath, manifest.Tokenizer))
		if err != nil {
			return nil, err
		}
		r.tokenizer = tok
		if strings.TrimSpace(manifest.Labels) == "" {
			return nil, fmt.Errorf("Needle manifest missing labels")
		}
		labels, err := LoadLabels(artifactFilePath(opts.ModelPath, manifest.Labels))
		if err != nil {
			return nil, err
		}
		r.labels = labels
		if strings.TrimSpace(manifest.WeightPath) == "" {
			return nil, fmt.Errorf("Needle manifest missing weight_path")
		}
		weightPath := artifactFilePath(opts.ModelPath, manifest.WeightPath)
		weights, err := ReadQ8Weights(weightPath)
		if err != nil {
			return nil, err
		}
		if err := validateWeightFileForLoad(weightPath, manifest, weights.Header); err != nil {
			return nil, err
		}
		if warnings := compareManifestWeightHeader(manifest.WeightHeader, weights.Header); len(warnings) > 0 {
			return nil, fmt.Errorf("Needle manifest weight_header mismatch: %s", strings.Join(warnings, "; "))
		}
		model, err := NewQ8Predictor(r.tokenizer, r.labels, weights)
		if err != nil {
			return nil, err
		}
		r.model = model
	}
	// Without an explicit artifact, keep the lightweight pure-Go rule predictor
	// available for development and shadow-mode smoke tests.
	if r.model == nil {
		r.model = NewRulePredictor()
	}
	return r, nil
}

func validateWeightFileForLoad(path string, manifest *Manifest, header *WeightHeader) error {
	if header == nil {
		return fmt.Errorf("Needle weight header is missing")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	embLen, headLen, biasLen, err := WeightDataLengths(header)
	if err != nil {
		return err
	}
	wantSize := int64(header.DataOffset) + int64(embLen+headLen+biasLen)
	if info.Size() != wantSize {
		return fmt.Errorf("Needle weight size mismatch: got %d bytes, want %d bytes", info.Size(), wantSize)
	}
	if manifest != nil && strings.TrimSpace(manifest.WeightSHA256) != "" {
		sha, err := fileSHA256(path)
		if err != nil {
			return fmt.Errorf("Needle weight sha256 failed: %w", err)
		}
		if !strings.EqualFold(manifest.WeightSHA256, sha) {
			return fmt.Errorf("Needle weight_sha256 mismatch")
		}
	}
	return nil
}

func (r *Runtime) Encode(req Request) (EncodedInput, error) {
	if r != nil && r.collection != nil {
		child, ok, err := r.runtimeForTask(req.Task)
		if err != nil {
			return EncodedInput{}, err
		}
		if !ok {
			return EncodedInput{}, fmt.Errorf("needleruntime: collection has no model for task %q", req.Task)
		}
		return child.Encode(req)
	}
	if r == nil || r.tokenizer == nil {
		return EncodedInput{}, fmt.Errorf("needleruntime: tokenizer is not loaded")
	}
	prompt := RenderPrompt(req)
	ids := r.tokenizer.Encode(prompt)
	return EncodedInput{Prompt: prompt, TokenIDs: ids}, nil
}

func (r *Runtime) Predict(ctx context.Context, req Request) (needledata.Decision, bool, error) {
	decision, accepted, _, err := r.PredictDetailed(ctx, req)
	return decision, accepted, err
}

func (r *Runtime) PredictDetailed(ctx context.Context, req Request) (needledata.Decision, bool, string, error) {
	if r != nil && r.collection != nil {
		child, ok, err := r.runtimeForTask(req.Task)
		if err != nil || !ok {
			return needledata.Decision{}, false, RejectReasonUnsupportedTask, err
		}
		return child.PredictDetailed(ctx, req)
	}
	if r == nil || !r.enabled || r.model == nil {
		return needledata.Decision{}, false, RejectReasonRuntimeDisabled, nil
	}
	if len(r.tasks) > 0 && !r.tasks[strings.TrimSpace(req.Task)] {
		return needledata.Decision{}, false, RejectReasonUnsupportedTask, nil
	}
	decision, err := r.model.Predict(ctx, req)
	if err != nil {
		return needledata.Decision{}, false, "", err
	}
	if strings.TrimSpace(decision.Name) == "" {
		return decision, false, RejectReasonEmptyDecision, nil
	}
	if !decisionAllowedByChoices(decision.Name, req.Choices) {
		return decision, false, RejectReasonOutsideChoices, nil
	}
	if decision.Confidence < r.minConf {
		return decision, false, RejectReasonBelowMinConf, nil
	}
	return decision, true, "", nil
}

func decisionAllowedByChoices(name string, choices []string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(choices) == 0 {
		return name != ""
	}
	for _, choice := range choices {
		if strings.EqualFold(strings.TrimSpace(choice), name) {
			return true
		}
	}
	return false
}

func (r *Runtime) runtimeForTask(task string) (*Runtime, bool, error) {
	if r == nil || r.collection == nil {
		return nil, false, nil
	}
	task = strings.TrimSpace(task)
	if task == "" || !r.tasks[task] {
		return nil, false, nil
	}
	r.taskMu.RLock()
	child := r.taskRuntimes[task]
	r.taskMu.RUnlock()
	if child != nil {
		return child, true, nil
	}
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	if child := r.taskRuntimes[task]; child != nil {
		return child, true, nil
	}
	entry := r.collection.Tasks[task]
	path := collectionTaskArtifactPath(r.collectionRoot, entry.Path)
	if path == "" {
		return nil, true, fmt.Errorf("Needle collection task %s has invalid path %q", task, entry.Path)
	}
	child, err := New(Options{Enabled: r.enabled, ModelPath: path, MinConf: collectionTaskMinConf(entry, r.minConf)})
	if err != nil {
		return nil, true, fmt.Errorf("Needle collection task %s: %w", task, err)
	}
	r.taskRuntimes[task] = child
	return child, true, nil
}

func collectionTaskMinConf(entry CollectionTaskEntry, fallback float64) float64 {
	if entry.MinConf > 0 {
		return entry.MinConf
	}
	return fallback
}

func manifestTaskSet(tasks []string) map[string]bool {
	out := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if task = strings.TrimSpace(task); task != "" {
			out[task] = true
		}
	}
	return out
}

func Inspect(opts Options) InspectResult {
	minConf := opts.MinConf
	if minConf <= 0 {
		minConf = 0.78
	}
	result := InspectResult{Enabled: opts.Enabled, ModelPath: opts.ModelPath, Mode: "rule_fallback", Usable: opts.Enabled, Fallback: "rule_predictor", MinConf: minConf}
	if strings.TrimSpace(opts.ModelPath) == "" {
		return result
	}
	if collection, ok, err := LoadCollection(opts.ModelPath); err != nil {
		result.Usable = false
		result.Error = err.Error()
		return result
	} else if ok {
		result.Mode = "collection"
		result.Fallback = "none"
		result.ArtifactPath = collectionArtifactPath(opts.ModelPath)
		result.Usable = opts.Enabled
		result.Collection = &CollectionInfo{Format: collection.Format, Version: collection.Version, Dim: collection.Dim, ModelPath: strings.TrimSpace(opts.ModelPath), Tasks: make(map[string]CollectionTaskInfo, len(collection.Tasks))}
		for _, task := range sortedTaskKeys(collectionTaskSet(collection.Tasks)) {
			entry := collection.Tasks[task]
			info := CollectionTaskInfo{Path: entry.Path, Records: entry.Records, Labels: entry.Labels, MinConf: collectionTaskMinConf(entry, minConf)}
			path := collectionTaskArtifactPath(opts.ModelPath, entry.Path)
			if path == "" {
				result.Usable = false
				result.Warnings = append(result.Warnings, fmt.Sprintf("task %s has invalid path %q", task, entry.Path))
				result.Collection.Tasks[task] = info
				continue
			}
			info.ResolvedPath = path
			child := Inspect(Options{Enabled: opts.Enabled, ModelPath: path, MinConf: info.MinConf})
			info.RuntimeInspect = &child
			if !child.Usable {
				result.Usable = false
			}
			for _, warning := range child.Warnings {
				result.Warnings = append(result.Warnings, fmt.Sprintf("task %s: %s", task, warning))
			}
			if child.Error != "" {
				result.Warnings = append(result.Warnings, fmt.Sprintf("task %s: %s", task, child.Error))
			}
			result.Collection.Tasks[task] = info
		}
		return result
	}
	manifest, err := LoadManifest(opts.ModelPath)
	if err != nil {
		result.Usable = false
		result.Error = err.Error()
		return result
	}
	result.Manifest = manifest
	result.ArtifactPath = manifestArtifactPath(opts.ModelPath)
	result.Warnings = inspectArtifactFiles(opts.ModelPath, manifest)
	weight, weightWarnings := inspectWeight(opts.ModelPath, manifest)
	result.Weight = weight
	result.Warnings = append(result.Warnings, weightWarnings...)
	tok, tokWarnings := inspectTokenizer(opts.ModelPath, manifest)
	result.Tokenizer = tok
	result.Warnings = append(result.Warnings, tokWarnings...)
	labels, labelWarnings := inspectLabels(opts.ModelPath, manifest)
	result.Labels = labels
	result.Warnings = append(result.Warnings, labelWarnings...)
	if weight != nil && weight.Header != nil && labels != nil && int(weight.Header.NumLabels) > len(labels.Labels) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("labels=%d smaller than weight labels=%d", len(labels.Labels), weight.Header.NumLabels))
	}
	if weight != nil && weight.Header != nil && manifest.WeightHeader != nil {
		result.Warnings = append(result.Warnings, compareManifestWeightHeader(manifest.WeightHeader, weight.Header)...)
	}
	if weight != nil && weight.Header != nil && tok != nil && tok.MaxID >= int(weight.Header.VocabSize) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("tokenizer max id %d exceeds weight vocab size %d", tok.MaxID, weight.Header.VocabSize))
	}
	if weight != nil && weight.Header != nil && weight.Header.Flags&(WeightFlagIdentityEmbedding|WeightFlagSparseHashHead) != 0 && weight.Header.VocabSize != weight.Header.HiddenSize {
		result.Warnings = append(result.Warnings, fmt.Sprintf("identity embedding flag requires vocab_size == hidden_size, got vocab=%d hidden=%d", weight.Header.VocabSize, weight.Header.HiddenSize))
	}
	if weight != nil && weight.Header != nil && weight.Header.Flags&WeightFlagSparseHashHead != 0 && (tok == nil || !tok.Hashing || tok.MaxID < 0 || tok.MaxID+1 != int(weight.Header.VocabSize)) {
		result.Warnings = append(result.Warnings, "sparse hash head requires a complete __hN hashing tokenizer vocabulary")
	}
	if strings.EqualFold(manifest.Runtime, "go") && manifest.WeightPath != "" {
		result.Mode = "q8_linear"
		result.Fallback = "rule_predictor_if_q8_load_fails"
		result.Usable = opts.Enabled && len(result.Warnings) == 0
	}
	return result
}

func compareManifestWeightHeader(manifestHeader, actual *WeightHeader) []string {
	if manifestHeader == nil || actual == nil {
		return nil
	}
	var warnings []string
	if manifestHeader.Version != 0 && manifestHeader.Version != actual.Version {
		warnings = append(warnings, fmt.Sprintf("manifest weight_header version=%d does not match weight version=%d", manifestHeader.Version, actual.Version))
	}
	if manifestHeader.VocabSize != actual.VocabSize {
		warnings = append(warnings, fmt.Sprintf("manifest weight_header vocab_size=%d does not match weight vocab_size=%d", manifestHeader.VocabSize, actual.VocabSize))
	}
	if manifestHeader.HiddenSize != actual.HiddenSize {
		warnings = append(warnings, fmt.Sprintf("manifest weight_header hidden_size=%d does not match weight hidden_size=%d", manifestHeader.HiddenSize, actual.HiddenSize))
	}
	if manifestHeader.NumLabels != actual.NumLabels {
		warnings = append(warnings, fmt.Sprintf("manifest weight_header num_labels=%d does not match weight num_labels=%d", manifestHeader.NumLabels, actual.NumLabels))
	}
	if manifestHeader.Flags != actual.Flags {
		warnings = append(warnings, fmt.Sprintf("manifest weight_header flags=%d does not match weight flags=%d", manifestHeader.Flags, actual.Flags))
	}
	if manifestHeader.DataOffset != 0 && manifestHeader.DataOffset != actual.DataOffset {
		warnings = append(warnings, fmt.Sprintf("manifest weight_header data_offset=%d does not match weight data_offset=%d", manifestHeader.DataOffset, actual.DataOffset))
	}
	return warnings
}

func LoadManifest(path string) (*Manifest, error) {
	manifestPath := manifestArtifactPath(path)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read Needle manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse Needle manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func LoadCollection(path string) (*CollectionManifest, bool, error) {
	collectionPath := collectionArtifactPath(path)
	data, err := os.ReadFile(collectionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, true, fmt.Errorf("read Needle collection: %w", err)
	}
	var collection CollectionManifest
	if err := json.Unmarshal(data, &collection); err != nil {
		return nil, true, fmt.Errorf("parse Needle collection: %w", err)
	}
	if err := collection.Validate(); err != nil {
		return nil, true, err
	}
	return &collection, true, nil
}

func ResolveCollectionTaskPath(root, task string) (string, bool, error) {
	collection, ok, err := LoadCollection(root)
	if err != nil || !ok {
		return "", false, err
	}
	task = strings.TrimSpace(task)
	entry, ok := collection.Tasks[task]
	if !ok {
		return "", false, nil
	}
	path := collectionTaskArtifactPath(root, entry.Path)
	if path == "" {
		return "", true, fmt.Errorf("Needle collection task %s has invalid path %q", task, entry.Path)
	}
	return path, true, nil
}
func (m CollectionManifest) Validate() error {
	if m.Format != "maclaw-needle-collection" {
		return fmt.Errorf("unsupported Needle collection format %q", m.Format)
	}
	if m.Version != ArtifactVersion {
		return fmt.Errorf("unsupported Needle collection version %d", m.Version)
	}
	if len(m.Tasks) == 0 {
		return fmt.Errorf("Needle collection missing tasks")
	}
	for task, entry := range m.Tasks {
		if strings.TrimSpace(task) == "" {
			return fmt.Errorf("Needle collection has empty task")
		}
		if strings.TrimSpace(entry.Path) == "" {
			return fmt.Errorf("Needle collection task %s missing path", task)
		}
	}
	return nil
}
func (m Manifest) Validate() error {
	if m.Format != "maclaw-needle" {
		return fmt.Errorf("unsupported Needle artifact format %q", m.Format)
	}
	if m.Version != ArtifactVersion {
		return fmt.Errorf("unsupported Needle artifact version %d", m.Version)
	}
	if strings.TrimSpace(m.Runtime) == "" {
		return fmt.Errorf("Needle manifest missing runtime")
	}
	if len(m.Tasks) == 0 {
		return fmt.Errorf("Needle manifest missing tasks")
	}
	return nil
}

func manifestArtifactPath(path string) string {
	path = strings.TrimSpace(path)
	if strings.EqualFold(filepath.Base(path), "manifest.json") {
		return path
	}
	return filepath.Join(path, "manifest.json")
}

func collectionArtifactPath(path string) string {
	path = strings.TrimSpace(path)
	if strings.EqualFold(filepath.Base(path), "collection.json") {
		return path
	}
	if strings.EqualFold(filepath.Base(path), "manifest.json") {
		path = filepath.Dir(path)
	}
	return filepath.Join(path, "collection.json")
}

func collectionTaskArtifactPath(root, rel string) string {
	root = strings.TrimSpace(root)
	rel = strings.TrimSpace(rel)
	if root == "" || rel == "" || filepath.IsAbs(rel) {
		return ""
	}
	if strings.EqualFold(filepath.Base(root), "collection.json") || strings.EqualFold(filepath.Base(root), "manifest.json") {
		root = filepath.Dir(root)
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	candidate, err := filepath.Abs(filepath.Join(base, rel))
	if err != nil {
		return ""
	}
	if candidate == base {
		return ""
	}
	relToBase, err := filepath.Rel(base, candidate)
	if err != nil || relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(filepath.Separator)) {
		return ""
	}
	return candidate
}
func inspectArtifactFiles(modelPath string, manifest *Manifest) []string {
	if manifest == nil {
		return nil
	}
	base := strings.TrimSpace(modelPath)
	if strings.EqualFold(filepath.Base(base), "manifest.json") {
		base = filepath.Dir(base)
	}
	var warnings []string
	for _, rel := range []struct {
		label string
		path  string
	}{
		{label: "weight_path", path: manifest.WeightPath},
		{label: "tokenizer", path: manifest.Tokenizer},
		{label: "labels", path: manifest.Labels},
	} {
		if strings.TrimSpace(rel.path) == "" {
			continue
		}
		candidate := artifactFilePath(base, rel.path)
		if _, err := os.Stat(candidate); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s not found: %s", rel.label, rel.path))
		}
	}
	return warnings
}

func inspectWeight(modelPath string, manifest *Manifest) (*WeightInfo, []string) {
	if manifest == nil || strings.TrimSpace(manifest.WeightPath) == "" {
		return nil, nil
	}
	path := artifactFilePath(modelPath, manifest.WeightPath)
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil
	}
	weight := &WeightInfo{Path: manifest.WeightPath, Size: info.Size()}
	sha, err := fileSHA256(path)
	if err != nil {
		return weight, []string{fmt.Sprintf("weight sha256 failed: %v", err)}
	}
	weight.SHA256 = sha
	var warnings []string
	if manifest.WeightSHA256 != "" && !strings.EqualFold(manifest.WeightSHA256, sha) {
		warnings = append(warnings, "weight_sha256 mismatch")
	}
	header, err := ReadWeightHeader(path)
	if err != nil {
		warnings = append(warnings, err.Error())
		return weight, warnings
	}
	weight.Header = header
	embLen, headLen, biasLen, err := WeightDataLengths(header)
	if err != nil {
		warnings = append(warnings, err.Error())
		return weight, warnings
	}
	wantSize := int64(header.DataOffset) + int64(embLen+headLen+biasLen)
	weight.ExpectedSize = wantSize
	weight.EmbeddingBytes = embLen
	weight.HeadBytes = headLen
	weight.BiasBytes = biasLen
	weight.SparseHashHead = header.Flags&WeightFlagSparseHashHead != 0
	weight.IdentityEmbedding = header.Flags&WeightFlagIdentityEmbedding != 0
	if info.Size() != wantSize {
		warnings = append(warnings, fmt.Sprintf("weight size mismatch: got %d bytes, want %d bytes", info.Size(), wantSize))
	}
	return weight, warnings
}

func artifactFilePath(modelPath, rel string) string {
	base := strings.TrimSpace(modelPath)
	if strings.EqualFold(filepath.Base(base), "manifest.json") {
		base = filepath.Dir(base)
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(base, rel)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type RulePredictor struct{}

func NewRulePredictor() *RulePredictor { return &RulePredictor{} }

func (p *RulePredictor) Predict(ctx context.Context, req Request) (needledata.Decision, error) {
	select {
	case <-ctx.Done():
		return needledata.Decision{}, ctx.Err()
	default:
	}
	switch req.Task {
	case needledata.EventWorkflowReview:
		return predictWorkflowReview(req.Text), nil
	default:
		return needledata.Decision{}, fmt.Errorf("needleruntime: unsupported task %q", req.Task)
	}
}

func predictWorkflowReview(text string) needledata.Decision {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return needledata.Decision{Name: "other", Confidence: 0.5, Source: "needle_rule"}
	}
	cases := []struct {
		name       string
		confidence float64
		terms      []string
	}{
		{name: "confirm", confidence: 0.95, terms: []string{"confirm", "confirmed", "approved", "approve", "looks good", "continue", "next step", "go ahead", "ok"}},
		{name: "supplement", confidence: 0.9, terms: []string{"add ", "change", "revise", "modify", "supplement", "not clear", "more detail", "include"}},
		{name: "skip", confidence: 0.92, terms: []string{"skip", "omit", "not needed", "unnecessary"}},
		{name: "cancel", confidence: 0.93, terms: []string{"cancel", "stop", "abort", "abandon", "end the workflow"}},
		{name: "switch_task", confidence: 0.88, terms: []string{"switch task", "different task", "instead", "change task"}},
	}
	for _, tc := range cases {
		for _, term := range tc.terms {
			if strings.Contains(lower, term) {
				return needledata.Decision{Name: tc.name, Confidence: tc.confidence, Source: "needle_rule"}
			}
		}
	}
	return needledata.Decision{Name: "other", Confidence: 0.65, Source: "needle_rule"}
}

func collectionTaskSet(tasks map[string]CollectionTaskEntry) map[string]bool {
	out := make(map[string]bool, len(tasks))
	for task := range tasks {
		if task = strings.TrimSpace(task); task != "" {
			out[task] = true
		}
	}
	return out
}

func sortedTaskKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
