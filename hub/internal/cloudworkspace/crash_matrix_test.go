package cloudworkspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

const crashMatrixExitCode = 86

var crashMatrixCases = []string{
	"object_whole",
	"object_chunk",
	"object_finalize",
	"manifest_put",
	"manifest_delta",
	"snapshot_restore",
	"sidecar",
	"task_binding_put",
	"task_binding_delete",
	"lease_acquire",
	"lease_handoff",
	"lease_heartbeat",
	"lease_release",
	"provisioning_begin",
	"provisioning_complete",
	"provisioning_abort",
}

type crashMatrixEnv struct {
	provider  *sqlite.Provider
	store     *Store
	service   *Service
	principal auth.MachinePrincipal
	workspace string
}

func openCrashMatrixEnv(dsn, root, keyDir string) (*crashMatrixEnv, error) {
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               dsn,
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 4,
		MaxWriteIdleConns: 2,
	})
	if err != nil {
		return nil, err
	}
	st := NewStore(provider.Write)
	blobs := &BlobStore{Root: root, KeyDir: keyDir, DB: provider.Write}
	return &crashMatrixEnv{
		provider: provider,
		store:    st,
		service:  &Service{Workspaces: st, Blobs: blobs},
		principal: auth.MachinePrincipal{
			TenantID: "t1", UserID: "u1", MachineID: "m1", ClientInstanceID: "cwi-matrix",
		},
	}, nil
}

func (e *crashMatrixEnv) close() {
	if e != nil && e.provider != nil {
		_ = e.provider.Close()
	}
}

func setupCrashMatrixCase(e *crashMatrixEnv, operation string) error {
	if err := sqlite.RunMigrations(e.provider.Write); err != nil {
		return err
	}
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := e.store.db.ExecContext(ctx, `INSERT INTO machines (id, tenant_id, user_id, name, platform, hostname, machine_token_hash, status, created_at, updated_at)
		VALUES ('m1', 't1', 'u1', 'matrix', 'windows', 'MATRIX', 'hash', 'online', ?, ?)`, now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		return err
	}
	if strings.HasPrefix(operation, "provisioning_") {
		if operation == "provisioning_begin" {
			return nil
		}
		op, err := e.service.BeginWorkspaceTaskProvision(ctx, e.principal, WorkspaceTaskProvisionParams{Name: "matrix-provision", DeviceTaskID: "device-task", Mode: "coding_dev", IdempotencyKey: "setup-" + operation, IdempotencyPayloadHash: "setup-payload-" + operation})
		if err != nil {
			return err
		}
		e.workspace = op.WorkspaceID
		lease, err := e.store.Acquire(ctx, AcquireParams{TenantID: "t1", UserID: "u1", WorkspaceID: op.WorkspaceID, MachineID: "m1", ClientInstanceID: "cwi-matrix"}, now)
		if err != nil {
			return err
		}
		e.principal.FencingToken = lease.FencingToken
		return nil
	}
	ws, err := e.store.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "matrix", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		return err
	}
	e.workspace = ws.ID
	if operation != "lease_acquire" {
		lease, err := e.store.Acquire(ctx, AcquireParams{TenantID: "t1", UserID: "u1", WorkspaceID: ws.ID, MachineID: "m1", ClientInstanceID: "cwi-matrix"}, now)
		if err != nil {
			return err
		}
		e.principal.FencingToken = lease.FencingToken
	}

	put := func(body string) (PutResult, error) {
		plain := []byte(body)
		return e.service.PutObject(ctx, e.principal, ws.ID, plaintextSHA256(plain), plain)
	}
	switch operation {
	case "object_finalize":
		plain := []byte("finalize-data")
		return e.service.PutObjectChunk(ctx, e.principal, ws.ID, plaintextSHA256(plain), 0, plain)
	case "manifest_put":
		_, err = put("alpha")
		return err
	case "manifest_delta":
		alpha, putErr := put("alpha")
		if putErr != nil {
			return putErr
		}
		if _, putErr = e.service.PutManifest(ctx, e.principal, ws.ID, "", []ManifestEntry{{Path: "a.txt", SHA256: alpha.SHA256, Size: alpha.SizeBytes}}); putErr != nil {
			return putErr
		}
		_, err = put("beta")
		return err
	case "snapshot_restore":
		alpha, putErr := put("alpha")
		if putErr != nil {
			return putErr
		}
		first, putErr := e.service.PutManifest(ctx, e.principal, ws.ID, "", []ManifestEntry{{Path: "a.txt", SHA256: alpha.SHA256, Size: alpha.SizeBytes}})
		if putErr != nil {
			return putErr
		}
		beta, putErr := put("beta")
		if putErr != nil {
			return putErr
		}
		_, putErr = e.service.PutManifest(ctx, e.principal, ws.ID, first.Revision, []ManifestEntry{{Path: "b.txt", SHA256: beta.SHA256, Size: beta.SizeBytes}})
		return putErr
	case "task_binding_delete":
		_, err = e.service.UpsertTaskBinding(ctx, e.principal, ws.ID, "", "device-task", "matrix", "coding_dev", "matrix", 0)
		return err
	default:
		return nil
	}
}

