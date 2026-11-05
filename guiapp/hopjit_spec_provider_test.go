package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
)

const hopJITChildSpecFixture = `# Spec: 子规约
Id: child
Goal: 生成子结果
Outputs:
- result % text
## Steps
1. [reason] 生成
  + → result % text
`

func TestFileHopSpecProviderSaveLoadAndList(t *testing.T) {
	provider := newFileHopSpecProvider(t.TempDir())
	if err := provider.SaveSpec("child", hopJITChildSpecFixture); err != nil {
		t.Fatal(err)
	}
	loaded, err := provider.LoadSpec(context.Background(), "child")
	if err != nil || loaded != hopJITChildSpecFixture {
		t.Fatalf("loaded=%q err=%v", loaded, err)
	}
	ids, err := provider.ListSpecs()
	if err != nil || len(ids) != 1 || ids[0] != "child" {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	for _, id := range []string{"", "../secret", "a/b", ".hidden"} {
		if _, err := provider.LoadSpec(context.Background(), id); err == nil {
			t.Fatalf("invalid id %q was accepted", id)
		}
	}
	mismatch := strings.Replace(hopJITChildSpecFixture, "Id: child", "Id: other", 1)
	if err := provider.SaveSpec("child", mismatch); err == nil || !strings.Contains(err.Error(), "SPEC_ID_MISMATCH") {
		t.Fatalf("mismatch err=%v", err)
	}
	if err := provider.DeleteSpec("child"); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.LoadSpec(context.Background(), "child"); err == nil || !strings.Contains(err.Error(), "SPEC_NOT_FOUND") {
		t.Fatalf("deleted spec load err=%v", err)
	}
	ids, err = provider.ListSpecs()
	if err != nil || len(ids) != 0 {
		t.Fatalf("ids after delete=%v err=%v", ids, err)
	}
	if err := provider.DeleteSpec("child"); err == nil {
		t.Fatal("deleting a missing spec did not fail")
	}
}

func TestGUIHopJITCallUsesRegisteredFileSpec(t *testing.T) {
	provider := newFileHopSpecProvider(t.TempDir())
	if err := provider.SaveSpec("child", hopJITChildSpecFixture); err != nil {
		t.Fatal(err)
	}
	caller := &guiHopJITCaller{
		identity: hopJITExecutionIdentity{PrincipalID: "user-a"}, specs: provider,
		invoke: func(context.Context, []interface{}, bool) (string, error) {
			return `{"result":"来自子规约"}`, nil
		},
	}
	result, err := runHopSpecWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), `# Spec: 父规约
Goal: 调用已注册子规约
Outputs:
- report % text
## Steps
1. [call child] 调用
  + → report: result
`, nil, caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["report"] != "来自子规约" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Snapshot.OwnerID != "user-a" {
		t.Fatalf("owner=%q", result.Snapshot.OwnerID)
	}
}

func TestHopJITSpecRootSeparatesOwners(t *testing.T) {
	rootA := hopJITSpecRoot("data", hopJITExecutionIdentity{PrincipalID: "user-a"})
	rootB := hopJITSpecRoot("data", hopJITExecutionIdentity{PrincipalID: "user-b"})
	if rootA == rootB || strings.Contains(rootA, "user-a") || strings.Contains(rootB, "user-b") {
		t.Fatalf("rootA=%q rootB=%q", rootA, rootB)
	}
}
