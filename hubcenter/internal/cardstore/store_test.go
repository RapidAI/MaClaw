package cardstore

import (
	"context"
	"strings"
	"testing"
)

type cardTypeTestRepo struct {
	created []*CardType
	updated []*CardType
}

func (r *cardTypeTestRepo) Create(_ context.Context, ct *CardType) error {
	r.created = append(r.created, ct)
	return nil
}

func (r *cardTypeTestRepo) Update(_ context.Context, ct *CardType) error {
	r.updated = append(r.updated, ct)
	return nil
}

func (r *cardTypeTestRepo) GetByID(_ context.Context, _ string) (*CardType, error) {
	return nil, nil
}

func (r *cardTypeTestRepo) ListEnabled(_ context.Context) ([]*CardType, error) {
	return nil, nil
}

func (r *cardTypeTestRepo) ListAll(_ context.Context) ([]*CardType, error) {
	return nil, nil
}

func (r *cardTypeTestRepo) Delete(_ context.Context, _ string) error {
	return nil
}

func TestUpdateCardTypeRequiresCompleteValidCard(t *testing.T) {
	tests := []struct {
		name string
		card CardType
		want string
	}{
		{
			name: "missing service group",
			card: CardType{ID: "ct1", Label: "Plan", Credits: 100, Period: "month", PriceRMB: 10, Template: "enterprise_monthly_blue"},
			want: "service_group_id is required",
		},
		{
			name: "missing label",
			card: CardType{ID: "ct1", ServiceGroupID: "g1", Credits: 100, Period: "month", PriceRMB: 10, Template: "enterprise_monthly_blue"},
			want: "label is required",
		},
		{
			name: "zero credits",
			card: CardType{ID: "ct1", ServiceGroupID: "g1", Label: "Plan", Period: "month", PriceRMB: 10, Template: "enterprise_monthly_blue"},
			want: "credits must be positive",
		},
		{
			name: "zero price",
			card: CardType{ID: "ct1", ServiceGroupID: "g1", Label: "Plan", Credits: 100, Period: "month", Template: "enterprise_monthly_blue"},
			want: "price must be positive",
		},
		{
			name: "bad period",
			card: CardType{ID: "ct1", ServiceGroupID: "g1", Label: "Plan", Credits: 100, Period: "week", PriceRMB: 10, Template: "enterprise_monthly_blue"},
			want: "period must be month, quarter, or year",
		},
		{
			name: "bad template",
			card: CardType{ID: "ct1", ServiceGroupID: "g1", Label: "Plan", Credits: 100, Period: "month", PriceRMB: 10, Template: "bad"},
			want: "invalid card template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &cardTypeTestRepo{}
			svc := NewService(repo, nil, nil)
			err := svc.UpdateCardType(context.Background(), &tt.card)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
			if len(repo.updated) != 0 {
				t.Fatalf("invalid card was persisted: %#v", repo.updated)
			}
		})
	}
}

func TestUpdateCardTypeNormalizesAndPersistsValidCard(t *testing.T) {
	repo := &cardTypeTestRepo{}
	svc := NewService(repo, nil, nil)
	card := &CardType{
		ID:             " ct1 ",
		ServiceGroupID: " g1 ",
		Label:          " Plan ",
		Description:    " Detail ",
		Credits:        100,
		Period:         " month ",
		PriceRMB:       10,
		Template:       " enterprise_monthly_blue ",
	}

	if err := svc.UpdateCardType(context.Background(), card); err != nil {
		t.Fatalf("UpdateCardType: %v", err)
	}
	if len(repo.updated) != 1 {
		t.Fatalf("updated count = %d, want 1", len(repo.updated))
	}
	got := repo.updated[0]
	if got.ID != "ct1" || got.ServiceGroupID != "g1" || got.Label != "Plan" || got.Description != "Detail" || got.Period != "month" || got.Template != "enterprise_monthly_blue" {
		t.Fatalf("card not normalized: %#v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt was not set")
	}
}

func TestCreateCardTypeDefaultsTemplateAndPersistsValidCard(t *testing.T) {
	repo := &cardTypeTestRepo{}
	svc := NewService(repo, nil, nil)
	card := &CardType{
		ID:             " ct1 ",
		ServiceGroupID: " g1 ",
		Label:          " Plan ",
		Credits:        100,
		Period:         " month ",
		PriceRMB:       10,
	}

	if err := svc.CreateCardType(context.Background(), card); err != nil {
		t.Fatalf("CreateCardType: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created count = %d, want 1", len(repo.created))
	}
	got := repo.created[0]
	if got.Template != "enterprise_monthly_blue" {
		t.Fatalf("default template = %q, want enterprise_monthly_blue", got.Template)
	}
	if got.ID != "ct1" || got.ServiceGroupID != "g1" || got.Label != "Plan" || got.Period != "month" {
		t.Fatalf("card not normalized: %#v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not set: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestCreateCardTypeRejectsInvalidCard(t *testing.T) {
	repo := &cardTypeTestRepo{}
	svc := NewService(repo, nil, nil)
	card := &CardType{ID: "ct1", ServiceGroupID: "g1", Label: "Plan", Credits: 100, Period: "month", PriceRMB: 10, Template: "bad"}

	err := svc.CreateCardType(context.Background(), card)
	if err == nil || !strings.Contains(err.Error(), "invalid card template") {
		t.Fatalf("error = %v, want invalid card template", err)
	}
	if len(repo.created) != 0 {
		t.Fatalf("invalid card was persisted: %#v", repo.created)
	}
}
