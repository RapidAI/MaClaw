package httpapi

import (
	"context"
	"encoding/json"
	"log"
)

type TenantIMRuntimeReloader interface {
	ReloadTenantIM(ctx context.Context, tenantID, platform string) error
}

type TenantIMRuntimeStopper interface {
	StopTenantIMs(ctx context.Context, tenantID string)
}

func firstTenantIMRuntimeReloader(items []TenantIMRuntimeReloader) TenantIMRuntimeReloader {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

func firstTenantIMRuntimeStopper(items []TenantIMRuntimeStopper) TenantIMRuntimeStopper {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

func reloadTenantIMRuntime(ctx context.Context, reloader TenantIMRuntimeReloader, tenantID, platform string) error {
	if reloader == nil {
		return nil
	}
	if err := reloader.ReloadTenantIM(ctx, tenantID, platform); err != nil {
		log.Printf("[httpapi] tenant IM runtime reload failed: tenant=%s platform=%s err=%v", tenantID, platform, err)
		return err
	}
	return nil
}

func tenantIMConfigResponse(cfg any, reloadErr error) map[string]any {
	out := map[string]any{}
	data, err := json.Marshal(cfg)
	if err == nil {
		_ = json.Unmarshal(data, &out)
	}
	out["runtime_reload_ok"] = reloadErr == nil
	out["runtime_reload_error"] = runtimeReloadErrorMessage(reloadErr)
	return out
}

func runtimeReloadErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