func loadCrashMatrixWorkspace(ctx context.Context, e *crashMatrixEnv, operation string) error {
	if operation == "provisioning_begin" {
		return nil
	}
	if strings.HasPrefix(operation, "provisioning_") {
		if err := e.store.db.QueryRowContext(ctx, `SELECT workspace_id FROM cloud_workspace_task_provisions WHERE tenant_id = 't1' AND user_id = 'u1' ORDER BY created_at LIMIT 1`).Scan(&e.workspace); err != nil {
			return err
		}
	} else if err := e.store.db.QueryRowContext(ctx, `SELECT id FROM cloud_workspaces WHERE tenant_id = 't1' AND user_id = 'u1' ORDER BY created_at LIMIT 1`).Scan(&e.workspace); err != nil {
		return err
	}
	if operation == "lease_acquire" {
		return nil
	}
	lease, err := getActiveLease(ctx, e.store.db, e.workspace)
	if err != nil {
		return err
	}
	if lease == nil {
		if operation == "lease_release" || operation == "provisioning_abort" {
			return nil
		}
		return ErrLeaseRequired
	}
	e.principal.FencingToken = lease.FencingToken
	return nil
}

func crashMatrixAtomicContext(ctx context.Context, e *crashMatrixEnv, operation string) (context.Context, *IdempotencyRecord, error) {
	workspaceID := e.workspace
	if strings.HasPrefix(operation, "provisioning_") {
		workspaceID = ""
	}
	encoder := func(value any) (int, []byte, error) {
		raw, err := json.Marshal(value)
		return 200, raw, err
	}
	return e.store.PrepareAtomicIdempotency(ctx, "t1", "u1", workspaceID, "cwi-matrix", "matrix:"+operation, "payload:"+operation, time.Now().UTC(), encoder)
}

