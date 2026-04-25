# HubCenter FQDN HA Mode

This rollout now supports a simpler HubCenter HA configuration model for multi-node deployments.

## Goal

Instead of hand-editing a different `node_id + peers` section on every machine, each HubCenter node can now use:

- one local `ha.self_fqdn`
- one shared `ha.nodes` catalog for the whole cluster
- one local `ha.private_key_path`

At startup, HubCenter resolves its own node identity from `ha.self_fqdn`, derives the effective `node_id`, `advertise_url`, and peer list, and keeps its own private key file on disk.

## Recommended Shape

```yaml
ha:
  enabled: true
  self_fqdn: hubs.maclaw.top
  private_key_path: ./data/ha_node_key.pem
  cluster_secret: replace-with-a-long-random-shared-secret
  sync_interval_seconds: 3
  pull_batch_size: 200
  heartbeat_sync_min_interval_seconds: 10
  nodes:
    - fqdn: hubs.mypapers.top
      node_id: hc-1
      node_name: hubcenter-1
      advertise_url: https://hubs.mypapers.top
      enabled: true
    - fqdn: hubs.maclaw.top
      node_id: hc-2
      node_name: hubcenter-2
      advertise_url: https://hubs.maclaw.top
      enabled: true
    - fqdn: hubs2.maclaw.top
      node_id: hc-3
      node_name: hubcenter-3
      advertise_url: https://hubs2.maclaw.top
      enabled: true
```

## Operational Rules

- Every node must use a different `ha.self_fqdn`.
- Every node in `ha.nodes` must have a unique `fqdn`, `node_id`, and `advertise_url`.
- `ha.cluster_secret` must be identical on every HubCenter node.
- `ha.private_key_path` must point to a private key local to that machine.
- Public keys may be shared through config or sync metadata, but private keys must never be copied between nodes.

## Benefits

- Adding more HubCenter nodes is less error-prone.
- Operators edit one shared node catalog instead of custom peer lists per host.
- A rendered config can be generated automatically from inventory plus FQDN.
- The config matches the admin UI goal of using the visible FQDN as the operator-facing identifier.

## Current Tooling

These files now support the simplified model:

- `deploy/render-hubcenter-ha-configs.ps1`
- `deploy/deploy_all_ha.ps1`
- `deploy/hubcenter-ha.inventory.example.psd1`
- `hubcenter/configs/config.ha-hc1.example.yaml`
- `hubcenter/configs/config.ha-hc2.example.yaml`
- `hubcenter/configs/config.ha-hc3.example.yaml`
