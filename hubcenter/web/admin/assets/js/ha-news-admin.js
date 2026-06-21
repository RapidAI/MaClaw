// HA cluster, SkillHub catalog, MCP market, and news management.
Object.assign(I18N_EN, {
      navHA: 'HA Cluster',
      navHADesc: 'Three-node hot standby and sync status',
      haTitle: 'HA Cluster',
      haDesc: 'Review three-node reachability, sync backlog, and peer health for lightweight multi-node hot standby.',
      haRefresh: 'Refresh Cluster',
      haTabTitle: 'HA Cluster',
      haTabSubtitle: 'Three-node standby status, latency, and peer reachability.',
      haSelfTitle: 'Current Node',
      haPeersTitle: 'Peer Nodes',
      haPeersDesc: 'Each peer shows service quality, sync cursor progress, and the latest observed error.',
      haLoading: 'Loading cluster status...',
      haLoadFailed: 'Load HA cluster status failed: {error}',
      haDisabled: 'HA is not enabled on this node.',
      haEnabled: 'Enabled',
      haNode: 'Node',
      haCluster: 'Cluster',
      haReachable: 'Reachable',
      haQuality: 'Quality',
      haSync: 'Sync',
      haMaxSeq: 'Max op seq',
      haHeartbeatWindow: 'Heartbeat sync window',
      haBacklog: 'Backlog',
      haLag: 'Lag',
      haLastSuccess: 'Last success',
      haLastChecked: 'Last checked',
      haCursor: 'Cursor',
      haError: 'Last error',
      haNoPeers: 'No peer nodes configured.',
      haStandalone: 'Standalone',
      haSeconds: '{value}s',
      haCursorSeq: 'Seq {value}',
      haSyncCategoriesTitle: 'Sync Categories',
      haSyncCategoriesDesc: 'Routing, gossip, skill market, and news replication status.',
      haNodeIDPlaceholder: 'hc-1',
      haNodeNamePlaceholder: 'Node 1',
      haAdvertiseURLPlaceholder: 'https://hubs.mypapers.top',
      haQuorum: 'Quorum',
      haLocalRecords: 'Local',
      haPendingOps: 'Pending',
      haErrorPeers: 'Error Peers',
      haRtt: 'Round-trip Time',
      haMilliseconds: 'ms',
      haNodeChecklistTitle: 'hubcenter HA node checklist',
      haChecklistTemplate: 'template={value}',
      haChecklistNodeID: 'node_id={value}',
      haChecklistNodeName: 'node_name={value}',
      haChecklistAdvertiseURL: 'advertise_url={value}',
      haChecklistSecretConfigured: 'cluster secret=(configured)',
      haChecklistSecretMissing: 'cluster secret=(missing)',
      haChecklistBeforeStart: '[before start]',
      haChecklistUseSameSecret: '- use the same cluster secret on all 3 nodes',
      haChecklistUniqueNodeID: '- confirm this node ID is unique in the cluster',
      haChecklistAdvertiseMatch: '- confirm advertise_url matches the public domain of this machine',
      haChecklistOnlyOtherPeers: '- confirm only the other 2 nodes are listed as peers',
      haChecklistEnabledPeers: '[enabled peers]',
      haChecklistNoEnabledPeers: '- no enabled peers configured',
      haChecklistPeerLine: '- peer[{index}] {value}',
      haChecklistAfterSave: '[after save]',
      haChecklistSaveAllNodes: '- save the config on all 3 nodes',
      haChecklistRestartAll: '- restart each hubcenter service',
      haChecklistVerifyReachability: '- return to this page and verify cluster reachability is 2/2',
      haConfigTitle: 'HA Configuration',
      haConfigDesc: 'Edit the three-node cluster parameters stored in the local hubcenter database.',
      haConfigRestartHint: 'Changes are saved immediately, but fully take effect after hubcenter restarts.',
      haEnabledLabel: 'Enable HA on this node',
      haNodeIDLabel: 'Node ID',
      haNodeNameLabel: 'Node name',
      haAdvertiseURLLabel: 'Advertise URL',
      haClusterSecretLabel: 'Cluster secret',
      haToggleSecret: 'Show Secret',
      haHideSecret: 'Hide Secret',
      haGenerateSecret: 'Generate Secret',
      haSecretGenerated: 'A new HA cluster secret was generated. Save it on all 3 nodes.',
      haSecretGenerationFailed: 'Generate HA cluster secret failed: {error}',
      haCopySecret: 'Copy Secret',
      haCopySecretDone: 'HA cluster secret copied to clipboard.',
      haCopySecretFailed: 'Copy HA cluster secret failed: {error}',
      haSecretHintEmpty: 'Set one shared cluster secret and save the same value on all 3 hubcenter nodes.',
      haSecretHintWeak: 'Current secret is short. Use at least 24 characters before rollout.',
      haSecretHintStrong: 'Secret length looks good for a 3-node rollout. Keep the same value on all nodes.',
      haSyncIntervalLabel: 'Sync interval (seconds)',
      haPushDebounceLabel: 'Push merge window (seconds)',
      haPullBatchLabel: 'Pull batch size',
      haHeartbeatMinLabel: 'Heartbeat sync minimum interval (seconds)',
      haPeerConfigTitle: 'Peer Nodes',
      haPeerConfigDesc: 'Configure the other hubcenter nodes in the cluster.',
      haUseTemplateHC1: 'Use hc-1 Template',
      haUseTemplateHC2: 'Use hc-2 Template',
      haUseTemplateHC3: 'Use hc-3 Template',
      haTemplateApplied: 'HA node template applied for {node}. Review the secret and save the config.',
      haAddPeer: 'Add Peer',
      haSaveConfig: 'Save HA Config',
      haSavingConfig: 'Saving...',
      haConfigLoadFailed: 'Load HA config failed: {error}',
      haConfigSaveFailed: 'Save HA config failed: {error}',
      haConfigSaved: 'HA config saved. Restart hubcenter to apply all changes.',
      haConfigInvalid: 'When HA is enabled, node ID, advertise URL, cluster secret, and at least one peer are required.',
      haPeerEnabled: 'Enabled',
      haPeerID: 'Peer Node ID',
      haPeerName: 'Peer Name',
      haPeerURL: 'Peer URL',
      haRemovePeer: 'Remove',
      haSummaryTitle: 'Node Summary',
      haSummaryDesc: 'Copy this summary to compare the saved HA config across the three hubcenter nodes.',
      haCopySummary: 'Copy Summary',
      haCopySummaryDone: 'HA summary copied to clipboard.',
      haCopySummaryFailed: 'Copy HA summary failed: {error}',
      haCopyYaml: 'Copy YAML',
      haCopyYamlDone: 'HA YAML snippet copied to clipboard.',
      haCopyYamlFailed: 'Copy HA YAML failed: {error}',
      haCopyChecklist: 'Copy Checklist',
      haCopyChecklistDone: 'HA rollout checklist copied to clipboard.',
      haCopyChecklistFailed: 'Copy HA rollout checklist failed: {error}',
      haConfigSelfPeer: 'The current node cannot appear in the peer list: {node}.',
      haConfigDuplicatePeerID: 'Duplicate peer node_id: {value}',
      haConfigDuplicatePeerURL: 'Duplicate peer base_url: {value}',
      haReadinessReadyTitle: 'Saved config is rollout-ready',
      haReadinessPendingTitle: 'Saved config still needs attention',
      haReadinessReady: 'Saved HA config includes the key fields needed for a 3-node rollout. Restart after saving on all nodes.',
      haReadinessPending: 'Finish these HA items before rollout: {items}',
      haReadinessEnabled: 'enable HA',
      haReadinessNodeID: 'node ID',
      haReadinessAdvertiseURL: 'advertise URL',
      haReadinessSecret: 'cluster secret',
      haReadinessSecretStrength: 'longer cluster secret',
      haReadinessPeers: 'at least one enabled peer',
      haReadinessPeerCount: 'the other 2 peer nodes',
      haRuntimeMatchTitle: 'Running config is in sync',
      haRuntimeRestartTitle: 'Restart required to apply',
      haRuntimeUnknownTitle: 'Waiting for runtime comparison',
      haStateHealthy: 'In Sync',
      haStateRestartNeeded: 'Restart Needed',
      haStatePending: 'Pending Check',
      haRuntimeMatches: 'Running HA settings match the saved config for the key fields visible on this page.',
      haRuntimeRestartNeeded: 'Saved HA config differs from the running node state for: {fields}. Restart hubcenter to fully apply it.',
      haRuntimeUnknown: 'Running HA state is not available yet. Save completes immediately, but a restart is still recommended after changes.',
      haFieldEnabled: 'HA enabled',
      haFieldNodeID: 'node ID',
      haFieldNodeName: 'node name',
      haFieldAdvertiseURL: 'advertise URL',
      haFieldHeartbeatMin: 'heartbeat sync minimum interval',
      haFieldPeers: 'peer list',
      haOverviewCluster: 'Cluster Health',
      haOverviewReachability: 'Reachability',
      haOverviewConfig: 'Config Match',
      haOverviewHealthy: 'Healthy',
      haOverviewDegraded: 'Degraded',
      haOverviewRestart: 'Restart Pending',
      haOverviewReady: 'The cluster currently satisfies quorum requirements for continued service.',
      haOverviewQuorumRisk: 'Check quorum and peer node health before rollout.',
      haOverviewPeersReachable: '{value} peer nodes are reachable',
      haOverviewPeersLag: 'Inspect lag, round-trip time, and backlog on the peer cards below.',
      haOverviewConfigSynced: 'Saved HA configuration matches the currently running state.',
      haOverviewConfigDrift: 'Saved HA config differs from running state and needs restart.',
      haNodePlanTitle: '3-Node Deployment Cards',
      haNodePlanDesc: 'Each card maps one hubcenter node identity and its peer list, ready for YAML or rollout checklist copy.',
      haNodePlanNodeID: 'Node ID',
      haNodePlanAdvertise: 'Advertise URL',
      haNodePlanPeers: 'Enabled Peers',
      haNodePlanSecret: 'Cluster Secret',
      haNodePlanSharedSecret: 'Uses the shared cluster_secret currently filled in this page.',
      haNodePlanPendingSecret: 'Set cluster_secret once, then reuse the same value on all 3 nodes.',
      haNodePlanApply: 'Apply to Form',
      haNodePlanTemplate: 'Template {node}',
      haNodePlanCopyYamlDone: 'Node YAML snippet copied to clipboard.',
      haNodePlanCopyYamlFailed: 'Copy node YAML snippet failed: {error}',
      haNodePlanCopyChecklistDone: 'Node rollout checklist copied to clipboard.',
      haNodePlanCopyChecklistFailed: 'Copy node rollout checklist failed: {error}',
      haNodeDisplayName: 'Node {value}',
      haSummaryEmptyValue: '(empty)',
      haSummaryEnabled: 'enabled={value}',
      haSummaryNodeID: 'node_id={value}',
      haSummaryNodeName: 'node_name={value}',
      haSummaryAdvertiseURL: 'advertise_url={value}',
      haSummaryClusterSecret: 'cluster secret={value}',
      haSummarySyncIntervalSeconds: 'sync_interval_seconds={value}',
      haSummaryPushDebounceSeconds: 'push_debounce_seconds={value}',
      haSummaryPullBatchSize: 'pull_batch_size={value}',
      haSummaryHeartbeatSyncMinIntervalSeconds: 'heartbeat_sync_min_interval_seconds={value}',
      haSummaryPeerCount: 'peer_count={value}',
      haSummaryPeerLine: 'peer[{index}]={value}',
      haChecklistTitle: 'hubcenter HA rollout checklist',
      haChecklistLocalNode: 'local_node={value}',
      haChecklistLocalName: 'local_name={value}',
      haChecklistLocalURL: 'local_url={value}',
      haChecklistEnabled: 'ha_enabled={value}',
      haChecklistEnabledPeerCount: 'enabled_peer_count={value}',
      haChecklistSharedChecks: '[shared checks]',
      haChecklistSecretShared: '- cluster secret is identical on all 3 nodes',
      haChecklistUniqueNodeIDLine: '- each node_id is unique',
      haChecklistNoSelfPeerLine: '- no node lists itself as a peer',
      haChecklistPeerReachableLine: '- peer base_url values are public and mutually reachable',
      haChecklistSaveThenRestartLine: '- save config on every node, then restart every hubcenter',
      haChecklistThisNodePeers: '[this node peers]',
      haChecklistNoEnabledPeersYet: '- no enabled peers configured yet',
      haChecklistPeerEntry: '- peer[{index}] {value}',
    });
    Object.assign(I18N_ZH, {
      navHA: '\u591a\u673a\u70ed\u5907',
      navHADesc: '\u4e09\u8282\u70b9\u70ed\u5907\u4e0e\u540c\u6b65\u72b6\u6001',
      haTitle: '\u591a\u673a\u70ed\u5907',
      haDesc: '\u67e5\u770b\u4e09\u8282\u70b9\u53ef\u8fbe\u6027\u3001\u540c\u6b65\u79ef\u538b\u4e0e\u5bf9\u7aef\u8282\u70b9\u5065\u5eb7\u72b6\u6001\uff0c\u7528\u4e8e\u8f7b\u91cf\u7ea7\u591a\u673a\u70ed\u5907\u3002',
      haRefresh: '\u5237\u65b0\u96c6\u7fa4',
      haTabTitle: '\u591a\u673a\u70ed\u5907',
      haTabSubtitle: '\u4e09\u8282\u70b9\u70ed\u5907\u72b6\u6001\u3001\u5ef6\u8fdf\u4e0e\u5bf9\u7aef\u8282\u70b9\u53ef\u8fbe\u6027\u3002',
      haSelfTitle: '\u5f53\u524d\u8282\u70b9',
      haPeersTitle: '\u5bf9\u7aef\u8282\u70b9',
      haPeersDesc: '\u6bcf\u4e2a\u5bf9\u7aef\u4f1a\u5c55\u793a\u670d\u52a1\u8d28\u91cf\u3001\u540c\u6b65\u6e38\u6807\u8fdb\u5ea6\u548c\u6700\u8fd1\u4e00\u6b21\u9519\u8bef\u3002',
      haLoading: '\u6b63\u5728\u52a0\u8f7d\u96c6\u7fa4\u72b6\u6001...',
      haLoadFailed: '\u52a0\u8f7d\u591a\u673a\u70ed\u5907\u96c6\u7fa4\u72b6\u6001\u5931\u8d25\uff1a{error}',
      haDisabled: '\u5f53\u524d\u8282\u70b9\u672a\u542f\u7528\u591a\u673a\u70ed\u5907\u3002',
      haEnabled: '\u5df2\u542f\u7528',
      haNode: '\u8282\u70b9',
      haCluster: '\u96c6\u7fa4',
      haReachable: '\u53ef\u8fbe\u6027',
      haQuality: '\u8d28\u91cf\u5206',
      haSync: '\u540c\u6b65',
      haMaxSeq: '\u6700\u5927\u64cd\u4f5c\u5e8f\u53f7',
      haHeartbeatWindow: '\u5fc3\u8df3\u540c\u6b65\u7a97\u53e3',
      haBacklog: '\u79ef\u538b',
      haLag: '\u5ef6\u8fdf',
      haLastSuccess: '\u4e0a\u6b21\u6210\u529f',
      haLastChecked: '\u4e0a\u6b21\u68c0\u67e5',
      haCursor: '\u540c\u6b65\u6e38\u6807',
      haError: '\u6700\u540e\u9519\u8bef',
      haNoPeers: '\u672a\u914d\u7f6e\u5bf9\u7aef\u8282\u70b9\u3002',
      haStandalone: '\u5355\u673a\u6a21\u5f0f',
      haSeconds: '{value} \u79d2',
      haCursorSeq: '\u5e8f\u53f7 {value}',
      haSyncCategoriesTitle: '\u540c\u6b65\u5206\u7c7b',
      haSyncCategoriesDesc: '\u67e5\u770b\u8def\u7531\u3001\u5410\u69fd\u5899\u3001\u6280\u80fd\u5e02\u573a\u4e0e\u6d88\u606f\u53d1\u5e03\u7684\u540c\u6b65\u72b6\u6001\u3002',
      haQuorum: '\u6cd5\u5b9a\u591a\u6570',
      haLocalRecords: '\u672c\u5730\u8bb0\u5f55',
      haPendingOps: '\u5f85\u5904\u7406\u64cd\u4f5c',
      haErrorPeers: '\u5f02\u5e38\u5bf9\u7aef',
      haRtt: '\u5f80\u8fd4\u65f6\u5ef6',
      haMilliseconds: '\u6beb\u79d2',
      haSyncCategoryRouting: '\u8def\u7531',
      haSyncCategoryGossip: '\u5410\u69fd\u5899',
      haSyncCategorySkillmarket: '\u6280\u80fd\u5e02\u573a',
      haSyncCategoryNews: '\u6d88\u606f\u53d1\u5e03',
      haSyncCategoryUnknown: '\u540c\u6b65\u5206\u7c7b',
      haConfigTitle: '\u591a\u673a\u70ed\u5907\u914d\u7f6e',
      haConfigDesc: '\u7f16\u8f91\u5f53\u524d\u8282\u70b9\u4e2d\u5fc3\u672c\u5730\u6570\u636e\u5e93\u4e2d\u4fdd\u5b58\u7684\u4e09\u8282\u70b9\u70ed\u5907\u53c2\u6570\u3002',
      haConfigRestartHint: '\u914d\u7f6e\u4f1a\u7acb\u5373\u4fdd\u5b58\uff0c\u4f46\u9700\u5728\u91cd\u542f\u8282\u70b9\u4e2d\u5fc3\u670d\u52a1\u540e\u624d\u4f1a\u5168\u9762\u751f\u6548\u3002',
      haEnabledLabel: '\u5728\u5f53\u524d\u8282\u70b9\u542f\u7528\u591a\u673a\u70ed\u5907',
      haNodeIDLabel: '\u8282\u70b9 ID',
      haNodeNameLabel: '\u8282\u70b9\u540d\u79f0',
      haAdvertiseURLLabel: '\u5bf9\u5916\u5730\u5740',
      haClusterSecretLabel: '\u96c6\u7fa4\u5bc6\u94a5',
      haToggleSecret: '\u663e\u793a\u5bc6\u94a5',
      haHideSecret: '\u9690\u85cf\u5bc6\u94a5',
      haGenerateSecret: '\u751f\u6210\u5bc6\u94a5',
      haSecretGenerated: '\u5df2\u751f\u6210\u65b0\u7684\u96c6\u7fa4\u5171\u4eab\u5bc6\u94a5\uff0c\u8bf7\u540c\u6b65\u4fdd\u5b58\u5230 3 \u53f0\u673a\u5668\u3002',
      haSecretGenerationFailed: '\u751f\u6210\u96c6\u7fa4\u5171\u4eab\u5bc6\u94a5\u5931\u8d25\uff1a{error}',
      haCopySecret: '\u590d\u5236\u5bc6\u94a5',
      haCopySecretDone: '\u96c6\u7fa4\u5171\u4eab\u5bc6\u94a5\u5df2\u590d\u5236\u5230\u526a\u8d34\u677f\u3002',
      haCopySecretFailed: '\u590d\u5236\u96c6\u7fa4\u5171\u4eab\u5bc6\u94a5\u5931\u8d25\uff1a{error}',
      haSecretHintEmpty: '\u8bf7\u8bbe\u7f6e\u4e00\u4e2a 3 \u53f0\u8282\u70b9\u4e2d\u5fc3\u516c\u7528\u7684\u96c6\u7fa4\u5171\u4eab\u5bc6\u94a5\uff0c\u5e76\u4fdd\u5b58\u76f8\u540c\u7684\u503c\u3002',
      haSecretHintWeak: '\u5f53\u524d\u5bc6\u94a5\u8fc7\u77ed\uff0c\u5efa\u8bae\u4e0a\u7ebf\u524d\u81f3\u5c11\u4f7f\u7528 24 \u4e2a\u5b57\u7b26\u3002',
      haSecretHintStrong: '\u5f53\u524d\u5bc6\u94a5\u957f\u5ea6\u9002\u5408 3 \u8282\u70b9\u90e8\u7f72\uff0c\u8bf7\u4fdd\u6301 3 \u53f0\u673a\u5668\u5b8c\u5168\u4e00\u81f4\u3002',
      haSyncIntervalLabel: '\u540c\u6b65\u95f4\u9694\uff08\u79d2\uff09',
      haPushDebounceLabel: '\u63a8\u9001\u5408\u5e76\u7a97\u53e3\uff08\u79d2\uff09',
      haPullBatchLabel: '\u6bcf\u6279\u62c9\u53d6\u6570\u91cf',
      haHeartbeatMinLabel: '\u5fc3\u8df3\u540c\u6b65\u6700\u5c0f\u95f4\u9694\uff08\u79d2\uff09',
      haPeerConfigTitle: '\u5bf9\u7aef\u8282\u70b9\u914d\u7f6e',
      haPeerConfigDesc: '\u586b\u5199\u96c6\u7fa4\u4e2d\u5176\u4ed6\u8282\u70b9\u4e2d\u5fc3\u8282\u70b9\u3002',
      haUseTemplateHC1: '\u5957\u7528 hc-1 \u6a21\u677f',
      haUseTemplateHC2: '\u5957\u7528 hc-2 \u6a21\u677f',
      haUseTemplateHC3: '\u5957\u7528 hc-3 \u6a21\u677f',
      haTemplateApplied: '\u5df2\u5957\u7528 {node} \u7684\u70ed\u5907\u8282\u70b9\u6a21\u677f\uff0c\u8bf7\u68c0\u67e5\u96c6\u7fa4\u5171\u4eab\u5bc6\u94a5\u540e\u4fdd\u5b58\u3002',
      haAddPeer: '\u6dfb\u52a0\u8282\u70b9',
      haSaveConfig: '\u4fdd\u5b58\u70ed\u5907\u914d\u7f6e',
      haSavingConfig: '\u4fdd\u5b58\u4e2d...',
      haConfigLoadFailed: '\u52a0\u8f7d\u70ed\u5907\u914d\u7f6e\u5931\u8d25\uff1a{error}',
      haConfigSaveFailed: '\u4fdd\u5b58\u70ed\u5907\u914d\u7f6e\u5931\u8d25\uff1a{error}',
      haConfigSaved: '\u70ed\u5907\u914d\u7f6e\u5df2\u4fdd\u5b58\uff0c\u8bf7\u91cd\u542f\u8282\u70b9\u4e2d\u5fc3\u670d\u52a1\u4f7f\u5168\u90e8\u53d8\u66f4\u751f\u6548\u3002',
      haConfigInvalid: '\u542f\u7528\u591a\u673a\u70ed\u5907\u65f6\uff0c\u5fc5\u987b\u586b\u5199\u8282\u70b9 ID\u3001\u5bf9\u5916\u5730\u5740\u3001\u96c6\u7fa4\u5bc6\u94a5\uff0c\u5e76\u81f3\u5c11\u914d\u7f6e\u4e00\u4e2a\u5bf9\u7aef\u8282\u70b9\u3002',
      haPeerEnabled: '\u5df2\u542f\u7528',
      haPeerID: '\u5bf9\u7aef\u8282\u70b9 ID',
      haPeerName: '\u5bf9\u7aef\u540d\u79f0',
      haPeerURL: '\u5bf9\u7aef\u5730\u5740',
      haRemovePeer: '\u5220\u9664',
      haSummaryTitle: '\u8282\u70b9\u6458\u8981',
      haSummaryDesc: '\u53ef\u590d\u5236\u8fd9\u4e2a\u6458\u8981\uff0c\u7528\u6765\u6a2a\u5411\u6bd4\u5bf9 3 \u53f0\u8282\u70b9\u4e2d\u5fc3\u7684\u70ed\u5907\u914d\u7f6e\u3002',
      haCopySummary: '\u590d\u5236\u6458\u8981',
      haCopySummaryDone: '\u70ed\u5907\u6458\u8981\u5df2\u590d\u5236\u5230\u526a\u8d34\u677f\u3002',
      haCopySummaryFailed: '\u590d\u5236\u70ed\u5907\u6458\u8981\u5931\u8d25\uff1a{error}',
      haCopyYaml: '\u590d\u5236 YAML',
      haCopyYamlDone: 'YAML \u914d\u7f6e\u7247\u6bb5\u5df2\u590d\u5236\u5230\u526a\u8d34\u677f\u3002',
      haCopyYamlFailed: '\u590d\u5236 YAML \u914d\u7f6e\u7247\u6bb5\u5931\u8d25\uff1a{error}',
      haCopyChecklist: '\u590d\u5236\u6e05\u5355',
      haCopyChecklistDone: '\u70ed\u5907\u4e0a\u7ebf\u6e05\u5355\u5df2\u590d\u5236\u5230\u526a\u8d34\u677f\u3002',
      haCopyChecklistFailed: '\u590d\u5236\u70ed\u5907\u4e0a\u7ebf\u6e05\u5355\u5931\u8d25\uff1a{error}',
      haConfigSelfPeer: '\u5f53\u524d\u8282\u70b9\u4e0d\u80fd\u51fa\u73b0\u5728\u5bf9\u7aef\u5217\u8868\u4e2d\uff1a{node}\u3002',
      haConfigDuplicatePeerID: '\u5bf9\u7aef\u8282\u70b9 ID \u91cd\u590d\uff1a{value}',
      haConfigDuplicatePeerURL: '\u5bf9\u7aef\u5730\u5740 \u91cd\u590d\uff1a{value}',
      haReadinessReadyTitle: '\u5df2\u5177\u5907\u4e0a\u7ebf\u6761\u4ef6',
      haReadinessPendingTitle: '\u8fd8\u6709\u5f85\u8865\u9879',
      haReadinessReady: '\u5f53\u524d\u70ed\u5907\u914d\u7f6e\u5df2\u5177\u5907 3 \u8282\u70b9\u4e0a\u7ebf\u7684\u5173\u952e\u5b57\u6bb5\uff0c\u8bf7\u5728 3 \u53f0\u673a\u5668\u5168\u90e8\u4fdd\u5b58\u540e\u518d\u91cd\u542f\u3002',
      haReadinessPending: '\u4e0a\u7ebf\u524d\u8fd8\u9700\u8981\u5b8c\u6210\uff1a{items}',
      haReadinessEnabled: '\u5f00\u542f\u70ed\u5907',
      haReadinessNodeID: '\u8282\u70b9 ID',
      haReadinessAdvertiseURL: '\u5bf9\u5916\u5730\u5740',
      haReadinessSecret: '\u96c6\u7fa4\u5171\u4eab\u5bc6\u94a5',
      haReadinessSecretStrength: '\u66f4\u957f\u7684\u96c6\u7fa4\u5171\u4eab\u5bc6\u94a5',
      haReadinessPeers: '\u81f3\u5c11 1 \u4e2a\u542f\u7528\u7684\u5bf9\u7aef\u8282\u70b9',
      haReadinessPeerCount: '\u53e6\u5916 2 \u4e2a\u5bf9\u7aef\u8282\u70b9',
      haRuntimeMatchTitle: '\u8fd0\u884c\u4e2d\u914d\u7f6e\u5df2\u540c\u6b65',
      haRuntimeRestartTitle: '\u9700\u8981\u91cd\u542f\u624d\u80fd\u5e94\u7528',
      haRuntimeUnknownTitle: '\u7b49\u5f85\u8fd0\u884c\u6001\u5bf9\u6bd4',
      haStateHealthy: '\u5df2\u540c\u6b65',
      haStateRestartNeeded: '\u5f85\u91cd\u542f',
      haStatePending: '\u5f85\u68c0\u67e5',
      haRuntimeMatches: '\u5f53\u524d\u8fd0\u884c\u4e2d\u7684\u70ed\u5907\u5173\u952e\u53c2\u6570\u4e0e\u672c\u9875\u5df2\u4fdd\u5b58\u914d\u7f6e\u4e00\u81f4\u3002',
      haRuntimeRestartNeeded: '\u9875\u9762\u4e2d\u5df2\u4fdd\u5b58\u7684\u70ed\u5907\u914d\u7f6e\u4e0e\u5f53\u524d\u8fd0\u884c\u72b6\u6001\u5728\u8fd9\u4e9b\u5173\u952e\u9879\u4e0a\u4e0d\u4e00\u81f4\uff1a{fields}\u3002\u8bf7\u91cd\u542f\u8282\u70b9\u4e2d\u5fc3\u4f7f\u914d\u7f6e\u5b8c\u5168\u751f\u6548\u3002',
      haRuntimeUnknown: '\u6682\u65f6\u8fd8\u65e0\u6cd5\u6bd4\u5bf9\u8fd0\u884c\u4e2d\u7684\u70ed\u5907\u72b6\u6001\u3002\u4fdd\u5b58\u4f1a\u7acb\u5373\u5b8c\u6210\uff0c\u4f46\u4fee\u6539\u540e\u4ecd\u5efa\u8bae\u91cd\u542f\u8282\u70b9\u4e2d\u5fc3\u3002',
      haFieldEnabled: '\u70ed\u5907\u542f\u7528\u72b6\u6001',
      haFieldNodeID: '\u8282\u70b9 ID',
      haFieldNodeName: '\u8282\u70b9\u540d\u79f0',
      haFieldAdvertiseURL: '\u5bf9\u5916\u5730\u5740',
      haFieldHeartbeatMin: '\u5fc3\u8df3\u540c\u6b65\u6700\u5c0f\u95f4\u9694',
      haFieldPeers: '\u5bf9\u7aef\u5217\u8868',
      haOverviewCluster: '\u96c6\u7fa4\u5065\u5eb7',
      haOverviewReachability: '\u53ef\u8fbe\u6027',
      haOverviewConfig: '\u914d\u7f6e\u4e00\u81f4\u6027',
      haOverviewHealthy: '\u5065\u5eb7',
      haOverviewDegraded: '\u964d\u7ea7',
      haOverviewRestart: '\u5f85\u91cd\u542f',
      haOverviewReady: '\u5f53\u524d\u96c6\u7fa4\u5df2\u6ee1\u8db3\u7ee7\u7eed\u63d0\u4f9b\u670d\u52a1\u7684\u6cd5\u5b9a\u591a\u6570\u8981\u6c42\u3002',
      haOverviewQuorumRisk: '\u4e0a\u7ebf\u524d\u4ecd\u9700\u5173\u6ce8\u6cd5\u5b9a\u591a\u6570\u6216\u5bf9\u7aef\u8282\u70b9\u5065\u5eb7\u3002',
      haOverviewPeersReachable: '\u5df2\u53ef\u8fbe {value} \u4e2a\u5bf9\u7aef\u8282\u70b9',
      haOverviewPeersLag: '\u8bf7\u5728\u4e0b\u65b9\u5bf9\u7aef\u5361\u7247\u68c0\u67e5\u5ef6\u8fdf\u3001\u5f80\u8fd4\u65f6\u5ef6\u548c\u79ef\u538b\u3002',
      haOverviewConfigSynced: '\u5df2\u4fdd\u5b58\u7684\u70ed\u5907\u914d\u7f6e\u4e0e\u5f53\u524d\u8fd0\u884c\u72b6\u6001\u4e00\u81f4\u3002',
      haOverviewConfigDrift: '\u5df2\u4fdd\u5b58\u7684\u70ed\u5907\u914d\u7f6e\u4e0e\u8fd0\u884c\u72b6\u6001\u4e0d\u4e00\u81f4\uff0c\u9700\u8981\u91cd\u542f\u751f\u6548\u3002',
      haNodePlanTitle: '\u4e09\u8282\u70b9\u90e8\u7f72\u5361\u7247',
      haNodePlanDesc: '\u6bcf\u5f20\u5361\u7247\u90fd\u5bf9\u5e94 1 \u53f0\u8282\u70b9\u7684\u8eab\u4efd\u548c\u5bf9\u7aef\u5217\u8868\uff0c\u53ef\u76f4\u63a5\u590d\u5236 YAML \u914d\u7f6e\u6216\u4e0a\u7ebf\u6e05\u5355\u3002',
      haNodePlanNodeID: '\u8282\u70b9 ID',
      haNodePlanAdvertise: '\u5bf9\u5916\u5730\u5740',
      haNodePlanPeers: '\u5df2\u542f\u7528\u5bf9\u7aef',
      haNodePlanSecret: '\u96c6\u7fa4\u5bc6\u94a5',
      haNodePlanSharedSecret: '\u4f7f\u7528\u672c\u9875\u5f53\u524d\u586b\u5199\u7684\u5171\u4eab\u96c6\u7fa4\u5bc6\u94a5\u3002',
      haNodePlanPendingSecret: '\u8bf7\u5148\u8bbe\u7f6e\u4e00\u6b21\u96c6\u7fa4\u5171\u4eab\u5bc6\u94a5\uff0c\u7136\u540e 3 \u53f0\u673a\u5668\u4fdd\u5b58\u76f8\u540c\u503c\u3002',
      haNodePlanApply: '\u5957\u7528\u5230\u8868\u5355',
      haNodePlanTemplate: '\u6a21\u677f {node}',
      haNodePlanCopyYamlDone: '\u8282\u70b9 YAML \u914d\u7f6e\u7247\u6bb5\u5df2\u590d\u5236\u5230\u526a\u8d34\u677f\u3002',
      haNodePlanCopyYamlFailed: '\u590d\u5236\u8282\u70b9 YAML \u914d\u7f6e\u7247\u6bb5\u5931\u8d25\uff1a{error}',
      haNodePlanCopyChecklistDone: '\u8282\u70b9\u4e0a\u7ebf\u6e05\u5355\u5df2\u590d\u5236\u5230\u526a\u8d34\u677f\u3002',
      haNodePlanCopyChecklistFailed: '\u590d\u5236\u8282\u70b9\u4e0a\u7ebf\u6e05\u5355\u5931\u8d25\uff1a{error}',

      haNodeDisplayName: '\u8282\u70b9 {value}',
      haSummaryEmptyValue: '(\u7a7a)',
      haSummaryEnabled: '\u542f\u7528={value}',
      haSummaryNodeID: '\u8282\u70b9ID={value}',
      haSummaryNodeName: '\u8282\u70b9\u540d\u79f0={value}',
      haSummaryAdvertiseURL: '\u5bf9\u5916\u5730\u5740={value}',
      haSummaryClusterSecret: '\u96c6\u7fa4\u5bc6\u94a5={value}',
      haSummarySyncIntervalSeconds: '\u540c\u6b65\u95f4\u9694\u79d2={value}',
      haSummaryPushDebounceSeconds: '\u63a8\u9001\u5408\u5e76\u7a97\u53e3={value}',
      haSummaryPullBatchSize: '\u62c9\u53d6\u6279\u6b21\u6570={value}',
      haSummaryHeartbeatSyncMinIntervalSeconds: '\u5fc3\u8df3\u540c\u6b65\u6700\u5c0f\u95f4\u9694={value}',
      haSummaryPeerCount: '\u5bf9\u7aef\u6570\u91cf={value}',
      haSummaryPeerLine: '\u5bf9\u7aef[{index}]={value}',
      haChecklistTitle: 'hubcenter HA \u4e0a\u7ebf\u68c0\u67e5\u6e05\u5355',
      haChecklistLocalNode: '\u5f53\u524d\u8282\u70b9={value}',
      haChecklistLocalName: '\u5f53\u524d\u540d\u79f0={value}',
      haChecklistLocalURL: '\u5f53\u524d\u5730\u5740={value}',
      haChecklistEnabled: 'HA\u542f\u7528={value}',
      haChecklistEnabledPeerCount: '\u5df2\u542f\u7528\u5bf9\u7aef\u6570={value}',
      haChecklistSharedChecks: '[\u5171\u4eab\u68c0\u67e5]',
      haChecklistSecretConfigured: '\u96c6\u7fa4\u5bc6\u94a5=(\u5df2\u914d\u7f6e)',
      haChecklistSecretMissing: '\u96c6\u7fa4\u5bc6\u94a5=(\u672a\u914d\u7f6e)',
      haChecklistSecretShared: '- 3 \u53f0\u8282\u70b9\u7684\u96c6\u7fa4\u5bc6\u94a5\u5fc5\u987b\u5b8c\u5168\u4e00\u81f4',
      haChecklistUniqueNodeIDLine: '- \u6bcf\u4e2a\u8282\u70b9 ID \u90fd\u5fc5\u987b\u552f\u4e00',
      haChecklistNoSelfPeerLine: '- \u4efb\u4f55\u8282\u70b9\u90fd\u4e0d\u80fd\u628a\u81ea\u5df1\u914d\u7f6e\u6210\u5bf9\u7aef',
      haChecklistPeerReachableLine: '- \u5bf9\u7aef\u5730\u5740\u5fc5\u987b\u53ef\u516c\u7f51\u8bbf\u95ee\u4e14\u5f7c\u6b64\u4e92\u901a',
      haChecklistSaveThenRestartLine: '- \u6bcf\u53f0\u8282\u70b9\u90fd\u4fdd\u5b58\u914d\u7f6e\u540e\uff0c\u518d\u9010\u53f0\u91cd\u542f hubcenter',
      haChecklistThisNodePeers: '[\u5f53\u524d\u8282\u70b9\u5bf9\u7aef]',
      haChecklistNoEnabledPeersYet: '- \u8fd8\u6ca1\u6709\u542f\u7528\u7684\u5bf9\u7aef\u914d\u7f6e',
      haChecklistPeerEntry: '- \u5bf9\u7aef[{index}] {value}',
    });
    tabMeta.ha = ['haTabTitle','haTabSubtitle'];
    TAB_ICONS.ha = '<svg viewBox="0 0 24 24"><path d="M5 8.5h14"></path><path d="M5 15.5h14"></path><circle cx="5" cy="8.5" r="2"></circle><circle cx="19" cy="8.5" r="2"></circle><circle cx="12" cy="15.5" r="2"></circle><path d="M7 15.5h3"></path><path d="M14 15.5h3"></path></svg>';
    function haFmtTime(v){ if(!v) return tr('na'); const d = new Date(v); return Number.isNaN(d.getTime()) ? tr('na') : d.toLocaleString(); }
    function haFmtSeconds(v){ return tr('haSeconds', {value: String(v ?? 0)}); }
    function haSyncCategoryText(value){
      const clean = String(value || '').trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
      const map = {
        routing: 'haSyncCategoryRouting',
        route: 'haSyncCategoryRouting',
        gossip: 'haSyncCategoryGossip',
        skillmarket: 'haSyncCategorySkillmarket',
        skill_market: 'haSyncCategorySkillmarket',
        news: 'haSyncCategoryNews'
      };
      return map[clean] ? tr(map[clean]) : (value || tr('haSyncCategoryUnknown'));
    }
    function renderHAOverview(statusData, cfgData){
      const root = document.getElementById('haOverviewGrid');
      if(!root) return;
      root.setAttribute('aria-busy', 'false');
      const status = statusData || haStatusCache || {};
      const cfg = cfgData || haConfigCache || {};
      if(!status || status.enabled === false){
        root.innerHTML = '<div class="ha-overview-card"><div class="item-title">' + tr('haOverviewCluster') + '</div><strong>' + tr('haStandalone') + '</strong><div class="item-meta">' + tr('haDisabled') + '</div><span class="badge warn">' + tr('haStatePending') + '</span></div>' + '<div class="ha-overview-card"><div class="item-title">' + tr('haOverviewReachability') + '</div><strong>0/0</strong><div class="item-meta">' + tr('haStandalone') + '</div><span class="badge warn">' + tr('haStatePending') + '</span></div>' + '<div class="ha-overview-card"><div class="item-title">' + tr('haOverviewConfig') + '</div><strong>' + tr('haStatePending') + '</strong><div class="item-meta">' + tr('haRuntimeUnknown') + '</div><span class="badge warn">' + tr('haStatePending') + '</span></div>';
        return;
      }
      const clusterStatus = String(status.cluster?.status || '').toLowerCase();
      const peerList = Array.isArray(status.peers) ? status.peers : [];
      const reachablePeers = peerList.filter(peer => !!peer.reachable).length;
      const totalPeers = peerList.length;
      const clusterOk = clusterStatus === 'healthy' || clusterStatus === 'online' || clusterStatus === 'ready';
      const driftFields = detectHARuntimeDifferences(cfg, status);
      const configOk = driftFields.length === 0;
      root.innerHTML = '<div class="ha-overview-card"><div class="item-title">' + tr('haOverviewCluster') + '</div><strong>' + escapeHtml(clusterOk ? tr('haOverviewHealthy') : tr('haOverviewDegraded')) + '</strong><div class="item-meta">' + escapeHtml(clusterOk ? tr('haOverviewReady') : tr('haOverviewQuorumRisk')) + '</div><span class="badge ' + (clusterOk ? 'ok' : 'warn') + '">' + escapeHtml(haStatusText(status.cluster?.status || tr('haStatePending'))) + '</span></div>' + '<div class="ha-overview-card"><div class="item-title">' + tr('haOverviewReachability') + '</div><strong>' + reachablePeers + '/' + totalPeers + '</strong><div class="item-meta">' + tr('haOverviewPeersReachable', {value: String(reachablePeers)}) + '<br>' + tr('haOverviewPeersLag') + '</div><span class="badge ' + (reachablePeers === totalPeers ? 'ok' : reachablePeers > 0 ? 'warn' : 'danger') + '">' + tr('haReachable') + '</span></div>' + '<div class="ha-overview-card"><div class="item-title">' + tr('haOverviewConfig') + '</div><strong>' + escapeHtml(configOk ? tr('haStateHealthy') : tr('haOverviewRestart')) + '</strong><div class="item-meta">' + escapeHtml(configOk ? tr('haOverviewConfigSynced') : tr('haOverviewConfigDrift')) + '</div><span class="badge ' + (configOk ? 'ok' : 'danger') + '">' + escapeHtml(configOk ? tr('haStateHealthy') : tr('haStateRestartNeeded')) + '</span></div>';
    }
    function renderHAStatus(data){
      const summary = document.getElementById('haSummaryList');
      const peers = document.getElementById('haPeerList');
      const syncDetails = document.getElementById('haSyncDetailList');
      if(!summary || !peers || !syncDetails) return;
      [summary, peers, syncDetails].forEach(el => el.setAttribute('aria-busy', 'false'));
      if(!data || data.enabled === false){
        summary.innerHTML = '<div class="hint">' + tr('haDisabled') + '</div>';
        peers.innerHTML = '<div class="hint">' + tr('haStandalone') + '</div>';
        syncDetails.innerHTML = '<div class="hint">' + tr('haStandalone') + '</div>';
        renderHAOverview(data, haConfigCache);
        return;
      }
      const reachableText = (data.cluster?.reachable_nodes ?? 0) + '/' + (data.cluster?.total_nodes ?? 0);
      summary.innerHTML = [
        '<div class="data-row"><div class="data-row-main"><strong>' + tr('haNode') + '</strong><div class="data-row-meta mono">' + escapeHtml((data.node_name || data.node_id || '-') + ' | ' + (data.advertise_url || '-')) + '</div></div><span class="badge info">' + escapeHtml(haStatusText(data.service_status || '-')) + '</span></div>',
        '<div class="data-row"><div class="data-row-main"><strong>' + tr('haCluster') + '</strong><div class="data-row-meta">' + escapeHtml(haStatusText(data.cluster?.status || '-')) + '</div></div><div class="data-row-value"><div>' + tr('haReachable') + ': ' + reachableText + '</div><small>' + tr('haQuorum') + ' ' + (data.cluster?.quorum_size ?? 0) + '</small></div></div>',
        '<div class="data-row"><div class="data-row-main"><strong>' + tr('haQuality') + '</strong><div class="data-row-meta">' + tr('haSync') + '</div></div><div class="data-row-value"><div>' + escapeHtml(String(data.quality_score ?? 0)) + '</div><small>' + tr('haMaxSeq') + ': ' + escapeHtml(String(data.sync?.max_op_seq ?? 0)) + '</small></div></div>',
        '<div class="data-row"><div class="data-row-main"><strong>' + tr('haHeartbeatWindow') + '</strong><div class="data-row-meta">' + tr('haLastSuccess') + ': ' + escapeHtml(haFmtTime(data.sync?.last_success_at)) + '</div></div><div class="data-row-value"><div>' + escapeHtml(haFmtSeconds(data.sync?.heartbeat_sync_min_interval_seconds ?? 0)) + '</div><small>' + tr('haEnabled') + ': ' + (data.sync?.enabled ? tr('hubRuntimeFetchOk') : tr('hubRuntimeFetchFail')) + '</small></div></div>'
      ].join('');
      const peerList = Array.isArray(data.peers) ? data.peers : [];
      const detailList = Array.isArray(data.sync?.details) ? data.sync.details : [];
      syncDetails.innerHTML = detailList.length ? detailList.map(function(item){
        const peerRows = Array.isArray(item.peers) ? item.peers : [];
        const statusClass = item.status === 'healthy' ? 'ok' : (item.status === 'syncing' ? 'warn' : (item.status === 'error' ? 'danger' : 'info'));
        const peerHtml = peerRows.length ? '<div class="ha-sync-peer-list">' + peerRows.map(function(peer){
          const peerStatusClass = peer.status === 'synced' ? 'ok' : (peer.status === 'pending' ? 'warn' : (peer.status === 'error' ? 'danger' : 'info'));
          return '<div class="ha-sync-peer"><div class="ha-sync-peer-main"><div class="ha-sync-peer-name">' + escapeHtml(peer.node_name || peer.node_id || '-') + '</div><div class="ha-sync-peer-meta">' + tr('haCursorSeq', {value: String(peer.cursor_last_pulled_seq ?? 0)}) + ' | ' + escapeHtml(haFmtTime(peer.cursor_last_success_at)) + '</div></div><span class="badge ' + peerStatusClass + '">' + escapeHtml(haStatusText(peer.status || '-')) + '</span></div>'; 
        }).join('') + '</div>' : '<div class="hint">' + tr('haNoPeers') + '</div>';
        const syncLabel = haSyncCategoryText(item.key || item.label || '');
        return '<div class="ha-sync-card"><div class="item-head"><div><div class="item-title">' + escapeHtml(syncLabel) + '</div><div class="item-meta">' + escapeHtml(syncLabel) + '</div></div><span class="badge ' + statusClass + '">' + escapeHtml(haStatusText(item.status || '-')) + '</span></div><div class="ha-sync-meta"><span>' + tr('haLocalRecords') + ': ' + escapeHtml(String(item.local_records ?? 0)) + '</span><span>' + tr('haMaxSeq') + ': ' + escapeHtml(String(item.last_op_seq ?? 0)) + '</span><span>' + tr('haPendingOps') + ': ' + escapeHtml(String(item.pending_ops ?? 0)) + '</span><span>' + tr('haErrorPeers') + ': ' + escapeHtml(String(item.error_peers ?? 0)) + '</span></div>' + peerHtml + '</div>'; 
      }).join('') : '<div class="hint">' + tr('haLoading') + '</div>';
      if(!peerList.length){
        peers.innerHTML = '<div class="hint">' + tr('haNoPeers') + '</div>';
        renderHAOverview(data, haConfigCache);
        return;
      }
      peers.innerHTML = peerList.map(function(peer){
        const statusClass = peer.reachable ? (peer.quality_score >= 80 ? 'ok' : 'warn') : 'danger';
        const errorLine = peer.last_error ? '<div class="ha-peer-error">' + tr('haError') + ': ' + escapeHtml(peer.last_error) + '</div>' : '';
        return '<div class="ha-peer-card">'
          + '<div class="item-head"><div><div class="item-title">' + escapeHtml(peer.node_name || peer.node_id || '-') + '</div><div class="item-meta mono">' + escapeHtml(peer.base_url || '-') + '</div></div><span class="badge ' + statusClass + '">' + escapeHtml(haStatusText(peer.service_status || '-')) + '</span></div>'
          + '<div class="ha-peer-meta"><span>' + tr('haQuality') + ': ' + escapeHtml(String(peer.quality_score ?? 0)) + '</span><span>' + tr('haRtt') + ': ' + escapeHtml(String(peer.rtt_ms ?? 0)) + ' ' + tr('haMilliseconds') + '</span><span>' + tr('haLag') + ': ' + escapeHtml(haFmtSeconds(peer.lag_seconds ?? 0)) + '</span><span>' + tr('haBacklog') + ': ' + escapeHtml(String(peer.backlog ?? 0)) + '</span></div>'
          + '<div class="ha-peer-meta"><span>' + tr('haCursor') + ': ' + tr('haCursorSeq', {value: String(peer.cursor_last_pulled_seq ?? 0)}) + '</span><span>' + tr('haLastSuccess') + ': ' + escapeHtml(haFmtTime(peer.cursor_last_success_at || peer.last_success_at)) + '</span><span>' + tr('haLastChecked') + ': ' + escapeHtml(haFmtTime(peer.last_checked_at)) + '</span></div>'
          + errorLine
          + '</div>';
      }).join('');
      renderHAOverview(data, haConfigCache);
    }
    const haNodeTemplates = {
      'hc-1': { node_id: 'hc-1', advertise_url: 'http://hub.mypapers.top:9388', peers: [
        { enabled: true, node_id: 'hc-2', base_url: 'http://107.172.86.131:9388' },
        { enabled: true, node_id: 'hc-3', base_url: 'http://66.154.113.63:9388' }
      ]},
      'hc-2': { node_id: 'hc-2', advertise_url: 'http://107.172.86.131:9388', peers: [
        { enabled: true, node_id: 'hc-1', base_url: 'http://hub.mypapers.top:9388' },
        { enabled: true, node_id: 'hc-3', base_url: 'http://66.154.113.63:9388' }
      ]},
      'hc-3': { node_id: 'hc-3', advertise_url: 'http://66.154.113.63:9388', peers: [
        { enabled: true, node_id: 'hc-1', base_url: 'http://hub.mypapers.top:9388' },
        { enabled: true, node_id: 'hc-2', base_url: 'http://107.172.86.131:9388' }
      ]}
    };
    function haNodeDisplayName(nodeID){
      const suffix = String(nodeID || '').replace(/^hc-/, '') || '?';
      return tr('haNodeDisplayName', {value: suffix});
    }
    let haConfigCache = null;
    let haStatusCache = null;
    function normalizedPeerSummary(peers){
      return (Array.isArray(peers) ? peers : []).filter(function(peer){
        return peer && (peer.enabled || peer.node_id || peer.name || peer.base_url);
      }).map(function(peer){
        return {
          enabled: !!peer.enabled,
          node_id: String(peer.node_id || '').trim(),
          name: String(peer.name || '').trim(),
          base_url: String(peer.base_url || '').replace(/\/+$/, '').trim()
        };
      }).sort(function(a, b){ return a.node_id.localeCompare(b.node_id) || a.base_url.localeCompare(b.base_url); });
    }
    function normalizeHAFQDN(value){
      const raw = String(value || '').replace(/\/+$/, '').trim();
      if(!raw) return '';
      try {
        const parsed = new URL(raw.indexOf('://') >= 0 ? raw : 'https://' + raw);
        return parsed.hostname.replace(/\.$/, '').toLowerCase();
      } catch (_) {
        return raw.replace(/^https?:\/\//i, '').split('/')[0].split(':')[0].replace(/\.$/, '').toLowerCase();
      }
    }
    function buildHANodesFromPeers(cfg){
      const data = cfg || {};
      const selfFQDN = normalizeHAFQDN(data.self_fqdn || data.suggested_self_fqdn || data.advertise_url);
      const nodes = [];
      if(selfFQDN || data.node_id || data.node_name || data.advertise_url){
        nodes.push({
          enabled: true,
          fqdn: selfFQDN,
          node_id: String(data.node_id || '').trim(),
          node_name: String(data.node_name || '').trim(),
          advertise_url: String(data.advertise_url || '').replace(/\/+$/, '').trim()
        });
      }
      normalizedPeerSummary(data.peers).forEach(function(peer){
        nodes.push({
          enabled: !!peer.enabled,
          fqdn: normalizeHAFQDN(peer.base_url),
          node_id: peer.node_id,
          node_name: peer.name,
          advertise_url: peer.base_url
        });
      });
      return nodes.filter(function(node){ return node.enabled || node.fqdn || node.node_id || node.node_name || node.advertise_url; });
    }
    function renderHAConfigReadiness(cfg){
      const el = document.getElementById('haConfigReadinessHint');
      const card = document.getElementById('haConfigReadinessBadge');
      const badge = document.getElementById('haConfigReadinessBadgeText');
      const title = card ? card.querySelector('strong') : null;
      if(!el) return;
      function setState(kind, titleKey, body){
        if(card){ card.className = 'status-card ' + kind; }
        if(badge){ badge.className = 'badge ' + kind; badge.textContent = tr(kind === 'ok' ? 'haStateHealthy' : 'haStatePending'); }
        if(title){ title.textContent = tr(titleKey); }
        el.textContent = body;
      }
      const data = cfg || {};
      if(!data.enabled){
        setState('warn', 'haReadinessPendingTitle', tr('haReadinessPending', {items: tr('haReadinessEnabled')}));
        return;
      }
      const missing = [];
      const peers = normalizedPeerSummary(data.peers);
      const enabledPeers = peers.filter(function(peer){ return peer.enabled && peer.node_id && peer.base_url; });
      if(!String(data.node_id || '').trim()) missing.push(tr('haReadinessNodeID'));
      if(!String(data.advertise_url || '').trim()) missing.push(tr('haReadinessAdvertiseURL'));
      const secret = String(data.cluster_secret || '').trim();
      if(!secret) missing.push(tr('haReadinessSecret'));
      else if(secret.length < 24) missing.push(tr('haReadinessSecretStrength'));
      if(!enabledPeers.length) missing.push(tr('haReadinessPeers'));
      else if(enabledPeers.length < 2) missing.push(tr('haReadinessPeerCount'));
      if(missing.length){
        setState('warn', 'haReadinessPendingTitle', tr('haReadinessPending', {items: missing.join(' / ')}));
        return;
      }
      setState('ok', 'haReadinessReadyTitle', tr('haReadinessReady'));
    }
    function renderHARuntimeHint(){
      const el = document.getElementById('haRuntimeConfigHint');
      const card = document.getElementById('haRuntimeConfigBadge');
      const badge = document.getElementById('haRuntimeConfigBadgeText');
      const title = card ? card.querySelector('strong') : null;
      if(!el) return;
      function setState(kind, titleKey, body){
        if(card){ card.className = 'status-card ' + kind; }
        if(badge){ badge.className = 'badge ' + kind; badge.textContent = tr(kind === 'ok' ? 'haStateHealthy' : kind === 'danger' ? 'haStateRestartNeeded' : 'haStatePending'); }
        if(title){ title.textContent = tr(titleKey); }
        el.textContent = body;
      }
      if(!haConfigCache || !haStatusCache){
        setState('warn', 'haRuntimeUnknownTitle', tr('haRuntimeUnknown'));
        return;
      }
      const diffs = detectHARuntimeDifferences(haConfigCache, haStatusCache);

      if(diffs.length){
        setState('danger', 'haRuntimeRestartTitle', tr('haRuntimeRestartNeeded', {fields: diffs.join(' / ')}));
        return;
      }
      setState('ok', 'haRuntimeMatchTitle', tr('haRuntimeMatches'));
    }
    function detectHARuntimeDifferences(savedCfg, runtimeStatus){
      if(!savedCfg || !runtimeStatus) return [];
      const diffs = [];
      if(!!savedCfg.enabled !== !!runtimeStatus.enabled) diffs.push(tr('haFieldEnabled'));
      if(String(savedCfg.node_id || '').trim() !== String(runtimeStatus.node_id || '').trim()) diffs.push(tr('haFieldNodeID'));
      if(String(savedCfg.node_name || '').trim() !== String(runtimeStatus.node_name || '').trim()) diffs.push(tr('haFieldNodeName'));
      const savedURL = String(savedCfg.advertise_url || '').replace(/\/+$/, '').trim();
      const runningURL = String(runtimeStatus.advertise_url || '').replace(/\/+$/, '').trim();
      if(savedURL !== runningURL) diffs.push(tr('haFieldAdvertiseURL'));
      const savedHeartbeat = Number(savedCfg.heartbeat_sync_min_interval_seconds || 0);
      const runtimeHeartbeat = Number(runtimeStatus.sync?.heartbeat_sync_min_interval_seconds || 0);
      if(savedHeartbeat !== runtimeHeartbeat) diffs.push(tr('haFieldHeartbeatMin'));
      const savedPushDebounce = Number(savedCfg.push_debounce_seconds || 0);
      const runtimePushDebounce = Number(runtimeStatus.sync?.push_debounce_seconds || 0);
      if(savedPushDebounce !== runtimePushDebounce) diffs.push(tr('haPushDebounceLabel'));
      const savedPeers = normalizedPeerSummary(savedCfg.peers);
      const runtimePeers = normalizedPeerSummary((runtimeStatus.peers || []).map(function(peer){
        return { enabled: true, node_id: peer.node_id, name: peer.node_name, base_url: peer.base_url };
      }));
      if(JSON.stringify(savedPeers) !== JSON.stringify(runtimePeers)) diffs.push(tr('haFieldPeers'));
      return diffs;
    }
    function haNodePlanConfig(nodeID, currentCfg){
      const template = haNodeTemplates[nodeID];
      const cfg = currentCfg || haConfigCache || {};
      if(!template) return null;
      return {
        enabled: true,
        self_fqdn: normalizeHAFQDN(template.advertise_url),
        node_id: template.node_id,
        node_name: haNodeDisplayName(template.node_id),
        advertise_url: template.advertise_url,
        cluster_secret: String(cfg.cluster_secret || '').trim(),
        sync_interval_seconds: Number(cfg.sync_interval_seconds || 5),
        push_debounce_seconds: Number(cfg.push_debounce_seconds || 5),
        pull_batch_size: Number(cfg.pull_batch_size || 1000),
        heartbeat_sync_min_interval_seconds: Number(cfg.heartbeat_sync_min_interval_seconds || 60),
        peers: template.peers.map(function(peer){ return Object.assign({}, peer, { name: haNodeDisplayName(peer.node_id) }); })
      };
    }
    function buildHANodeChecklist(nodeID, currentCfg){
      const cfg = haNodePlanConfig(nodeID, currentCfg);
      if(!cfg) return '';
      const peers = normalizedPeerSummary(cfg.peers).filter(function(peer){ return peer.enabled; });
      const lines = [
        tr('haNodeChecklistTitle'),
        tr('haChecklistTemplate', {value: nodeID}),
        tr('haChecklistNodeID', {value: (cfg.node_id || '')}),
        tr('haChecklistNodeName', {value: (cfg.node_name || '')}),
        tr('haChecklistAdvertiseURL', {value: (cfg.advertise_url || '')}),
        cfg.cluster_secret ? tr('haChecklistSecretConfigured') : tr('haChecklistSecretMissing'),
        '',
        tr('haChecklistBeforeStart'),
        tr('haChecklistUseSameSecret'),
        tr('haChecklistUniqueNodeID'),
        tr('haChecklistAdvertiseMatch'),
        tr('haChecklistOnlyOtherPeers'),
        '',
        tr('haChecklistEnabledPeers')
      ];
      if(!peers.length){
        lines.push(tr('haChecklistNoEnabledPeers'));
      } else {
        peers.forEach(function(peer, index){
          lines.push(tr('haChecklistPeerLine', {index: String(index), value: [peer.node_id, peer.name, peer.base_url].join(' | ')}));
        });
      }
      lines.push('');
      lines.push(tr('haChecklistAfterSave'));
      lines.push(tr('haChecklistSaveAllNodes'));
      lines.push(tr('haChecklistRestartAll'));
      lines.push(tr('haChecklistVerifyReachability'));
      return lines.join('\n');
    }
    async function copyHANodeYaml(nodeID){
      try {
        await copyTextValue(buildHAYamlSnippet(haNodePlanConfig(nodeID, haConfigCache || collectHAConfig())));
        const msg = tr('haNodePlanCopyYamlDone');
        setOutput(msg);
        showToast(msg, 'success');
      } catch (err) {
        const msg = tr('haNodePlanCopyYamlFailed', {error: err.message});
        setOutput(msg);
        showToast(msg, 'error');
      }
    }
    async function copyHANodeChecklist(nodeID){
      try {
        await copyTextValue(buildHANodeChecklist(nodeID, haConfigCache || collectHAConfig()));
        const msg = tr('haNodePlanCopyChecklistDone');
        setOutput(msg);
        showToast(msg, 'success');
      } catch (err) {
        const msg = tr('haNodePlanCopyChecklistFailed', {error: err.message});
        setOutput(msg);
        showToast(msg, 'error');
      }
    }
    function renderHANodePlans(cfg){
      const root = document.getElementById('haNodePlanGrid');
      if(!root) return;
      const current = cfg || haConfigCache || {};
      const cards = Object.keys(haNodeTemplates).sort().map(function(nodeID){
        const plan = haNodePlanConfig(nodeID, current);
        const peers = normalizedPeerSummary(plan.peers).filter(function(peer){ return peer.enabled; });
        const secretReady = !!String(plan.cluster_secret || '').trim();
        const peerHtml = peers.map(function(peer){
          return '<div class="ha-node-plan-peer"><div><strong class="ha-node-plan-peer-name">' + escapeHtml(peer.name || peer.node_id || '-') + '</strong></div><div class="mono">' + escapeHtml(peer.node_id || '-') + ' | ' + escapeHtml(peer.base_url || '-') + '</div></div>';
        }).join('');
        return '<div class="ha-node-plan-card">'
          + '<div class="ha-node-plan-head"><div><div class="item-title">' + tr('haNodePlanTemplate', {node: nodeID}) + '</div><strong>' + escapeHtml(plan.node_name || nodeID) + '</strong></div><span class="badge ' + (secretReady ? 'ok' : 'warn') + '">' + escapeHtml(secretReady ? tr('haStateHealthy') : tr('haStatePending')) + '</span></div>'
          + '<div class="ha-node-plan-fields">'
          + '<div class="ha-node-plan-field"><label>' + tr('haNodePlanNodeID') + '</label><div class="mono">' + escapeHtml(plan.node_id || '-') + '</div></div>'
          + '<div class="ha-node-plan-field"><label>' + tr('haNodePlanAdvertise') + '</label><div class="mono">' + escapeHtml(plan.advertise_url || '-') + '</div></div>'
          + '<div class="ha-node-plan-field"><label>' + tr('haNodePlanSecret') + '</label><div class="item-meta">' + escapeHtml(secretReady ? tr('haNodePlanSharedSecret') : tr('haNodePlanPendingSecret')) + '</div></div>'
          + '<div class="ha-node-plan-field"><label>' + tr('haNodePlanPeers') + '</label><div class="ha-node-plan-peers">' + (peerHtml || '<div class="item-meta">-</div>') + '</div></div>'
          + '</div>'
          + '<div class="ha-node-plan-actions"><button class="btn-secondary" type="button" onclick="applyHAClusterTemplate(\'' + nodeID + '\')">' + tr('haNodePlanApply') + '</button><button class="btn-ghost" type="button" onclick="copyHANodeYaml(\'' + nodeID + '\')">' + tr('haCopyYaml') + '</button><button class="btn-ghost" type="button" onclick="copyHANodeChecklist(\'' + nodeID + '\')">' + tr('haCopyChecklist') + '</button></div>'
          + '</div>';
      });
      root.innerHTML = cards.join('');
    }

    function renderHAPeerConfigRows(peers){
      const root = document.getElementById('haPeerConfigRows');
      if(!root) return;
      const list = normalizedPeerSummary(peers);
      root.innerHTML = list.map(function(peer, index){
        return '<div class="item ha-peer-config-card">'
          + '<div class="ha-peer-config-top meta-spaced"><div class="inline-check"><label class="inline-label">' + tr('haPeerEnabled') + '</label><input type="checkbox" class="auto-check" data-ha-peer-enabled ' + (peer.enabled ? 'checked' : '') + '></div><div class="actions"><button class="btn-danger" type="button" onclick="removeHAPeerRow(' + index + ')">' + tr('haRemovePeer') + '</button></div></div>'
          + '<div class="grid3">'
          + '<div><label>' + tr('haPeerID') + '</label><input data-ha-peer-node-id value="' + escapeHtml(peer.node_id || '') + '"></div>'
          + '<div><label>' + tr('haPeerName') + '</label><input data-ha-peer-name value="' + escapeHtml(peer.name || '') + '"></div>'
          + '<div><label>' + tr('haPeerURL') + '</label><input data-ha-peer-base-url value="' + escapeHtml(peer.base_url || '') + '"></div>'
          + '</div>'
          + '</div>';
      }).join('');
    }
    function buildHAConfigSummary(cfg){
      const peers = normalizedPeerSummary(cfg.peers);
      const secretRaw = String(cfg.cluster_secret || '');
      const secretMasked = secretRaw ? '*'.repeat(Math.min(Math.max(secretRaw.length, 8), 16)) : tr('haSummaryEmptyValue');
      const lines = [
        tr('haSummaryEnabled', {value: String(!!cfg.enabled)}),
        tr('haSummaryNodeID', {value: cfg.node_id || ''}),
        tr('haSummaryNodeName', {value: cfg.node_name || ''}),
        tr('haSummaryAdvertiseURL', {value: cfg.advertise_url || ''}),
        tr('haSummaryClusterSecret', {value: secretMasked}),
        tr('haSummarySyncIntervalSeconds', {value: String(cfg.sync_interval_seconds || 0)}),
        tr('haSummaryPushDebounceSeconds', {value: String(cfg.push_debounce_seconds || 0)}),
        tr('haSummaryPullBatchSize', {value: String(cfg.pullBatchSize || cfg.pull_batch_size || 0)}),
        tr('haSummaryHeartbeatSyncMinIntervalSeconds', {value: String(cfg.heartbeat_sync_min_interval_seconds || 0)}),
        tr('haSummaryPeerCount', {value: String(peers.length)})
      ];
      peers.forEach(function(peer, index){
        lines.push(tr('haSummaryPeerLine', {index: String(index), value: [peer.enabled, peer.node_id, peer.name, peer.base_url].join(' | ')}));
      });
      return lines.join('\n');
    }
    function renderHAConfigSummary(cfg){
      const el = document.getElementById('haConfigSummary');
      if(!el) return;
      el.value = buildHAConfigSummary(cfg || {});
    }
    function buildHAYamlSnippet(cfg){
      const peers = normalizedPeerSummary(cfg.peers);
      const lines = [
        'ha:',
        '  enabled: ' + (!!cfg.enabled),
        '  node_id: ' + (cfg.node_id || ''),
        '  node_name: ' + (cfg.node_name || ''),
        '  advertise_url: ' + (cfg.advertise_url || ''),
        '  cluster_secret: ' + (cfg.cluster_secret || ''),
        '  sync_interval_seconds: ' + String(cfg.sync_interval_seconds || 0),
        '  push_debounce_seconds: ' + String(cfg.push_debounce_seconds || 0),
        '  pull_batch_size: ' + String(cfg.pull_batch_size || 0),
        '  heartbeat_sync_min_interval_seconds: ' + String(cfg.heartbeat_sync_min_interval_seconds || 0)
      ];
      if(!peers.length){
        lines.push('  peers: []');
      } else {
        lines.push('  peers:');
        peers.forEach(function(peer){
          lines.push('    - node_id: ' + (peer.node_id || ''));
          lines.push('      name: ' + (peer.name || ''));
          lines.push('      base_url: ' + (peer.base_url || ''));
          lines.push('      enabled: ' + (!!peer.enabled));
        });
      }
      return lines.join('\n');
    }
    async function copyHAYamlSnippet(){
      try {
        const text = buildHAYamlSnippet(haConfigCache || collectHAConfig());
        if(navigator.clipboard && navigator.clipboard.writeText){
          await navigator.clipboard.writeText(text);
        } else {
          const el = document.getElementById('haConfigSummary');
          if(el){
            const previous = el.value;
            el.value = text;
            el.focus();
            el.select();
            document.execCommand('copy');
            el.value = previous;
          }
        }
        const msg = tr('haCopyYamlDone');
        setOutput(msg);
        showToast(msg, 'success');
      } catch (err) {
        const msg = tr('haCopyYamlFailed', {error: err.message});
        setOutput(msg);
        showToast(msg, 'error');
      }
    }
    async function copyHAConfigSummary(){
      try {
        const text = buildHAConfigSummary(haConfigCache || collectHAConfig());
        if(navigator.clipboard && navigator.clipboard.writeText){
          await navigator.clipboard.writeText(text);
        } else {
          const el = document.getElementById('haConfigSummary');
          if(el){
            el.focus();
            el.select();
            document.execCommand('copy');
          }
        }
        const msg = tr('haCopySummaryDone');
        setOutput(msg);
        showToast(msg, 'success');
      } catch (err) {
        const msg = tr('haCopySummaryFailed', {error: err.message});
        setOutput(msg);
        showToast(msg, 'error');
      }
    }
    function buildHAChecklist(cfg){
      const peers = normalizedPeerSummary(cfg.peers);
      const enabledPeers = peers.filter(function(peer){ return peer.enabled; });
      const lines = [
        tr('haChecklistTitle'),
        tr('haChecklistLocalNode', {value: cfg.node_id || ''}),
        tr('haChecklistLocalName', {value: cfg.node_name || ''}),
        tr('haChecklistLocalURL', {value: cfg.advertise_url || ''}),
        tr('haChecklistEnabled', {value: String(!!cfg.enabled)}),
        tr('haChecklistEnabledPeerCount', {value: String(enabledPeers.length)}),
        '',
        tr('haChecklistSharedChecks'),
        tr('haChecklistSecretShared'),
        tr('haChecklistUniqueNodeIDLine'),
        tr('haChecklistNoSelfPeerLine'),
        tr('haChecklistPeerReachableLine'),
        tr('haChecklistSaveThenRestartLine'),
        '',
        tr('haChecklistThisNodePeers')
      ];
      if(!enabledPeers.length){
        lines.push(tr('haChecklistNoEnabledPeersYet'));
      } else {
        enabledPeers.forEach(function(peer, index){
          lines.push(tr('haChecklistPeerEntry', {index: String(index), value: [peer.node_id, peer.name, peer.base_url].join(' | ')}));
        });
      }
      return lines.join('\n');
    }
    async function copyHAChecklist(){
      try {
        const text = buildHAChecklist(haConfigCache || collectHAConfig());
        if(navigator.clipboard && navigator.clipboard.writeText){
          await navigator.clipboard.writeText(text);
        } else {
          const el = document.getElementById('haConfigSummary');
          if(el){
            const previous = el.value;
            el.value = text;
            el.focus();
            el.select();
            document.execCommand('copy');
            el.value = previous;
          }
        }
        const msg = tr('haCopyChecklistDone');
        setOutput(msg);
        showToast(msg, 'success');
      } catch (err) {
        const msg = tr('haCopyChecklistFailed', {error: err.message});
        setOutput(msg);
        showToast(msg, 'error');
      }
    }
    function randomHASecret(){
      if(window.crypto && window.crypto.getRandomValues){
        const bytes = new Uint8Array(24);
        window.crypto.getRandomValues(bytes);
        return Array.from(bytes).map(function(v){ return v.toString(16).padStart(2, '0'); }).join('');
      }
      return 'hc-' + Math.random().toString(36).slice(2) + Date.now().toString(36);
    }
    function renderHAClusterSecretHint(secret){
      const el = document.getElementById('haClusterSecretHint');
      if(!el) return;
      const value = String(secret || '').trim();
      if(!value){
        el.textContent = tr('haSecretHintEmpty');
        return;
      }
      el.textContent = value.length >= 24 ? tr('haSecretHintStrong') : tr('haSecretHintWeak');
    }
    async function copyHAClusterSecret(){
      try {
        const input = document.getElementById('haClusterSecret');
        const value = String(input?.value || '').trim();
        if(!value){
          renderHAClusterSecretHint('');
          return;
        }
        if(navigator.clipboard && navigator.clipboard.writeText){
          await navigator.clipboard.writeText(value);
        } else if(input) {
          const previousType = input.type;
          input.type = 'text';
          input.focus();
          input.select();
          document.execCommand('copy');
          input.type = previousType;
        }
        const msg = tr('haCopySecretDone');
        setOutput(msg);
        showToast(msg, 'success');
      } catch (err) {
        const msg = tr('haCopySecretFailed', {error: err.message});
        setOutput(msg);
        showToast(msg, 'error');
      }
    }
    function toggleHAClusterSecretVisibility(){
      const input = document.getElementById('haClusterSecret');
      const btn = document.getElementById('toggleHAClusterSecretButton');
      if(!input || !btn) return;
      const show = input.type === 'password';
      input.type = show ? 'text' : 'password';
      btn.textContent = tr(show ? 'haHideSecret' : 'haToggleSecret');
    }
    function generateHAClusterSecret(){
      try {
        const input = document.getElementById('haClusterSecret');
        if(!input) return;
        input.value = randomHASecret();
        renderHAClusterSecretHint(input.value);
        refreshHAConfigSummaryFromForm();
        const msg = tr('haSecretGenerated');
        setOutput(msg);
        showToast(msg, 'success');
      } catch (err) {
        const msg = tr('haSecretGenerationFailed', {error: err.message});
        setOutput(msg);
        showToast(msg, 'error');
      }
    }
    function applyHAClusterTemplate(nodeID){
      const template = haNodeTemplates[nodeID];
      if(!template) return;
      const current = haConfigCache || {};
      const merged = {
        enabled: true,
        self_fqdn: normalizeHAFQDN(template.advertise_url),
        node_id: template.node_id,
        node_name: haNodeDisplayName(template.node_id),
        advertise_url: template.advertise_url,
        cluster_secret: current.cluster_secret || '',
        sync_interval_seconds: Number(current.sync_interval_seconds || 5),
        push_debounce_seconds: Number(current.push_debounce_seconds || 5),
        pull_batch_size: Number(current.pull_batch_size || 1000),
        heartbeat_sync_min_interval_seconds: Number(current.heartbeat_sync_min_interval_seconds || 60),
        peers: template.peers.map(function(peer){ return Object.assign({}, peer, { name: haNodeDisplayName(peer.node_id) }); })
      };
      renderHAConfig(merged);
      const msg = tr('haTemplateApplied', {node: nodeID});
      setOutput(msg);
      showToast(msg, 'success');
    }
    function addHAPeerRow(){
      const peers = (haConfigCache && Array.isArray(haConfigCache.peers)) ? haConfigCache.peers.slice() : [];
      peers.push({enabled:true,node_id:'',name:'',base_url:''});
      haConfigCache = Object.assign({}, haConfigCache || {}, { peers });
      renderHAPeerConfigRows(peers);
      refreshHAConfigSummaryFromForm();
    }
    function removeHAPeerRow(index){
      const peers = (haConfigCache && Array.isArray(haConfigCache.peers)) ? haConfigCache.peers.slice() : [];
      if(index >= 0 && index < peers.length) peers.splice(index, 1);
      haConfigCache = Object.assign({}, haConfigCache || {}, { peers });
      renderHAPeerConfigRows(peers);
      refreshHAConfigSummaryFromForm();
    }
    function renderHAConfig(cfg){
      haConfigCache = cfg || {};
      document.getElementById('haEnabled').checked = !!cfg.enabled;
      document.getElementById('haNodeID').value = cfg.node_id || '';
      document.getElementById('haNodeName').value = cfg.node_name || '';
      document.getElementById('haAdvertiseURL').value = cfg.advertise_url || '';
      document.getElementById('haClusterSecret').value = cfg.cluster_secret || '';
      const secretInput = document.getElementById('haClusterSecret');
      const secretToggleBtn = document.getElementById('toggleHAClusterSecretButton');
      if(secretInput) secretInput.type = 'password';
      if(secretToggleBtn) secretToggleBtn.textContent = tr('haToggleSecret');
      renderHAClusterSecretHint(cfg.cluster_secret || '');
      document.getElementById('haSyncIntervalSeconds').value = String(cfg.sync_interval_seconds || 5);
      document.getElementById('haPushDebounceSeconds').value = String(cfg.push_debounce_seconds || 5);
      document.getElementById('haPullBatchSize').value = String(cfg.pull_batch_size || 1000);
      document.getElementById('haHeartbeatSyncMinIntervalSeconds').value = String(cfg.heartbeat_sync_min_interval_seconds || 60);
      renderHAPeerConfigRows(cfg.peers || []);
      renderHAConfigSummary(cfg || {});
      renderHAConfigReadiness(cfg || {});
      renderHANodePlans(cfg || {});
      renderHARuntimeHint();
      renderHAOverview(haStatusCache, cfg || {});
    }
    function collectHAConfig(){
      const peerNodes = Array.from(document.querySelectorAll('#haPeerConfigRows .item')).map(function(item){
        return {
          enabled: !!item.querySelector('[data-ha-peer-enabled]')?.checked,
          node_id: item.querySelector('[data-ha-peer-node-id]')?.value?.trim() || '',
          name: item.querySelector('[data-ha-peer-name]')?.value?.trim() || '',
          base_url: item.querySelector('[data-ha-peer-base-url]')?.value?.trim() || ''
        };
      }).filter(function(peer){ return peer.enabled || peer.node_id || peer.name || peer.base_url; });
      const payload = {
        enabled: !!document.getElementById('haEnabled').checked,
        self_fqdn: normalizeHAFQDN(document.getElementById('haAdvertiseURL').value.trim() || (haConfigCache && (haConfigCache.self_fqdn || haConfigCache.suggested_self_fqdn))),
        node_id: document.getElementById('haNodeID').value.trim(),
        node_name: document.getElementById('haNodeName').value.trim(),
        advertise_url: document.getElementById('haAdvertiseURL').value.trim(),
        cluster_secret: document.getElementById('haClusterSecret').value.trim(),
        sync_interval_seconds: Number(document.getElementById('haSyncIntervalSeconds').value || '0'),
        push_debounce_seconds: Number(document.getElementById('haPushDebounceSeconds').value || '0'),
        pull_batch_size: Number(document.getElementById('haPullBatchSize').value || '0'),
        heartbeat_sync_min_interval_seconds: Number(document.getElementById('haHeartbeatSyncMinIntervalSeconds').value || '0'),
        peers: peerNodes
      };
      payload.nodes = buildHANodesFromPeers(payload);
      if(payload.enabled){
        const enabledPeers = peerNodes.filter(function(peer){ return peer.enabled && peer.node_id && peer.base_url; });
        if(!payload.node_id || !payload.advertise_url || !payload.cluster_secret || !enabledPeers.length){
          throw new Error(tr('haConfigInvalid'));
        }
        const seenPeerIDs = new Set();
        const seenPeerURLs = new Set();
        enabledPeers.forEach(function(peer){
          const peerID = String(peer.node_id || '').trim();
          const peerURL = String(peer.base_url || '').replace(/\/+$/, '').trim();
          if(peerID === payload.node_id){
            throw new Error(tr('haConfigSelfPeer', {node: peerID || payload.node_id}));
          }
          if(seenPeerIDs.has(peerID)){
            throw new Error(tr('haConfigDuplicatePeerID', {value: peerID}));
          }
          if(seenPeerURLs.has(peerURL)){
            throw new Error(tr('haConfigDuplicatePeerURL', {value: peerURL}));
          }
          seenPeerIDs.add(peerID);
          seenPeerURLs.add(peerURL);
        });
      }
      return payload;
    }
    let haConfigInFlight = null;
    async function loadHAConfig(){
      if (haConfigInFlight) return haConfigInFlight;
      haConfigInFlight = (async function(){
      try {
        const data = await api('/api/admin/ha/config');
        renderHAConfig(data || {});
      } catch (err) {
        const msg = tr('haConfigLoadFailed', {error: err.message});
        setOutput(msg);
        showToast(msg, 'error');
      }
      })();
      try { return await haConfigInFlight; }
      finally { haConfigInFlight = null; }
    }
    function refreshHAConfigSummaryFromForm(){
      try {
        const next = collectHAConfig();
        renderHAConfigSummary(next);
        renderHAConfigReadiness(next);
      } catch (_) {
        const next = Object.assign({}, haConfigCache || {}, {
          enabled: !!document.getElementById('haEnabled')?.checked,
          node_id: document.getElementById('haNodeID')?.value || '',
          node_name: document.getElementById('haNodeName')?.value || '',
          advertise_url: document.getElementById('haAdvertiseURL')?.value || '',
          cluster_secret: document.getElementById('haClusterSecret')?.value || '',
          sync_interval_seconds: Number(document.getElementById('haSyncIntervalSeconds')?.value || '0'),
          push_debounce_seconds: Number(document.getElementById('haPushDebounceSeconds')?.value || '0'),
          pull_batch_size: Number(document.getElementById('haPullBatchSize')?.value || '0'),
          heartbeat_sync_min_interval_seconds: Number(document.getElementById('haHeartbeatSyncMinIntervalSeconds')?.value || '0'),
          peers: Array.from(document.querySelectorAll('#haPeerConfigRows .item')).map(function(item){
            return {
              enabled: !!item.querySelector('[data-ha-peer-enabled]')?.checked,
              node_id: item.querySelector('[data-ha-peer-node-id]')?.value || '',
              name: item.querySelector('[data-ha-peer-name]')?.value || '',
              base_url: item.querySelector('[data-ha-peer-base-url]')?.value || ''
            };
          })
        });
        renderHAConfigSummary(next);
        renderHAConfigReadiness(next);
      }
    }
    async function saveHAConfig(){
      const btn = document.getElementById('saveHAConfigButton');
      const previous = btn ? btn.textContent : '';
      try {
        const payload = collectHAConfig();
        if(btn){ btn.disabled = true; btn.textContent = tr('haSavingConfig'); }
        const data = await api('/api/admin/ha/config', { method: 'POST', body: JSON.stringify(payload) });
        renderHAConfig(data || payload);
        await loadHAStatus();
        const msg = tr('haConfigSaved');
        setOutput(msg);
        showToast(msg, 'success');
      } catch (err) {
        const msg = tr('haConfigSaveFailed', {error: err.message});
        setOutput(msg);
        showToast(msg, 'error');
      } finally {
        if(btn){ btn.disabled = false; btn.textContent = previous || tr('haSaveConfig'); }
      }
    }
    let haStatusInFlight = null;
    async function loadHAStatus(){
      if (haStatusInFlight) return haStatusInFlight;
      haStatusInFlight = (async function(){
      const overview = document.getElementById('haOverviewGrid');
      const summary = document.getElementById('haSummaryList');
      const peers = document.getElementById('haPeerList');
      const syncDetails = document.getElementById('haSyncDetailList');
      if(overview) overview.setAttribute('aria-busy', 'true');
      [summary, peers, syncDetails].forEach(el => { if(el) el.setAttribute('aria-busy', 'true'); });
      if(summary) summary.innerHTML = '<div class="hint">' + tr('haLoading') + '</div>';
      if(peers) peers.innerHTML = '<div class="hint">' + tr('haLoading') + '</div>';
      if(syncDetails) syncDetails.innerHTML = '<div class="hint">' + tr('haLoading') + '</div>';
      try {
        const data = await api('/api/admin/ha/status');
        haStatusCache = data || {};
        renderHAStatus(haStatusCache);
        renderHARuntimeHint();
        renderHAOverview(haStatusCache, haConfigCache);
      } catch (err) {
        const msg = tr('haLoadFailed', {error: err.message});
        if(overview) overview.setAttribute('aria-busy', 'false');
        [summary, peers, syncDetails].forEach(el => { if(el) el.setAttribute('aria-busy', 'false'); });
        if(summary) summary.innerHTML = '<div class="hint">' + escapeHtml(msg) + '</div>';
        if(peers) peers.innerHTML = '<div class="hint">' + escapeHtml(msg) + '</div>';
        if(syncDetails) syncDetails.innerHTML = '<div class="hint">' + escapeHtml(msg) + '</div>';
        setOutput(msg);
      }
      })();
      try { return await haStatusInFlight; }
      finally { haStatusInFlight = null; }
    }
    const _baseApplyI18nHA = applyI18n;
    applyI18n = function(){
      _baseApplyI18nHA();
      renderHAClusterSecretHint(document.getElementById('haClusterSecret')?.value || '');
      renderHAConfigReadiness(haConfigCache || {});
      renderHARuntimeHint();
    };
    const _baseRefreshAllHA = refreshAll;
    refreshAll = async function(){ await Promise.all([_baseRefreshAllHA(), loadHAStatus(), loadHAConfig()]); };
    document.addEventListener('input', function(event){ if(event.target && event.target.closest && event.target.closest('#tab-ha')) { if(event.target.id === 'haClusterSecret') renderHAClusterSecretHint(event.target.value || ''); refreshHAConfigSummaryFromForm(); } });
    document.addEventListener('change', function(event){ if(event.target && event.target.closest && event.target.closest('#tab-ha')) refreshHAConfigSummaryFromForm(); });
    applyI18n();
    if (token() && document.getElementById('tab-ha') && document.getElementById('tab-ha').classList.contains('active')) { loadHAStatus(); loadHAConfig(); }

    // ─── MCP Catalog Management ───────────────────────────────────────────────
    let mcpCatalogData = [];
    let mcpEditMode = 'remote'; // 'remote' | 'local'
    let mcpCurrentValidateId = '';
    let catalogActiveSubTab = 'skill'; // 'skill' | 'mcp'

    function switchCatalogSubTab(tab) {
      catalogActiveSubTab = tab;
      document.getElementById('catalogSubTabSkill').className = tab === 'skill' ? 'btn-secondary' : 'btn-ghost';
      document.getElementById('catalogSubTabMCP').className = tab === 'mcp' ? 'btn-secondary' : 'btn-ghost';
      document.getElementById('catalogSubTabSkill').setAttribute('aria-pressed', tab === 'skill' ? 'true' : 'false');
      document.getElementById('catalogSubTabMCP').setAttribute('aria-pressed', tab === 'mcp' ? 'true' : 'false');
      document.getElementById('catalogSubViewSkill').classList.toggle('hidden-view', tab !== 'skill');
      document.getElementById('catalogSubViewMCP').classList.toggle('hidden-view', tab !== 'mcp');
      if (tab === 'mcp') loadMCPCatalog();
    }

    function reloadCurrentCatalogSubTab() {
      if (catalogActiveSubTab === 'mcp') loadMCPCatalog();
      else if (typeof loadSkillHubList === 'function') { loadSkillHubList(); if (typeof applyImportI18n === 'function') applyImportI18n(); }
    }

    function setMCPType(type) {
      mcpEditMode = type;
      document.getElementById('mcpTypeRemoteBtn').className = type === 'remote' ? 'btn-secondary' : 'btn-ghost';
      document.getElementById('mcpTypeLocalBtn').className = type === 'local' ? 'btn-secondary' : 'btn-ghost';
      document.getElementById('mcpTypeRemoteBtn').setAttribute('aria-pressed', type === 'remote' ? 'true' : 'false');
      document.getElementById('mcpTypeLocalBtn').setAttribute('aria-pressed', type === 'local' ? 'true' : 'false');
      document.getElementById('mcpRemoteFields').classList.toggle('hidden-view', type !== 'remote');
      document.getElementById('mcpLocalFields').classList.toggle('hidden-view', type !== 'local');
    }

    function showMCPEditor(item) {
      document.getElementById('mcpEditorWrap').classList.remove('hidden-view');
      document.getElementById('mcpEditorStatus').className = 'sm-status';
      document.getElementById('mcpEditorStatus').textContent = '';
      // Always clear all fields first to prevent cross-type data leakage
      document.getElementById('mcpEditId').value = '';
      document.getElementById('mcpEditName').value = '';
      document.getElementById('mcpEditDesc').value = '';
      document.getElementById('mcpEditEndpoint').value = '';
      document.getElementById('mcpEditTransport').value = 'streamable-http';
      document.getElementById('mcpEditAuth').value = 'none';
      document.getElementById('mcpEditApiKey').value = '';
      document.getElementById('mcpEditCommand').value = '';
      document.getElementById('mcpEditArgs').value = '';
      document.getElementById('mcpEditEnv').value = '';
      if (item) {
        document.getElementById('mcpEditId').value = item.id || item.capability_id || '';
        document.getElementById('mcpEditName').value = item.display_name || item.mcp?.name || '';
        document.getElementById('mcpEditDesc').value = item.description || '';
        var isLocal = !!(item.mcp?.command);
        setMCPType(isLocal ? 'local' : 'remote');
        if (isLocal) {
          document.getElementById('mcpEditCommand').value = item.mcp.command || '';
          document.getElementById('mcpEditArgs').value = (item.mcp.args || []).join(' ');
          document.getElementById('mcpEditEnv').value = Object.entries(item.mcp.env || {}).map(function(e){return e[0]+'='+e[1]}).join(', ');
        } else {
          document.getElementById('mcpEditEndpoint').value = item.mcp?.endpoint_url || '';
          document.getElementById('mcpEditTransport').value = item.mcp?.transport || 'streamable-http';
          document.getElementById('mcpEditAuth').value = item.mcp?.auth_type || 'none';
          document.getElementById('mcpEditApiKey').value = item.mcp?.api_key || '';
        }
      } else {
        setMCPType('remote');
      }
      // Load or reset secret requirements
      if (item && Array.isArray(item.secret_requirements) && item.secret_requirements.length > 0) {
        _mcpSecretReqs = item.secret_requirements.map(function(s) { return { name: s.name || '', label: s.label || s.name || '', scope: s.scope || 'user', storage_policy: s.storage_policy || 'hub_or_local', required: s.required !== false }; });
      } else {
        _mcpSecretReqs = [];
      }
      renderMCPSecretReqs();
    }

    function hideMCPEditor() { document.getElementById('mcpEditorWrap').classList.add('hidden-view'); }

    var _mcpSecretReqs = [];
    function renderMCPSecretReqs() {
      var root = document.getElementById('mcpSecretReqList');
      if (!_mcpSecretReqs.length) { root.innerHTML = ''; return; }
      root.innerHTML = _mcpSecretReqs.map(function(s, i) {
        return '<div class="mcp-secret-req-item"><span class="mcp-secret-req-name">' + escapeHtml(s.name) + '</span><span class="mcp-secret-req-label">' + escapeHtml(s.label || '') + '</span><button class="btn-ghost mcp-secret-req-remove" onclick="removeMCPSecretReq(' + i + ')">×</button></div>';
      }).join('');
    }
    function addMCPSecretReq() {
      var name = document.getElementById('mcpSecretReqName').value.trim();
      if (!name) return;
      var label = document.getElementById('mcpSecretReqLabel2').value.trim();
      _mcpSecretReqs.push({ name: name, label: label || name, scope: 'user', storage_policy: 'hub_or_local', required: true });
      document.getElementById('mcpSecretReqName').value = '';
      document.getElementById('mcpSecretReqLabel2').value = '';
      renderMCPSecretReqs();
    }
    function removeMCPSecretReq(idx) { _mcpSecretReqs.splice(idx, 1); renderMCPSecretReqs(); }
    function resetMCPSecretReqs() { _mcpSecretReqs = []; renderMCPSecretReqs(); }

    function importMCPFromJSON() {
      var statusEl = document.getElementById('mcpJsonImportStatus');
      statusEl.className = 'muted-result mcp-json-status';
      var raw = document.getElementById('mcpJsonImportInput').value.trim();
      if (!raw) { statusEl.textContent = tr('mcpJsonImportEmpty') || 'Please paste JSON'; return; }
      var obj;
      try { obj = JSON.parse(raw); } catch (e) { statusEl.textContent = (tr('mcpJsonImportInvalid') || 'Invalid JSON') + ': ' + e.message; statusEl.className = 'muted-result mcp-json-status error'; return; }
      if (typeof obj !== 'object' || obj === null || Array.isArray(obj)) { statusEl.textContent = tr('mcpJsonImportInvalid') || 'JSON must be an object'; statusEl.className = 'muted-result mcp-json-status error'; return; }
      // Unwrap nested formats: {"mcpServers":{"name":{...}}} or {"name":{...}} where value has command/endpoint_url
      if (!obj.command && !obj.endpoint_url && !obj.url && !obj.transport) {
        var keys = Object.keys(obj);
        // {"mcpServers": {...}} wrapper
        if (keys.length === 1 && keys[0] === 'mcpServers' && typeof obj.mcpServers === 'object') {
          obj = obj.mcpServers;
          keys = Object.keys(obj);
        }
        // {"serverName": {config...}} — take first entry
        if (keys.length >= 1) {
          var firstVal = obj[keys[0]];
          if (firstVal && typeof firstVal === 'object' && !Array.isArray(firstVal) && (firstVal.command || firstVal.endpoint_url || firstVal.url || firstVal.transport)) {
            if (!firstVal.name && !firstVal.id) firstVal.name = keys[0];
            obj = firstVal;
          }
        }
      }
      // Fill form fields from parsed JSON
      var name = obj.name || obj.id || obj.serverName || '';
      if (name) document.getElementById('mcpEditName').value = name;
      if (obj.description) document.getElementById('mcpEditDesc').value = obj.description;
      var filled = false;
      if (obj.command || obj.transport === 'stdio') {
        setMCPType('local');
        if (obj.command) { document.getElementById('mcpEditCommand').value = obj.command; filled = true; }
        if (obj.args) document.getElementById('mcpEditArgs').value = Array.isArray(obj.args) ? obj.args.join(' ') : String(obj.args);
        if (obj.env && typeof obj.env === 'object') document.getElementById('mcpEditEnv').value = Object.entries(obj.env).map(function(kv) { return kv[0] + '=' + kv[1]; }).join(', ');
      } else {
        setMCPType('remote');
        var endpoint = obj.endpoint_url || obj.url || obj.endpointUrl || '';
        if (endpoint) { document.getElementById('mcpEditEndpoint').value = endpoint; filled = true; }
        var transport = obj.transport || obj.type || '';
        if (transport) document.getElementById('mcpEditTransport').value = transport === 'http' ? 'streamable-http' : transport;
        if (obj.auth_type || obj.authType) document.getElementById('mcpEditAuth').value = obj.auth_type || obj.authType || 'none';
        var apiKey = obj.api_key || obj.apiKey || obj.auth_secret || '';
        if (!apiKey && obj.headers && typeof obj.headers === 'object') {
          var authHeader = obj.headers['Authorization'] || obj.headers['authorization'] || '';
          if (authHeader.toLowerCase().indexOf('bearer ') === 0) { apiKey = authHeader.slice(7); document.getElementById('mcpEditAuth').value = 'bearer'; }
          else if (authHeader) { apiKey = authHeader; document.getElementById('mcpEditAuth').value = 'api_key'; }
        }
        if (apiKey) document.getElementById('mcpEditApiKey').value = apiKey;
      }
      if (!filled && !name) { statusEl.textContent = (tr('mcpJsonImportInvalid') || 'Invalid JSON') + ': missing command or endpoint_url'; statusEl.className = 'muted-result mcp-json-status error'; return; }
      statusEl.className = 'muted-result mcp-json-status ok';
      statusEl.textContent = tr('mcpJsonImportOk') || 'JSON imported successfully';
      document.getElementById('mcpJsonImportInput').value = '';
    }

    async function saveMCP() {
      var status = document.getElementById('mcpEditorStatus');
      var name = document.getElementById('mcpEditName').value.trim();
      if (!name) {
        status.className = 'sm-status show error';
        status.textContent = tr('mcpName') + ' is required';
        return;
      }
      var existingId = document.getElementById('mcpEditId').value.trim();
      var isUpdate = !!existingId;
      var id = existingId || name.toLowerCase().replace(/[^a-z0-9]+/g, '-');
      var desc = document.getElementById('mcpEditDesc').value.trim();
      var mcpObj = { name: name, id: id };
      if (mcpEditMode === 'remote') {
        mcpObj.endpoint_url = document.getElementById('mcpEditEndpoint').value.trim();
        if (!mcpObj.endpoint_url) {
          status.className = 'sm-status show error';
          status.textContent = tr('mcpEndpoint') + ' is required';
          return;
        }
        mcpObj.transport = document.getElementById('mcpEditTransport').value;
        mcpObj.auth_type = document.getElementById('mcpEditAuth').value;
        var apiKey = document.getElementById('mcpEditApiKey').value.trim();
        if (apiKey) mcpObj.api_key = apiKey;
      } else {
        mcpObj.command = document.getElementById('mcpEditCommand').value.trim();
        if (!mcpObj.command) {
          status.className = 'sm-status show error';
          status.textContent = tr('mcpCommand') + ' is required';
          return;
        }
        var argsStr = document.getElementById('mcpEditArgs').value.trim();
        if (argsStr) mcpObj.args = argsStr.split(/\s+/);
        var envStr = document.getElementById('mcpEditEnv').value.trim();
        if (envStr) {
          mcpObj.env = {};
          envStr.split(',').forEach(function(pair) { var kv = pair.trim().split('='); if (kv.length >= 2) mcpObj.env[kv[0].trim()] = kv.slice(1).join('=').trim(); });
        }
        mcpObj.transport = 'stdio';
      }
      var body = { id: id, capability_id: id, display_name: name, description: desc, mcp: mcpObj, secret_requirements: _mcpSecretReqs.length > 0 ? _mcpSecretReqs : [] };
      try {
        // Validate remote MCP before saving
        if (mcpEditMode === 'remote') {
          status.className = 'sm-status show loading';
          status.textContent = tr('mcpValidating');
          var valRes = await api('/api/admin/capability-market/mcp/validate', { method: 'POST', body: JSON.stringify({ endpoint_url: mcpObj.endpoint_url, transport: mcpObj.transport, api_key: mcpObj.api_key || '' }) });
          if (valRes && valRes.overall_status === 'fail') {
            status.className = 'sm-status show error';
            status.textContent = tr('mcpValidateFail') + ': ' + (valRes.summary || '');
            return;
          }
        } else {
          status.className = 'sm-status show loading';
          status.textContent = tr('mcpSave') + '...';
        }
        var method = isUpdate ? 'PUT' : 'POST';
        var url = isUpdate ? '/api/admin/capability-market/mcp/' + encodeURIComponent(id) : '/api/admin/capability-market/mcp';
        await api(url, { method: method, body: JSON.stringify(body) });
        status.className = 'sm-status show';
        status.textContent = tr('mcpSaved');
        hideMCPEditor();
        loadMCPCatalog();
      } catch (err) {
        status.className = 'sm-status show error';
        status.textContent = String(err.message || err);
      }
    }

    async function deleteMCP(id) {
      if (!confirm(tr('mcpDeleteConfirm'))) return;
      try {
        await api('/api/admin/capability-market/mcp/' + encodeURIComponent(id), { method: 'DELETE' });
        loadMCPCatalog();
      } catch (err) { alert(String(err.message || err)); }
    }

    async function validateMCP(id) {
      mcpCurrentValidateId = id;
      var item = mcpCatalogData.find(function(m) { return m.id === id || m.capability_id === id; });
      if (!item || !item.mcp || !item.mcp.endpoint_url) { alert('No endpoint URL'); return; }
      var panel = document.getElementById('mcpValidationResult');
      panel.classList.remove('hidden-view');
      document.getElementById('mcpValTitle').textContent = tr('mcpValidate') + ': ' + (item.display_name || id);
      document.getElementById('mcpValChecks').innerHTML = '<div class="hint">' + tr('mcpValidating') + '</div>';
      document.getElementById('mcpValTools').innerHTML = '';
      try {
        var report = await api('/api/admin/capability-market/mcp/validate', { method: 'POST', body: JSON.stringify({ endpoint_url: item.mcp.endpoint_url, transport: item.mcp.transport || 'streamable-http', api_key: item.mcp.api_key || '' }) });
        renderMCPValidation(report);
      } catch (err) {
        document.getElementById('mcpValChecks').innerHTML = '<div class="status-card danger"><strong>' + tr('mcpValidateFail') + '</strong><p>' + esc(String(err.message || err)) + '</p></div>';
      }
    }

    function renderMCPValidation(report) {
      if (!report) return;
      var checks = document.getElementById('mcpValChecks');
      var html = '';
      (report.checks || []).forEach(function(c) {
        var cls = c.status === 'pass' ? 'ok' : c.status === 'warn' ? 'warn' : 'danger';
        html += '<div class="status-card ' + cls + '"><strong>' + esc(c.name || c.check) + '</strong><p>' + esc(c.message || c.status) + (c.latency_ms ? ' (' + c.latency_ms + 'ms)' : '') + '</p></div>';
      });
      checks.innerHTML = html || '<div class="status-card ok"><strong>' + tr('mcpValidateSuccess') + '</strong></div>';
      var tools = report.tools || [];
      if (tools.length) {
        var toolHtml = '<div class="item-title">' + tr('mcpTools') + ' (' + tools.length + ')</div><div class="list">';
        tools.forEach(function(tool) { toolHtml += '<div class="item"><strong>' + esc(tool.name || '-') + '</strong><div class="item-meta">' + esc(tool.description || '') + '</div></div>'; });
        toolHtml += '</div>';
        document.getElementById('mcpValTools').innerHTML = toolHtml;
      }
    }

    function closeMCPValidation() { document.getElementById('mcpValidationResult').classList.add('hidden-view'); }
    function revalidateMCP() { if (mcpCurrentValidateId) validateMCP(mcpCurrentValidateId); }

    async function loadMCPCatalog() {
      var list = document.getElementById('mcpCatalogList');
      list.innerHTML = '<div class="hint">' + tr('mcpCatalogEmpty') + '</div>';
      try {
        var data = await api('/api/capability-market/mcp?include_drafts=1');
        mcpCatalogData = data.items || [];
        if (!mcpCatalogData.length) { list.innerHTML = '<div class="hint">' + tr('mcpCatalogEmpty') + '</div>'; return; }
        list.innerHTML = '';
        list.className = 'list mcp-grid';
        mcpCatalogData.forEach(function(item, idx) {
          var isLocal = !!(item.mcp && item.mcp.command);
          var typeBadge = isLocal ? '<span class="badge warn">' + tr('mcpTypeLocal') + '</span>' : '<span class="badge ok">' + tr('mcpTypeRemote') + '</span>';
          var transport = item.mcp ? (item.mcp.transport || 'stdio') : '-';
          var card = document.createElement('div');
          card.className = 'skillhub-card';
          card.dataset.mcpIdx = String(idx);
          var actions = '<button class="btn-ghost" data-mcp-action="edit">' + tr('mcpEdit') + '</button>' +
            '<button class="btn-danger" data-mcp-action="delete">' + tr('mcpDelete') + '</button>';
          if (!isLocal) {
            actions = '<button class="btn-ghost" data-mcp-action="validate">' + tr('mcpValidate') + '</button>' + actions;
          }
          card.innerHTML = '<div class="skillhub-title" title="' + esc(item.display_name || item.capability_id) + '">' + esc(item.display_name || item.capability_id) + '</div>' +
            '<div class="skillhub-desc" title="' + esc(item.description || '') + '">' + esc(item.description || '') + '</div>' +
            '<div class="skillhub-stats">' + typeBadge + '<span class="skillhub-badge badge info">' + esc(transport) + '</span></div>' +
            '<div class="actions">' + actions + '</div>';
          list.appendChild(card);
        });
      } catch (err) { list.innerHTML = '<div class="hint danger-text">' + esc(String(err.message || err)) + '</div>'; }
    }

    // Event delegation for MCP card actions avoids inline onclick with string interpolation.
    document.getElementById('mcpCatalogList').addEventListener('click', function(e) {
      var btn = e.target.closest('[data-mcp-action]');
      if (!btn) return;
      var card = btn.closest('[data-mcp-idx]');
      if (!card) return;
      var idx = parseInt(card.dataset.mcpIdx, 10);
      var item = mcpCatalogData[idx];
      if (!item) return;
      var action = btn.dataset.mcpAction;
      if (action === 'validate') validateMCP(item.id || item.capability_id);
      else if (action === 'edit') showMCPEditor(item);
      else if (action === 'delete') deleteMCP(item.id || item.capability_id);
    });

    // Auto-load the restored catalog tab after this deferred module finishes loading.
    if (token() && document.getElementById('tab-skillhub')?.classList.contains('active') && !window._skillhubAdminLoaded) {
      setTimeout(reloadCurrentCatalogSubTab, 0);
    }