func executeCrashMatrixOperation(ctx context.Context, e *crashMatrixEnv, operation string) error {
	switch operation {
	case "object_whole":
		plain := []byte("whole-data")
		_, err := e.service.PutObject(ctx, e.principal, e.workspace, plaintextSHA256(plain), plain)
		return err
	case "object_chunk":
		plain := []byte("chunk-data")
		return e.service.PutObjectChunk(ctx, e.principal, e.workspace, plaintextSHA256(plain), 0, plain)
	case "object_finalize":
		plain := []byte("finalize-data")
		_, err := e.service.CompleteObject(ctx, e.principal, e.workspace, plaintextSHA256(plain))
		return err
	case "manifest_put":
		plain := []byte("alpha")
		current, err := e.service.GetManifest(ctx, e.principal, e.workspace)
		if err != nil {
			return err
		}
		_, err = e.service.PutManifest(ctx, e.principal, e.workspace, current.Revision, []ManifestEntry{{Path: "a.txt", SHA256: plaintextSHA256(plain), Size: int64(len(plain))}})
		return err
	case "manifest_delta":
		plain := []byte("beta")
		current, err := e.service.GetManifest(ctx, e.principal, e.workspace)
		if err != nil {
			return err
		}
		_, err = e.service.ApplyManifestDelta(ctx, e.principal, e.workspace, ManifestDelta{IfMatchRevision: current.Revision, Puts: []ManifestEntry{{Path: "b.txt", SHA256: plaintextSHA256(plain), Size: int64(len(plain))}}})
		return err
	case "snapshot_restore":
		current, err := e.service.GetManifest(ctx, e.principal, e.workspace)
		if err != nil {
			return err
		}
		var snapshotID string
		if err := e.store.db.QueryRowContext(ctx, `SELECT snapshot_id FROM cloud_workspace_snapshots WHERE workspace_id = ? ORDER BY rowid LIMIT 1`, e.workspace).Scan(&snapshotID); err != nil {
			return err
		}
		_, err = e.service.RestoreSnapshot(ctx, e.principal, e.workspace, snapshotID, current.Revision)
		return err
	case "sidecar":
		// A retry after a crash re-reads the canonical file's revision and
		// CASes against it, exactly like a real client retrying with If-Match.
		ifMatch := ""
		current, getErr := e.service.GetSidecarWithRevision(ctx, e.principal, e.workspace, SidecarSession)
		if getErr == nil {
			ifMatch = current.Revision
		} else if getErr != ErrBlobNotFound {
			return getErr
		}
		_, err := e.service.PutSidecarWithRevision(ctx, e.principal, e.workspace, SidecarSession, ifMatch, []byte(`{"session":"matrix"}`))
		return err
	case "task_binding_put":
		_, err := e.service.UpsertTaskBinding(ctx, e.principal, e.workspace, "", "device-task", "matrix", "coding_dev", "matrix", 0)
		return err
	case "task_binding_delete":
		return e.service.DeleteTaskBinding(ctx, e.principal, e.workspace, 1)
	case "lease_acquire":
		_, err := e.service.AcquireLease(ctx, e.principal, e.workspace, false)
		return err
	case "lease_handoff":
		_, err := e.service.RequestLeaseHandoff(ctx, e.principal, e.workspace)
		return err
	case "lease_heartbeat":
		lease, err := getActiveLease(ctx, e.store.db, e.workspace)
		if err != nil {
			return err
		}
		if lease == nil {
			return ErrLeaseRequired
		}
		_, err = e.service.HeartbeatLease(ctx, e.principal, e.workspace, lease.ID)
		return err
	case "lease_release":
		lease, err := getActiveLease(ctx, e.store.db, e.workspace)
		if err != nil {
			return err
		}
		if lease == nil {
			return ErrLeaseRequired
		}
		return e.service.ReleaseLease(ctx, e.principal, e.workspace, lease.ID)
	case "provisioning_begin":
		_, err := e.service.BeginWorkspaceTaskProvision(ctx, e.principal, WorkspaceTaskProvisionParams{Name: "matrix-provision", DeviceTaskID: "device-task", Mode: "coding_dev", IdempotencyKey: "domain-matrix", IdempotencyPayloadHash: "payload:provisioning"})
		return err
	case "provisioning_complete", "provisioning_abort":
		var operationID string
		if err := e.store.db.QueryRowContext(ctx, `SELECT operation_id FROM cloud_workspace_task_provisions WHERE workspace_id = ?`, e.workspace).Scan(&operationID); err != nil {
			return err
		}
		if operation == "provisioning_complete" {
			_, err := e.service.CompleteWorkspaceTaskProvision(ctx, e.principal, operationID)
			return err
		}
		_, err := e.service.AbortWorkspaceTaskProvision(ctx, e.principal, operationID, "matrix abort")
		return err
	default:
		return fmt.Errorf("unknown crash matrix operation %q", operation)
	}
}

