package guiapp

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

var hopSpecIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type fileHopSpecProvider struct {
	root string
}

var _ hopjit.SpecProvider = (*fileHopSpecProvider)(nil)

func newFileHopSpecProvider(root string) *fileHopSpecProvider {
	return &fileHopSpecProvider{root: root}
}

func hopJITSpecRoot(dataDir string, identity hopJITExecutionIdentity) string {
	owner := strings.TrimSpace(identity.PrincipalID)
	if owner == "" {
		owner = "local"
	}
	digest := sha256.Sum256([]byte(owner))
	return filepath.Join(dataDir, "hopjit", "specs", fmt.Sprintf("%x", digest[:16]))
}

func validateHopSpecID(id string) error {
	if !hopSpecIDPattern.MatchString(strings.TrimSpace(id)) {
		return errors.New("INVALID_SPEC_ID: use 1-64 letters, digits, dot, underscore, or dash")
	}
	return nil
}

func (p *fileHopSpecProvider) specPath(id string) (string, error) {
	id = strings.TrimSpace(id)
	if err := validateHopSpecID(id); err != nil {
		return "", err
	}
	if p == nil || strings.TrimSpace(p.root) == "" {
		return "", errors.New("HopJIT spec store is unavailable")
	}
	return filepath.Join(p.root, id+".md"), nil
}

func (p *fileHopSpecProvider) LoadSpec(ctx context.Context, id string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := p.specPath(id)
	if err != nil {
		return "", err
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("SPEC_NOT_FOUND: %s", id)
		}
		return "", err
	}
	markdown := string(encoded)
	if err := validateStoredHopSpec(id, markdown); err != nil {
		return "", err
	}
	return markdown, nil
}

func (p *fileHopSpecProvider) SaveSpec(id, markdown string) error {
	id = strings.TrimSpace(id)
	if err := validateStoredHopSpec(id, markdown); err != nil {
		return err
	}
	path, err := p.specPath(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(markdown), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (p *fileHopSpecProvider) DeleteSpec(id string) error {
	path, err := p.specPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("SPEC_NOT_FOUND: %s", strings.TrimSpace(id))
		}
		return err
	}
	return nil
}

func (p *fileHopSpecProvider) ListSpecs() ([]string, error) {
	if p == nil || strings.TrimSpace(p.root) == "" {
		return nil, errors.New("HopJIT spec store is unavailable")
	}
	entries, err := os.ReadDir(p.root)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		if !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".md")
		if validateHopSpecID(id) == nil {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func validateStoredHopSpec(id, markdown string) error {
	if err := validateHopSpecID(id); err != nil {
		return err
	}
	spec, parseErrs := hopspec.Parse(markdown)
	if len(parseErrs) > 0 {
		return errors.Join(parseErrs...)
	}
	if validationErrs := hopspec.Validate(spec); len(validationErrs) > 0 {
		return errors.Join(validationErrs...)
	}
	if spec.ID != "" && spec.ID != id {
		return fmt.Errorf("SPEC_ID_MISMATCH: %s != %s", spec.ID, id)
	}
	return nil
}

func (a *App) hopJITSpecProvider(identity hopJITExecutionIdentity) (*fileHopSpecProvider, error) {
	if a == nil {
		return nil, errors.New("HopJIT app is unavailable")
	}
	return newFileHopSpecProvider(hopJITSpecRoot(a.GetDataDir(), identity)), nil
}

func (c *guiHopJITCaller) specProvider() hopjit.SpecProvider {
	if c == nil {
		return nil
	}
	if c.specs != nil {
		return c.specs
	}
	provider, err := c.app.hopJITSpecProvider(c.identity)
	if err != nil {
		return nil
	}
	return provider
}

func (c *guiHopJITCaller) stepToolProviderFactory() hopjit.StepToolProviderFactory {
	return func(step hopjit.StepReady) (hopjit.ToolProvider, error) {
		if c == nil || c.handler == nil {
			return nil, errors.New("HopJIT tool host is unavailable")
		}
		provider, _, err := c.handler.hopJITActFreeToolProvider(step, c.identity)
		if err != nil {
			return nil, err
		}
		return provider, nil
	}
}

func newHopJITEngine(store hopjit.PersistenceProvider, caller hopjit.ReuseCaller) *hopjit.ExecutionEngine {
	options := []hopjit.EngineOption{hopjit.WithRunOwner(hopJITRunOwnerID(caller))}
	if owner, ok := caller.(interface{ specProvider() hopjit.SpecProvider }); ok {
		if provider := owner.specProvider(); provider != nil {
			options = append(options, hopjit.WithSpecProvider(provider))
		}
	}
	if owner, ok := caller.(interface {
		stepToolProviderFactory() hopjit.StepToolProviderFactory
	}); ok {
		options = append(options, hopjit.WithStepToolProviderFactory(owner.stepToolProviderFactory()))
	}
	return hopjit.NewExecutionEngine(store, options...)
}
