package llmservice

import (
	"context"
	"testing"
)

func TestResolveEffectiveModelRolesSingleModelFallback(t *testing.T) {
	got := ResolveEffectiveModelRoles(ModelRoles{}, ModelRoles{}, "gpt-5", 1)
	want := ModelRoles{Foreground: "gpt-5", Background: "gpt-5", ComputerUse: "gpt-5"}
	if got != want {
		t.Fatalf("single model roles = %+v, want %+v", got, want)
	}
}

func TestResolveEffectiveModelRolesMultiModelStaysAuto(t *testing.T) {
	got := ResolveEffectiveModelRoles(ModelRoles{}, ModelRoles{}, "gpt-5", 3)
	want := ModelRoles{Foreground: ModelRoleAuto, Background: ModelRoleAuto, ComputerUse: ModelRoleAuto}
	if got != want {
		t.Fatalf("multi model roles = %+v, want %+v", got, want)
	}
}

func TestResolveEffectiveModelRolesEmptyCatalogIsAuto(t *testing.T) {
	got := ResolveEffectiveModelRoles(ModelRoles{}, ModelRoles{}, "", 0)
	if got.Foreground != ModelRoleAuto || got.Background != ModelRoleAuto || got.ComputerUse != ModelRoleAuto {
		t.Fatalf("empty catalog roles = %+v", got)
	}
}

func TestResolveEffectiveModelRolesUserPinBeatsTenantAndFallback(t *testing.T) {
	defaults := ModelRoles{Foreground: "tenant-fg", Background: "tenant-bg"}
	user := ModelRoles{Foreground: "user-fg", ComputerUse: "Default"}
	got := ResolveEffectiveModelRoles(defaults, user, "only-model", 1)
	if got.Foreground != "user-fg" {
		t.Fatalf("foreground = %q", got.Foreground)
	}
	if got.Background != "tenant-bg" {
		t.Fatalf("background = %q", got.Background)
	}
	if got.ComputerUse != ModelRoleAuto {
		t.Fatalf("computer_use = %q, want auto", got.ComputerUse)
	}
}

func TestMergeUserBindingsKeepsRoleOnlyBinding(t *testing.T) {
	reg := &Registry{UserBindings: []UserBinding{
		{Email: "role@example.com", ModelRoles: ModelRoles{Background: "fast-model"}},
		{Email: "role@example.com", ServiceGroupIDs: []string{"coding"}},
		{Email: "groups@example.com", ServiceGroupIDs: []string{}},
	}}
	reg.Normalize()
	if len(reg.UserBindings) != 1 {
		t.Fatalf("bindings = %+v, want the role user only", reg.UserBindings)
	}
	got := reg.UserBindings[0]
	if got.Email != "role@example.com" {
		t.Fatalf("email = %q", got.Email)
	}
	if len(got.ServiceGroupIDs) != 1 || got.ServiceGroupIDs[0] != "coding" {
		t.Fatalf("groups = %#v", got.ServiceGroupIDs)
	}
	if got.ModelRoles.Background != "fast-model" {
		t.Fatalf("background pin = %+v", got.ModelRoles)
	}
}

func TestPreserveOmittedModelRoles(t *testing.T) {
	old := &Registry{
		DefaultModelRoles: ModelRoles{Foreground: "strong"},
		UserBindings: []UserBinding{{
			Email:           "user@example.com",
			ServiceGroupIDs: []string{"coding"},
			ModelRoles:      ModelRoles{ComputerUse: "vision"},
		}},
	}
	req := &Registry{UserBindings: []UserBinding{{
		Email:           "user@example.com",
		ServiceGroupIDs: []string{"coding"},
	}}}
	PreserveOmittedDefaultModelRoles(req, old, false)
	PreserveOmittedUserModelRoles(req, old, []bool{false})
	if req.DefaultModelRoles.Foreground != "strong" {
		t.Fatalf("tenant foreground = %+v", req.DefaultModelRoles)
	}
	if req.UserBindings[0].ModelRoles.ComputerUse != "vision" {
		t.Fatalf("user computer_use = %+v", req.UserBindings[0].ModelRoles)
	}

	req.DefaultModelRoles = ModelRoles{Foreground: "pinned"}
	req.UserBindings[0].ModelRoles = ModelRoles{Foreground: "mine"}
	PreserveOmittedDefaultModelRoles(req, old, true)
	PreserveOmittedUserModelRoles(req, old, []bool{true})
	if req.DefaultModelRoles.Foreground != "pinned" || req.UserBindings[0].ModelRoles.Foreground != "mine" {
		t.Fatalf("explicit pins were overwritten: %+v %+v", req.DefaultModelRoles, req.UserBindings[0].ModelRoles)
	}
}

func TestServiceStatusFillsRoleModels(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "coding",
			Name: "Coding",
			Models: []ModelServiceModel{{
				Name:        "only-model",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		GlobalServiceGroupIDs: []string{"coding"},
		DefaultModelRoles:     ModelRoles{Foreground: "default"},
		UserBindings: []UserBinding{{
			Email:      "user@example.com",
			ModelRoles: ModelRoles{Background: "worker"},
		}},
	}
	status, _, err := ResolveStatusFromRegistryForUser(context.Background(), reg, nil, "", "user@example.com", "https://hub.example")
	if err != nil {
		t.Fatal(err)
	}
	if status.RoleModels.Foreground != ModelRoleAuto {
		t.Fatalf("foreground = %q, want auto", status.RoleModels.Foreground)
	}
	if status.RoleModels.Background != "worker" {
		t.Fatalf("background = %q", status.RoleModels.Background)
	}
	if status.RoleModels.ComputerUse != "only-model" {
		t.Fatalf("computer_use = %q, want the single model", status.RoleModels.ComputerUse)
	}
	if status.DefaultModel != "only-model" {
		t.Fatalf("default_model = %q", status.DefaultModel)
	}
}