func verifyCrashMatrixState(ctx context.Context, e *crashMatrixEnv, operation string) error {
	var committed int
	workspaceID := e.workspace
	if strings.HasPrefix(operation, "provisioning_") {
		workspaceID = ""
	}
	if err := e.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_idempotency WHERE tenant_id = 't1' AND user_id = 'u1' AND workspace_id = ? AND idempotency_key = ? AND status = 'committed'`, workspaceID, "matrix:"+operation).Scan(&committed); err != nil {
		return err
	}
	if committed != 1 {
		return fmt.Errorf("committed receipts=%d", committed)
	}
	switch operation {
	case "object_whole", "object_finalize":
		var ready int
		if err := e.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_objects WHERE workspace_id = ? AND COALESCE(object_state, 'ready') = 'ready'`, e.workspace).Scan(&ready); err != nil {
			return err
		}
		if ready != 1 {
			return fmt.Errorf("ready objects=%d", ready)
		}
	case "object_chunk":
		var chunks int
		if err := e.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_staging_chunks WHERE workspace_id = ?`, e.workspace).Scan(&chunks); err != nil {
			return err
		}
		if chunks != 1 {
			return fmt.Errorf("staging chunks=%d", chunks)
		}
	case "manifest_put", "manifest_delta", "snapshot_restore":
		manifest, err := e.service.GetManifest(ctx, e.principal, e.workspace)
		if err != nil {
			return err
		}
		if manifest.Revision == "" || len(manifest.Entries) == 0 {
			return fmt.Errorf("manifest not committed: %+v", manifest)
		}
	case "sidecar":
		var revision string
		if err := e.store.db.QueryRowContext(ctx, `SELECT revision FROM cloud_workspace_sidecars WHERE workspace_id = ? AND name = ?`, e.workspace, SidecarSession).Scan(&revision); err != nil {
			return err
		}
		if strings.TrimSpace(revision) == "" {
			return fmt.Errorf("sidecar revision is empty")
		}
	case "task_binding_put":
		var version int64
		if err := e.store.db.QueryRowContext(ctx, `SELECT version FROM cloud_workspace_task_bindings WHERE workspace_id = ?`, e.workspace).Scan(&version); err != nil {
			return err
		}
		if version != 1 {
			return fmt.Errorf("binding version=%d", version)
		}
	case "task_binding_delete":
		var bindings int
		if err := e.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_task_bindings WHERE workspace_id = ?`, e.workspace).Scan(&bindings); err != nil {
			return err
		}
		if bindings != 0 {
			return fmt.Errorf("bindings=%d", bindings)
		}
	case "lease_acquire", "lease_handoff", "lease_heartbeat":
		var leases int
		if err := e.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_leases WHERE workspace_id = ? AND released_at IS NULL`, e.workspace).Scan(&leases); err != nil {
			return err
		}
		if leases != 1 {
			return fmt.Errorf("active leases=%d", leases)
		}
	case "lease_release":
		var leases int
		if err := e.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_leases WHERE workspace_id = ? AND released_at IS NULL`, e.workspace).Scan(&leases); err != nil {
			return err
		}
		if leases != 0 {
			return fmt.Errorf("active leases=%d", leases)
		}
	case "provisioning_begin":
		var operations, workspaces int
		if err := e.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_task_provisions`).Scan(&operations); err != nil {
			return err
		}
		if err := e.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspaces`).Scan(&workspaces); err != nil {
			return err
		}
		if operations != 1 || workspaces != 1 {
			return fmt.Errorf("operations=%d workspaces=%d", operations, workspaces)
		}
	case "provisioning_complete", "provisioning_abort":
		var state string
		if err := e.store.db.QueryRowContext(ctx, `SELECT state FROM cloud_workspace_task_provisions WHERE workspace_id = ?`, e.workspace).Scan(&state); err != nil {
			return err
		}
		expected := ProvisionStateActive
		if operation == "provisioning_abort" {
			expected = ProvisionStateFailed
		}
		if state != expected {
			return fmt.Errorf("provision state=%s want=%s", state, expected)
		}
	}
	return nil
}

