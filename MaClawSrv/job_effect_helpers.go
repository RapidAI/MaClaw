package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// executeRecordedJobEffect wraps a domain worker with the shared protected
// effect ledger. The worker is admitted before any provider/filesystem
// mutation, binds a stable resource identity when one is available, and only
// reports success after the committed receipt is durable. A failure to settle
// after the side effect is deliberately projected as unknown so the async job
// manager cannot retry an operation whose outcome is no longer observable.
//
// result and resourceID are kept separate because the generic Job envelope
// must remain transport-safe while the effect repository may retain private
// domain correlation data.
func executeRecordedJobEffect(
	ctx context.Context,
	kind string,
	preparation any,
	uncertainOnError bool,
	run func(context.Context) (result any, resourceID string, err error),
) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	effect, err := agentruntime.PrepareJobEffect(ctx, kind, preparation)
	if err != nil {
		return nil, err
	}
	result, resourceID, runErr := run(ctx)
	if runErr != nil {
		// Cancellation is handled by the manager and leaves the prepared effect
		// for a later read-only reconciliation. Do not attempt a write with a
		// canceled context.
		if errors.Is(runErr, context.Canceled) {
			return nil, runErr
		}
		if errors.Is(runErr, context.DeadlineExceeded) && uncertainOnError {
			return nil, errors.Join(agentruntime.ErrJobEffectOutcomeUncertain, runErr)
		}
		state := agentruntime.JobEffectFailed
		if uncertainOnError || errors.Is(runErr, agentruntime.ErrJobEffectOutcomeUncertain) {
			state = agentruntime.JobEffectUnknown
		}
		if _, settleErr := agentruntime.SettleJobEffect(ctx, kind, state, "", effectReason(runErr, state), nil); settleErr != nil {
			return nil, errors.Join(agentruntime.ErrJobEffectOutcomeUncertain, settleErr)
		}
		if state == agentruntime.JobEffectUnknown {
			return nil, errors.Join(agentruntime.ErrJobEffectOutcomeUncertain, runErr)
		}
		return nil, runErr
	}

	resourceID = strings.TrimSpace(resourceID)
	if resourceID != "" {
		if _, err := agentruntime.BindJobEffectResource(ctx, kind, resourceID); err != nil {
			return nil, errors.Join(agentruntime.ErrJobEffectOutcomeUncertain, err)
		}
	}
	// The result is stored in the protected effect payload for evidence-backed
	// replay. It is never returned by the generic effect API or exposed in the
	// async Job JSON until the reconciler explicitly projects it.
	if _, err := agentruntime.SettleJobEffect(ctx, kind, agentruntime.JobEffectCommitted, effectReceipt(effect, resourceID), "", result); err != nil {
		return nil, errors.Join(agentruntime.ErrJobEffectOutcomeUncertain, err)
	}
	return result, nil
}

func effectReceipt(effect agentruntime.JobEffect, resourceID string) string {
	return fmt.Sprintf("%s:%s:%d", effect.Kind, strings.TrimSpace(resourceID), effect.Version)
}

func effectReason(err error, state agentruntime.JobEffectState) string {
	if state == agentruntime.JobEffectUnknown {
		return "effect_outcome_unknown"
	}
	if err == nil {
		return "effect_failed"
	}
	// Keep reason codes stable and free of provider/path data. The full error
	// remains in the ordinary redacted Job failure projection where appropriate.
	if errors.Is(err, context.Canceled) {
		return agentruntime.JobErrorCodeCanceled
	}
	return "effect_failed"
}

// skillEffectResourceID encodes one or more installed skill names without
// retaining archive bytes, credentials, or local paths in the protected
// effect payload. JSON keeps the identity unambiguous when an archive contains
// several packages.
func skillEffectResourceID(names []string) string {
	clean := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		clean = append(clean, name)
	}
	sort.Strings(clean)
	if len(clean) == 0 {
		return ""
	}
	if len(clean) == 1 {
		return clean[0]
	}
	raw, _ := json.Marshal(clean)
	return string(raw)
}

func shortSkillArchiveDigest(encoded string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(encoded)))
	return hex.EncodeToString(sum[:])
}

