package database

import (
	"context"
	"strings"
	"testing"
)

func TestReplicaProfileIsReadOnlyAndUsesReplicaHost(t *testing.T) {
	p := replicaProfile(Profile{
		Type: SourcePostgres, Host: "primary", Port: 5432, Database: "crm",
		ReplicaHost: "replica.internal", ReplicaPort: 5433, WriteEnabled: true, ReadOnly: false,
		SSHSessionID: "ssh-primary", ReplicaSSHSessionID: "ssh-replica",
	})
	if p.Host != "replica.internal" || p.Port != 5433 {
		t.Fatalf("replica profile = %+v", p)
	}
	if !p.ReadOnly || p.WriteEnabled || p.ReplicaHost != "" || p.ReplicaSSHSessionID != "" {
		t.Fatalf("replica must be read-only and not nested: %+v", p)
	}
	if p.SSHSessionID != "ssh-replica" {
		t.Fatalf("replica ssh = %q", p.SSHSessionID)
	}
	inherited := replicaProfile(Profile{Host: "primary", ReplicaHost: "replica.internal", SSHSessionID: "ssh-primary"})
	if inherited.SSHSessionID != "ssh-primary" {
		t.Fatalf("unset replica ssh must inherit primary tunnel: %q", inherited.SSHSessionID)
	}
}

func TestValidateProfileRejectsReplicaOnExcel(t *testing.T) {
	err := ValidateProfile(Profile{ID: "x", Type: SourceExcel, FilePath: "a.xlsx", ReplicaHost: "db"})
	if err == nil || !strings.Contains(err.Error(), "unsupported_capability") {
		t.Fatalf("err = %v", err)
	}
}

func TestRoutedAdapterReadsReplicaWritesPrimary(t *testing.T) {
	primary := &toolTestAdapter{}
	replica := &toolTestAdapter{}
	routed := &routedAdapter{primary: primary, replica: replica}
	if _, err := routed.Query(context.Background(), QueryRequest{SQL: "select 1"}); err != nil {
		t.Fatal(err)
	}
	if replica.queryRequest.SQL != "select 1" {
		t.Fatalf("query must hit replica, got %q", replica.queryRequest.SQL)
	}
	if primary.queryRequest.SQL != "" {
		t.Fatal("query must not hit primary when replica is healthy")
	}
	if _, err := routed.Execute(context.Background(), ExecuteRequest{SQL: "update t set a=1 where id=1", DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if primary.executeRequest.SQL == "" {
		t.Fatal("execute must hit primary")
	}
	if replica.executeRequest.SQL != "" {
		t.Fatal("execute must not hit replica")
	}
}

func TestRoutedAdapterFallsBackWhenReplicaQueryFails(t *testing.T) {
	primary := &toolTestAdapter{}
	replica := &failingQueryAdapter{}
	routed := &routedAdapter{primary: primary, replica: replica}
	out, err := routed.Query(context.Background(), QueryRequest{SQL: "select 1"})
	if err != nil {
		t.Fatal(err)
	}
	if primary.queryRequest.SQL != "select 1" {
		t.Fatal("fallback must query primary")
	}
	if !hasWarning(out.Warnings, "replica_fallback") {
		t.Fatalf("warnings = %v", out.Warnings)
	}
}

type failingQueryAdapter struct{ toolTestAdapter }

func (a *failingQueryAdapter) Query(context.Context, QueryRequest) (QueryResult, error) {
	return QueryResult{}, context.Canceled
}

type failingInspectAdapter struct{ toolTestAdapter }

func (a *failingInspectAdapter) Inspect(context.Context, InspectRequest) (SchemaInfo, error) {
	return SchemaInfo{}, context.Canceled
}

func TestRoutedAdapterInspectFallsBackAndWarns(t *testing.T) {
	primary := &toolTestAdapter{}
	replica := &failingInspectAdapter{}
	routed := &routedAdapter{primary: primary, replica: replica}
	info, err := routed.Inspect(context.Background(), InspectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(info.Warnings, "replica_fallback") {
		t.Fatalf("warnings = %v", info.Warnings)
	}
}

func TestRoutedAdapterInspectWarnsWhenReplicaUnavailable(t *testing.T) {
	primary := &toolTestAdapter{}
	routed := &routedAdapter{primary: primary, replicaUnavailable: true}
	info, err := routed.Inspect(context.Background(), InspectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(info.Warnings, "replica_unavailable") {
		t.Fatalf("warnings = %v", info.Warnings)
	}
}