func runCrashMatrixHelper() error {
	dsn := os.Getenv("CWS_CRASH_DSN")
	root := os.Getenv("CWS_CRASH_ROOT")
	keyDir := os.Getenv("CWS_CRASH_KEY_DIR")
	operation := os.Getenv("CWS_CRASH_OPERATION")
	phase := os.Getenv("CWS_CRASH_PHASE")
	e, err := openCrashMatrixEnv(dsn, root, keyDir)
	if err != nil {
		return err
	}
	defer e.close()
	if phase == "setup" {
		return setupCrashMatrixCase(e, operation)
	}
	ctx := context.Background()
	if err := loadCrashMatrixWorkspace(ctx, e, operation); err != nil {
		return err
	}
	ctx, replay, err := crashMatrixAtomicContext(ctx, e, operation)
	if err != nil {
		return err
	}
	if phase == "crash" {
		if replay != nil {
			return fmt.Errorf("unexpected pre-crash replay")
		}
		target := os.Getenv("CWS_CRASH_STAGE")
		atomicCommitHookState.Lock()
		atomicCommitHookState.hook = func(stage string) {
			if stage == target {
				os.Exit(crashMatrixExitCode)
			}
		}
		atomicCommitHookState.Unlock()
		if err := executeCrashMatrixOperation(ctx, e, operation); err != nil {
			return err
		}
		return fmt.Errorf("crash hook %q was not reached", target)
	}
	if phase != "verify" {
		return fmt.Errorf("unknown helper phase %q", phase)
	}
	stage := os.Getenv("CWS_CRASH_STAGE")
	if stage == "after_commit" {
		if replay == nil || !replay.Completed {
			return fmt.Errorf("committed response was not replayable")
		}
	} else {
		if replay != nil {
			return fmt.Errorf("before-commit response unexpectedly survived")
		}
		if err := executeCrashMatrixOperation(ctx, e, operation); err != nil {
			return err
		}
		_, replay, err = crashMatrixAtomicContext(context.Background(), e, operation)
		if err != nil {
			return err
		}
		if replay == nil || !replay.Completed {
			return fmt.Errorf("retry did not commit a replayable response")
		}
	}
	return verifyCrashMatrixState(context.Background(), e, operation)
}

func crashMatrixCommand(t *testing.T, baseDir, operation, phase, stage string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCloudWorkspaceCrossProcessCrashMatrix$", "-test.count=1")
	env := make([]string, 0, len(os.Environ())+8)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "CWS_CRASH_") && !strings.HasPrefix(item, masterKeyEnv+"=") {
			env = append(env, item)
		}
	}
	cmd.Env = append(env,
		"CWS_CRASH_HELPER=1",
		"CWS_CRASH_DSN="+filepath.Join(baseDir, "hub.db"),
		"CWS_CRASH_ROOT="+filepath.Join(baseDir, "blobs"),
		"CWS_CRASH_KEY_DIR="+filepath.Join(baseDir, "keys"),
		"CWS_CRASH_OPERATION="+operation,
		"CWS_CRASH_PHASE="+phase,
		"CWS_CRASH_STAGE="+stage,
		masterKeyEnv+"=",
	)
	return cmd
}

func TestCloudWorkspaceCrossProcessCrashMatrix(t *testing.T) {
	if os.Getenv("CWS_CRASH_HELPER") == "1" {
		if err := runCrashMatrixHelper(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	for _, operation := range crashMatrixCases {
		operation := operation
		for _, stage := range []string{"before_commit", "after_commit"} {
			stage := stage
			t.Run(operation+"/"+stage, func(t *testing.T) {
				baseDir := t.TempDir()
				if output, err := crashMatrixCommand(t, baseDir, operation, "setup", stage).CombinedOutput(); err != nil {
					t.Fatalf("setup: %v\n%s", err, output)
				}
				output, err := crashMatrixCommand(t, baseDir, operation, "crash", stage).CombinedOutput()
				exitErr, ok := err.(*exec.ExitError)
				if err == nil || !ok {
					t.Fatalf("crash helper did not exit: err=%v\n%s", err, output)
				}
				if exitErr == nil || exitErr.ExitCode() != crashMatrixExitCode {
					t.Fatalf("crash exit=%v\n%s", err, output)
				}
				if output, err := crashMatrixCommand(t, baseDir, operation, "verify", stage).CombinedOutput(); err != nil {
					t.Fatalf("verify: %v\n%s", err, output)
				}
			})
		}
	}
}