func shortKnowledgePackageDigest(pkg knowledgePackage) string {
	raw, err := json.Marshal(pkg)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func knowledgeUploadDigest(uploads []uploadedKnowledgeFile) (string, error) {
	h := sha256.New()
	for _, upload := range uploads {
		name := strings.TrimSpace(upload.Name)
		if _, err := io.WriteString(h, name+"\x00"); err != nil {
			return "", err
		}
		file, err := os.Open(upload.Path)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(h, file); err != nil {
			_ = file.Close()
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func knowledgeEffectResourceID(result any) string {
	ids := make([]string, 0)
	seen := make(map[string]struct{})
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	var walk func(any)
	walk = func(value any) {
		switch item := value.(type) {
		case knowledge.DirectoryImportResult:
			for _, entry := range item.Items {
				add(entry.SourceID)
			}
			add(item.BatchID)
		case *knowledge.DirectoryImportResult:
			if item != nil {
				walk(*item)
			}
		case knowledge.URLBatchSaveResult:
			for _, entry := range item.Items {
				add(entry.SourceID)
			}
			for _, source := range item.Sources {
				add(source.ID)
			}
		case *knowledge.URLBatchSaveResult:
			if item != nil {
				walk(*item)
			}
		case knowledge.DeepCrawlResult:
			for _, entry := range item.Items {
				add(entry.SourceID)
			}
			add(item.JobID)
		case *knowledge.DeepCrawlResult:
			if item != nil {
				walk(*item)
			}
		case knowledge.PackageImportResult:
			for _, id := range item.ImportedSourceIDs {
				add(id)
			}
			for _, id := range item.SkippedSourceIDs {
				add(id)
			}
			for _, id := range item.FailedSourceIDs {
				add(id)
			}
		case *knowledge.PackageImportResult:
			if item != nil {
				walk(*item)
			}
		case knowledge.Source:
			add(item.ID)
		case *knowledge.Source:
			if item != nil {
				add(item.ID)
			}
		case map[string]any:
			for _, nested := range item {
				walk(nested)
			}
		case []knowledge.DeepCrawlResult:
			for _, nested := range item {
				walk(nested)
			}
		}
	}
	walk(result)
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	if len(ids) == 1 {
		return ids[0]
	}
	raw, _ := json.Marshal(ids)
	return string(raw)
}

// sanitizeKnowledgeJobResult keeps the protected replay payload safe even
// when a worker completed the import but crashed before the generic Job result
// redaction path ran. Reconciliation may project this payload directly.
func sanitizeKnowledgeJobResult(dataRoot string, result any) any {
	switch item := result.(type) {
	case knowledge.DirectoryImportResult:
		return sanitizeKnowledgeDirectoryImportResultForAPI(dataRoot, item)
	case *knowledge.DirectoryImportResult:
		if item == nil {
			return (*knowledge.DirectoryImportResult)(nil)
		}
		copy := sanitizeKnowledgeDirectoryImportResultForAPI(dataRoot, *item)
		return &copy
	case knowledge.URLBatchSaveResult:
		for i := range item.Items {
			item.Items[i].URL = redactKnowledgeURIForAPI(dataRoot, item.Items[i].URL)
			item.Items[i].Error = redactSupportBundleText(dataRoot, item.Items[i].Error)
		}
		for i := range item.Sources {
			item.Sources[i] = sanitizeKnowledgeSourceForAPI(dataRoot, item.Sources[i])
		}
		return item
	case *knowledge.URLBatchSaveResult:
		if item == nil {
			return (*knowledge.URLBatchSaveResult)(nil)
		}
		copy := sanitizeKnowledgeJobResult(dataRoot, *item)
		return copy
	case knowledge.DeepCrawlResult:
		for i := range item.Items {
			item.Items[i].URL = redactKnowledgeURIForAPI(dataRoot, item.Items[i].URL)
			item.Items[i].Error = redactSupportBundleText(dataRoot, item.Items[i].Error)
		}
		for i := range item.ByDepth {
			for j := range item.ByDepth[i].URLs {
				item.ByDepth[i].URLs[j] = redactKnowledgeURIForAPI(dataRoot, item.ByDepth[i].URLs[j])
			}
		}
		return item
	case *knowledge.DeepCrawlResult:
		if item == nil {
			return (*knowledge.DeepCrawlResult)(nil)
		}
		copy := sanitizeKnowledgeJobResult(dataRoot, *item)
		return copy
	case map[string]any:
		out := make(map[string]any, len(item))
		for key, value := range item {
			out[key] = sanitizeKnowledgeJobResult(dataRoot, value)
		}
		return out
	case []knowledge.DeepCrawlResult:
		out := make([]knowledge.DeepCrawlResult, len(item))
		for i := range item {
			out[i] = sanitizeKnowledgeJobResult(dataRoot, item[i]).(knowledge.DeepCrawlResult)
		}
		return out
	case knowledge.PackageImportResult:
		return item
	case *knowledge.PackageImportResult:
		if item == nil {
			return (*knowledge.PackageImportResult)(nil)
		}
		return *item
	case knowledge.Source:
		return sanitizeKnowledgeSourceForAPI(dataRoot, item)
	case *knowledge.Source:
		if item == nil {
			return (*knowledge.Source)(nil)
		}
		copy := sanitizeKnowledgeSourceForAPI(dataRoot, *item)
		return &copy
	default:
		return result
	}
}
