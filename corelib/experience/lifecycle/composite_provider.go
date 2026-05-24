package lifecycle

import (
	"context"
	"errors"
)

type CompositeProvider struct {
	Providers []Provider
}

func NewCompositeProvider(providers ...Provider) CompositeProvider {
	return CompositeProvider{Providers: append([]Provider(nil), providers...)}
}

func (p CompositeProvider) ListExperience(ctx context.Context, scope Scope) ([]Entry, error) {
	var out []Entry
	var errs []error
	for _, provider := range p.Providers {
		if provider == nil {
			continue
		}
		entries, err := provider.ListExperience(ctx, scope)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, entries...)
	}
	return limitEntries(out, scope.Limit), errors.Join(errs...)
}

func (p CompositeProvider) SearchExperience(ctx context.Context, query Query) ([]Candidate, error) {
	var out []Candidate
	var errs []error
	for _, provider := range p.Providers {
		if provider == nil {
			continue
		}
		candidates, err := provider.SearchExperience(ctx, query)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, candidates...)
	}
	return out, errors.Join(errs...)
}

func (p CompositeProvider) UpdateUtility(ctx context.Context, update UtilityUpdate) error {
	var errs []error
	for _, provider := range p.Providers {
		if provider == nil {
			continue
		}
		if err := provider.UpdateUtility(ctx, update); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func limitEntries(entries []Entry, limit int) []Entry {
	if limit <= 0 || len(entries) <= limit {
		return entries
	}
	return append([]Entry(nil), entries[:limit]...)
}
