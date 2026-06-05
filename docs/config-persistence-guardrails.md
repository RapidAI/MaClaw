# Config Persistence Guardrails

Use `PatchConfig` or `PatchConfigFields` for small, local config changes.

Avoid this pattern for partial settings:

```go
cfg, _ := app.LoadConfig()
cfg.SomeField = value
_ = app.SaveConfig(cfg)
```

That pattern can overwrite newer config fields written by another UI action,
background task, or Hub sync between `LoadConfig` and `SaveConfig`.

Use `PatchConfig` for backend-only fields or grouped internal updates:

```go
err := app.PatchConfig(func(cfg *corelib.AppConfig) {
    cfg.SomeField = value
})
```

Use `PatchConfigFields` for frontend-safe scalar settings:

```ts
await PatchConfigFields({ some_field: value })
```

Prefer object literals so the guard can verify every patched key exists in the
backend allowlist. Dynamic patch objects, such as `PatchConfigFields(patch)`,
must be added to the dynamic allowlist in `scripts/check-main-ui-guards.mjs`
with a clear reason that explains where the allowed keys are constrained.

`SaveConfig` is reserved for full authoritative snapshots, such as the main
model settings save, config import, or TUI messages that carry a complete config.
Any new production `SaveConfig` use must be added to the allowlist in
`scripts/check-main-ui-guards.mjs` with a clear reason. The allowlist entry is
required to document why the caller owns a full authoritative snapshot.

The guard is wired into frontend `prebuild` and `npm run check:ui-guards`.
It also self-tests that accidental `SaveConfig` calls, aliases, unsupported
patch fields, and unallowlisted dynamic patch objects are caught.
