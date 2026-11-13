(function (global) {
  'use strict';

  var copy = {
    zh: {
      title: 'Bot \u7ba1\u7406',
      subtitle: '\u5bf9\u63a5 MaClawSrv\uff0c\u5e76\u8bbe\u7f6e Bot \u4f7f\u7528\u7684 Docker \u670d\u52a1\u4e0e LLM\u3002Bot \u7531\u7528\u6237\u5728 MaClaw \u5ba2\u6237\u7aef\u521b\u5efa\u3002',
      nav: 'Bot \u7ba1\u7406',
      navDesc: '\u5bf9\u63a5 MaClawSrv\u3001Docker \u670d\u52a1\u4e0e LLM',
      connection: '\u5bf9\u63a5 MaClawSrv',
      url: 'MaClawSrv \u5730\u5740',
      token: '\u8bbf\u95ee\u4ee4\u724c',
      tokenHint: '\u7559\u7a7a\u5219\u4e0d\u4fee\u6539\u5df2\u4fdd\u5b58\u7684\u4ee4\u724c',
      tokenSet: '\u4ee4\u724c\u5df2\u4fdd\u5b58',
      tokenMissing: '\u5c1a\u672a\u4fdd\u5b58\u4ee4\u724c',
      adminSecret: '\u7ba1\u7406\u5bc6\u94a5',
      adminHint: '\u7528\u4e8e\u6309\u7528\u6237\u521b\u5efa MaClawSrv \u8d26\u53f7\u3002\u7559\u7a7a\u5219\u4e0d\u4fee\u6539\u3002',
      adminSet: '\u7ba1\u7406\u5bc6\u94a5\u5df2\u4fdd\u5b58',
      adminMissing: '\u5c1a\u672a\u4fdd\u5b58\u7ba1\u7406\u5bc6\u94a5',
      save: '\u4fdd\u5b58\u5bf9\u63a5\u8bbe\u7f6e',
      test: '\u6d4b\u8bd5\u8fde\u63a5',
      saved: '\u5df2\u4fdd\u5b58',
      connected: '\u8fde\u63a5\u6210\u529f\uff0c\u5f53\u524d\u5b9e\u4f8b {count} \u4e2a',
      testing: '\u6b63\u5728\u6d4b\u8bd5...',
      needConfig: '\u8bf7\u5148\u4fdd\u5b58 MaClawSrv \u5730\u5740\u548c\u8bbf\u95ee\u4ee4\u724c',
      saveBeforeTest: '\u5730\u5740\u6216\u4ee4\u724c\u8fd8\u6ca1\u4fdd\u5b58\uff0c\u8bf7\u5148\u4fdd\u5b58\u518d\u6d4b\u8bd5',
      srvRejected: 'MaClawSrv \u62d2\u7edd\u4e86\u672c\u6b21\u8bf7\u6c42',
      srvTokenRejected: '\u8bbf\u95ee\u4ee4\u724c\u88ab MaClawSrv \u62d2\u7edd\uff08HTTP 401\uff09\u3002\u4ee4\u724c\u6709\u65f6\u6548\uff1a\u4fdd\u5b58\u65b0\u4ee4\u724c\u6216\u4fdd\u5b58\u7ba1\u7406\u5bc6\u94a5\u540e\u6d4b\u8bd5\u8fde\u63a5\u53ef\u81ea\u52a8\u7eed\u671f',
      srvTokenForbidden: '\u8bbf\u95ee\u4ee4\u724c\u65e0\u6743\u6267\u884c\u8be5\u64cd\u4f5c\uff08HTTP 403\uff09',
      srvNotFound: '\u627e\u4e0d\u5230\u5b9e\u4f8b\u6216\u63a5\u53e3\uff08HTTP 404\uff09\uff0c\u8bf7\u68c0\u67e5\u5730\u5740\u662f\u5426\u6b63\u786e\u8f6c\u53d1 /api',
      bots: '\u7528\u6237\u521b\u5efa\u7684 Bot',
      botsHint: '\u5728 MaClaw \u5ba2\u6237\u7aef\u7684 Bot \u7ba1\u7406\u91cc\u521b\u5efa\uff0c\u8fd9\u91cc\u53ea\u67e5\u770b\u3002',
      empty: '\u8fd8\u6ca1\u6709 Bot\u3002\u7528\u6237\u5728 MaClaw \u5ba2\u6237\u7aef\u7684 Bot \u7ba1\u7406\u91cc\u521b\u5efa\u3002',
      needUrl: '\u8bf7\u586b\u5199 MaClawSrv \u5730\u5740',
      badUrl: 'MaClawSrv \u5730\u5740\u9700\u8981\u4ee5 http:// \u6216 https:// \u5f00\u5934',
      owner: '\u7528\u6237',
      access: '\u5f00\u901a\u8303\u56f4',
      accessHint: '\u9ed8\u8ba4\u5173\u95ed\u3002\u53ea\u6709\u8fd9\u91cc\u5f00\u901a\u7684\u7528\u6237\uff0c\u684c\u9762\u5de6\u4e0b\u89d2\u624d\u4f1a\u51fa\u73b0 Bot\u3002',
      global: '\u5168\u5c40\u5f00\u901a',
      department: '\u90e8\u95e8',
      user: '\u7528\u6237',
      orgTitle: '\u7ec4\u7ec7\u673a\u6784',
      orgHint: '\u5c55\u5f00\u90e8\u95e8\u540e\uff0c\u53ef\u4ee5\u5f00\u901a\u8fd9\u4e2a\u90e8\u95e8\uff08\u542b\u4e0b\u7ea7\u90e8\u95e8\uff09\uff0c\u6216\u53ea\u5f00\u901a\u5176\u4e2d\u67d0\u4e2a\u7528\u6237\u3002',
      orgSearch: '\u641c\u7d22\u90e8\u95e8\u6216\u7528\u6237',
      orgLoading: '\u6b63\u5728\u52a0\u8f7d\u7ec4\u7ec7\u673a\u6784...',
      orgEmpty: '\u8fd8\u6ca1\u6709\u7ec4\u7ec7\u673a\u6784\u3002',
      orgFailed: '\u7ec4\u7ec7\u673a\u6784\u52a0\u8f7d\u5931\u8d25',
      orgUsersFailed: '\u7528\u6237\u76ee\u5f55\u52a0\u8f7d\u5931\u8d25\uff0c\u6682\u65f6\u4e0d\u80fd\u6309\u7528\u6237\u5f00\u901a\u3002',
      orgReload: '\u91cd\u65b0\u52a0\u8f7d',
      orgAddDepartment: '\u5f00\u901a\u90e8\u95e8',
      orgAddUser: '\u5f00\u901a\u7528\u6237',
      orgAdded: '\u5df2\u5f00\u901a',
      orgNoAccount: '\u672a\u7ed1\u5b9a\u8d26\u53f7',
      orgMembersLoading: '\u6b63\u5728\u52a0\u8f7d\u6210\u5458...',
      orgMembersFailed: '\u6210\u5458\u52a0\u8f7d\u5931\u8d25\uff0c\u518d\u5c55\u5f00\u4e00\u6b21\u53ef\u4ee5\u91cd\u8bd5\u3002',
      orgNoMembers: '\u6ca1\u6709\u6210\u5458',
      orgSearchEmpty: '\u6ca1\u6709\u5339\u914d\u7684\u90e8\u95e8\u6216\u7528\u6237',
      orgSearchLimited: '\u53ea\u641c\u7d22\u4e86\u524d\u9762\u4e00\u90e8\u5206\u90e8\u95e8\u3002\u6362\u4e2a\u66f4\u5177\u4f53\u7684\u5173\u952e\u8bcd\u3002',
      orgSearching: '\u6b63\u5728\u641c\u7d22...',
      orgMatchUsers: '\u5339\u914d\u7684\u8d26\u53f7',
      orgMatchLimited: '\u53ea\u663e\u793a\u4e86\u524d 50 \u4e2a\u8d26\u53f7\u3002\u518d\u591a\u8f93\u5165\u51e0\u4e2a\u5b57\u3002',
      grantEmpty: '\u5f53\u524d\u6ca1\u6709\u5f00\u901a\u3002',
      instance: '\u5b9e\u4f8b',
      configured: '\u5df2\u914d\u7f6e',
      unconfigured: '\u672a\u914d\u7f6e',
      online: '\u5df2\u8fde\u901a',
      connectionHint: '\u586b\u5199 MaClawSrv \u7684\u5730\u5740\u4e0e\u8bbf\u95ee\u4ee4\u724c\uff0c\u4fdd\u5b58\u540e\u53ef\u6d4b\u8bd5\u8fde\u63a5\u3002',
      globalHint: '\u5168\u4f53\u6210\u5458\u90fd\u80fd\u4f7f\u7528 Bot',
      enableList: '\u5f00\u901a\u6e05\u5355',
      everyoneNote: '\u5df2\u5bf9\u5168\u4f53\u6210\u5458\u5f00\u653e\u3002\u5173\u95ed\u4e0a\u65b9\u5f00\u5173\uff0c\u5373\u53ef\u6309\u90e8\u95e8\u6216\u7528\u6237\u5f00\u901a\u3002',
      countUnit: '\u9879',
      botColumn: 'Bot',
      created: '\u521b\u5efa\u65f6\u95f4',
      readOnly: '\u53ea\u8bfb',
      remove: '\u5220\u9664',
      failed: '\u64cd\u4f5c\u5931\u8d25',
      llmTitle: 'LLM \u8bbe\u7f6e',
      llmHint: '\u5148\u6dfb\u52a0 OpenAI \u6216 Anthropic \u534f\u8bae\u7684\u670d\u52a1\u5546\uff0c\u518d\u4e3a\u524d\u53f0\u4ea4\u4e92\u3001\u540e\u53f0\u6267\u884c\u3001\u7535\u8111\u64cd\u4f5c\u548c\u5b9a\u4f4d\u6a21\u578b\u5206\u522b\u6307\u5b9a\u670d\u52a1\u5546\u4e0e\u6a21\u578b\u3002\u524d\u53f0\u4ea4\u4e92\u9ed8\u8ba4\u4f7f\u7528 MaClaw \u5b98\u65b9\u670d\u52a1\u7ec4\uff0c\u6309\u5173\u8054\u7528\u6237\u5728\u670d\u52a1\u5151\u6362\u4e2d\u7684\u989d\u5ea6\u6263\u8d39\uff1b\u5176\u4f59\u89d2\u8272\u9ed8\u8ba4\u8ddf\u968f\u524d\u53f0\uff0c\u6a21\u578b\u7559\u7a7a\u5219\u4f7f\u7528\u670d\u52a1\u5546\u9ed8\u8ba4\u6a21\u578b\u3002\u4fdd\u5b58\u540e\uff0c\u4e0b\u4e00\u6b21 Bot \u64cd\u4f5c\u4f1a\u5199\u5165 MaClawSrv\u3002',
      llmDefaultNote: '\u524d\u53f0\u4ea4\u4e92\u672a\u9009\u5176\u5b83\u670d\u52a1\u5546\u65f6\uff0c\u4f7f\u7528 MaClaw \u5b98\u65b9\u670d\u52a1\u7ec4\uff08\u6309\u5173\u8054\u7528\u6237\u5728\u670d\u52a1\u5151\u6362\u4e2d\u7684\u989d\u5ea6\u6263\u8d39\uff09\u3002',
      llmOfficialName: 'MaClaw \u5b98\u65b9\u670d\u52a1\u7ec4',
      llmAdd: '\u6dfb\u52a0\u670d\u52a1\u5546',
      llmEdit: '\u7f16\u8f91',
      llmEditProvider: '\u7f16\u8f91\u670d\u52a1\u5546',
      llmCancel: '\u53d6\u6d88',
      llmDialogSave: '\u4fdd\u5b58',
      llmName: '\u540d\u79f0',
      llmProtocol: '\u534f\u8bae',
      llmURL: '\u63a5\u53e3\u5730\u5740',
      llmKey: 'API Key',
      llmModel: '\u6a21\u578b',
      llmSave: '\u4fdd\u5b58 LLM \u8bbe\u7f6e',
      llmRemove: '\u79fb\u9664',
      llmSaved: '\u5df2\u4fdd\u5b58 LLM \u8bbe\u7f6e',
      llmSavedStale: '\u5df2\u4fdd\u5b58\u3002\u4e4b\u540e\u7684\u4fee\u6539\u5c1a\u672a\u5199\u5165\uff0c\u8bf7\u518d\u4fdd\u5b58\u4e00\u6b21\u3002',
      llmNeedName: '\u8bf7\u586b\u5199\u670d\u52a1\u5546\u540d\u79f0',
      llmDuplicateName: '\u670d\u52a1\u5546\u540d\u79f0\u4e0d\u80fd\u91cd\u590d',
      llmLimit: '\u6700\u591a\u6dfb\u52a0 8 \u4e2a\u670d\u52a1\u5546',
      llmBadProtocol: '\u534f\u8bae\u53ea\u80fd\u662f OpenAI \u6216 Anthropic',
      llmBadURL: '\u63a5\u53e3\u5730\u5740\u9700\u8981\u4ee5 http:// \u6216 https:// \u5f00\u5934',
      llmNeedModel: '\u8bf7\u586b\u5199\u6a21\u578b',
      llmNeedKey: '\u8bf7\u586b\u5199\u65b0\u670d\u52a1\u5546\u7684 API Key',
      llmBadKey: 'API Key \u65e0\u6548',
      llmReservedName: '\u8fd9\u4e2a\u540d\u79f0\u5df2\u4fdd\u7559',
      llmKeyKeep: '\u7559\u7a7a\u5219\u4e0d\u4fee\u6539\u5df2\u4fdd\u5b58\u7684\u5bc6\u94a5',
      llmBadCurrent: '\u8bf7\u9009\u62e9 MaClaw \u5b98\u65b9\u670d\u52a1\u7ec4\u6216\u4e00\u4e2a\u5df2\u6dfb\u52a0\u7684\u670d\u52a1\u5546',
      llmKeySet: '\u5bc6\u94a5\u5df2\u4fdd\u5b58',
      llmRolesTitle: '\u6a21\u578b\u914d\u7f6e',
      llmRolesHint: '\u4e3a\u56db\u79cd\u64cd\u4f5c\u5206\u522b\u6307\u5b9a\u670d\u52a1\u5546\u4e0e\u6a21\u578b\u3002\u540e\u53f0\u6267\u884c\u3001\u7535\u8111\u64cd\u4f5c\u548c\u5b9a\u4f4d\u6a21\u578b\u9ed8\u8ba4\u8ddf\u968f\u524d\u53f0\u4ea4\u4e92\uff1b\u6a21\u578b\u7559\u7a7a\u4f7f\u7528\u670d\u52a1\u5546\u9ed8\u8ba4\u6a21\u578b\u3002',
      llmRoleForeground: '\u524d\u53f0\u4ea4\u4e92',
      llmRoleBackground: '\u540e\u53f0\u6267\u884c',
      llmRoleComputerUse: '\u7535\u8111\u64cd\u4f5c',
      llmRoleGrounder: '\u5b9a\u4f4d\u6a21\u578b',
      llmRoleInherit: '\u8ddf\u968f\u524d\u53f0\u4ea4\u4e92',
      llmRoleUnused: '\u4e0d\u4f7f\u7528',
      llmRoleModelOfficial: '\u5b98\u65b9\u9ed8\u8ba4\uff08auto\uff09',
      llmRoleModelDefault: '\u9ed8\u8ba4 {m}\uff0c\u53ef\u8986\u76d6',
      llmRoleModelEmpty: '\u670d\u52a1\u5546\u9ed8\u8ba4\u6a21\u578b',
      llmRoleModelBad: '\u89d2\u8272\u6a21\u578b\u540d\u65e0\u6548',
      llmFetchModels: '\u83b7\u53d6\u6a21\u578b\u5217\u8868',
      llmFetching: '\u6b63\u5728\u83b7\u53d6...',
      llmModelsLoaded: '\u5df2\u83b7\u53d6 {n} \u4e2a\u6a21\u578b\uff0c\u70b9\u51fb\u8f93\u5165\u6846\u53ef\u76f4\u63a5\u9009\u62e9',
      llmModelsEmpty: '\u670d\u52a1\u5546\u672a\u8fd4\u56de\u6a21\u578b\u5217\u8868',
      llmFetchNeedURL: '\u8bf7\u5148\u586b\u5199\u63a5\u53e3\u5730\u5740\u548c API Key',
      llmProviderGone: '\u670d\u52a1\u5546\u5df2\u4e0d\u5b58\u5728\uff0c\u5237\u65b0\u9875\u9762\u540e\u91cd\u8bd5'
    },
    en: {
      title: 'Bot management',
      subtitle: 'Connect MaClawSrv and set the Docker service and LLM bots use. Users create bots in the MaClaw app.',
      nav: 'Bot management',
      navDesc: 'MaClawSrv connection, Docker service, and LLM',
      connection: 'MaClawSrv connection',
      url: 'MaClawSrv URL',
      token: 'Access token',
      tokenHint: 'Leave blank to keep the saved token',
      tokenSet: 'Token saved',
      tokenMissing: 'No token saved',
      adminSecret: 'Admin secret',
      adminHint: 'Creates one MaClawSrv user per Hub user. Leave blank to keep the saved secret.',
      adminSet: 'Admin secret saved',
      adminMissing: 'No admin secret saved',
      save: 'Save connection',
      test: 'Test connection',
      saved: 'Saved',
      connected: 'Connected, {count} instances',
      testing: 'Testing...',
      needConfig: 'Save the MaClawSrv URL and access token first',
      saveBeforeTest: 'Save the URL and token before testing',
      srvRejected: 'MaClawSrv rejected the request',
      srvTokenRejected: 'MaClawSrv rejected the access token (HTTP 401). Tokens expire: save a fresh one, or keep the admin secret saved and test again to renew automatically',
      srvTokenForbidden: 'The access token lacks permission (HTTP 403)',
      srvNotFound: 'Instance or endpoint not found (HTTP 404); check that the URL forwards /api',
      bots: 'Bots created by users',
      botsHint: 'Created in the MaClaw app. This page only lists them.',
      empty: 'No bots yet. Users create them in the MaClaw app.',
      needUrl: 'Enter the MaClawSrv URL',
      badUrl: 'The MaClawSrv URL must start with http:// or https://',
      owner: 'User',
      access: 'Who can use bots',
      accessHint: 'Off by default. The desktop Bot entry appears only for users switched on here.',
      global: 'Everyone',
      department: 'Department',
      user: 'User',
      orgTitle: 'Organization',
      orgHint: 'Open a department to enable it, including child departments, or enable one person.',
      orgSearch: 'Search departments or users',
      orgLoading: 'Loading organization...',
      orgEmpty: 'No organization yet.',
      orgFailed: 'Could not load the organization',
      orgUsersFailed: 'The user directory failed to load, so people cannot be enabled yet.',
      orgReload: 'Reload',
      orgAddDepartment: 'Enable department',
      orgAddUser: 'Enable user',
      orgAdded: 'Enabled',
      orgNoAccount: 'No account',
      orgMembersLoading: 'Loading people...',
      orgMembersFailed: 'Could not load people. Collapse and open this department to retry.',
      orgNoMembers: 'No people',
      orgSearchEmpty: 'No matching department or user',
      orgSearchLimited: 'Only part of the organization was searched. Try a more specific word.',
      orgSearching: 'Searching...',
      orgMatchUsers: 'Matching accounts',
      orgMatchLimited: 'Showing the first 50 accounts. Type a few more letters.',
      grantEmpty: 'Nobody is enabled.',
      instance: 'Instance',
      configured: 'Configured',
      unconfigured: 'Not configured',
      online: 'Connected',
      connectionHint: 'Enter the MaClawSrv URL and access token, then save and test.',
      globalHint: 'Everyone in this tenant can use bots',
      enableList: 'Enabled',
      everyoneNote: 'Everyone already has access. Turn the switch off to enable specific departments or users.',
      countUnit: 'entries',
      botColumn: 'Bot',
      created: 'Created',
      readOnly: 'Read-only',
      remove: 'Delete',
      failed: 'Request failed',
      llmTitle: 'LLM settings',
      llmHint: 'Add an OpenAI or Anthropic provider, then set the provider and model for each of the four roles: chat, background jobs, computer use, and the grounding model. The foreground role defaults to the MaClaw official service group, which deducts the associated user redeemed allowance; the other roles follow the foreground by default, and an empty model keeps the provider default. The next bot action writes the saved settings.',
      llmOfficialName: 'MaClaw official service group',
      llmAdd: 'Add provider',
      llmEdit: 'Edit',
      llmEditProvider: 'Edit provider',
      llmCancel: 'Cancel',
      llmDialogSave: 'Save',
      llmName: 'Name',
      llmProtocol: 'Protocol',
      llmURL: 'API URL',
      llmKey: 'API key',
      llmModel: 'Model',
      llmSave: 'Save LLM settings',
      llmRemove: 'Remove',
      llmSaved: 'LLM settings saved',
      llmSavedStale: 'Saved. Later edits are not stored yet. Save again.',
      llmNeedName: 'Enter a provider name',
      llmDuplicateName: 'Provider names must be unique',
      llmLimit: 'At most 8 providers can be added',
      llmBadProtocol: 'Protocol must be OpenAI or Anthropic',
      llmBadURL: 'The API URL must start with http:// or https://',
      llmNeedModel: 'Enter a model',
      llmNeedKey: 'Enter an API key for the new provider',
      llmBadKey: 'The API key is invalid',
      llmReservedName: 'This name is reserved',
      llmKeyKeep: 'Leave blank to keep the saved key',
      llmBadCurrent: 'Choose the MaClaw official service group or a provider you added',
      llmKeySet: 'Key saved',
      llmRolesTitle: 'Model settings',
      llmRolesHint: 'Set the provider and model for each of the four roles. Background jobs, computer use, and the grounding model follow the foreground by default; an empty model keeps the provider default.',
      llmRoleForeground: 'Chat',
      llmRoleBackground: 'Background jobs',
      llmRoleComputerUse: 'Computer use',
      llmRoleGrounder: 'Grounding model',
      llmRoleInherit: 'Follow foreground',
      llmRoleUnused: 'Do not use',
      llmRoleModelOfficial: 'Official default (auto)',
      llmRoleModelDefault: 'Default {m}; override here',
      llmRoleModelEmpty: 'Provider default model',
      llmRoleModelBad: 'The role model is invalid',
      llmDefaultNote: 'When no other provider is pinned for the foreground role, bots use the MaClaw official service group and deduct the associated user official allowance.',
      llmFetchModels: 'Fetch models',
      llmFetching: 'Fetching...',
      llmModelsLoaded: '{n} models fetched; click the input to pick one',
      llmModelsEmpty: 'The provider returned no models',
      llmFetchNeedURL: 'Enter the API URL and API key first',
      llmProviderGone: 'The provider no longer exists; reload the page and retry'
    }
  };

  function text() {
    var lang = global.hubAdminLang ? global.hubAdminLang() : (global.currentLang === 'zh' ? 'zh' : 'en');
    if (lang === 'mn') {
      var mn = global.HUB_ADMIN_DICT_MN && global.HUB_ADMIN_DICT_MN.BOT;
      return Object.assign({}, copy.en, mn || {});
    }
    return copy[lang] || copy.en;
  }

  function byID(id) {
    return document.getElementById(id);
  }

  function api(path, opts) {
    if (typeof global.api === 'function') return global.api(path, opts);
    return Promise.reject(new Error('api unavailable'));
  }

  function showToast(msg, kind) {
    if (typeof global.showToast === 'function') global.showToast(msg, kind);
  }

  function escapeHtml(value) {
    return String(value == null ? '' : value)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  // srvReason keeps what MaClawSrv actually said, so the localized line
  // explains the fix without hiding the upstream detail.
  function srvReason(raw) {
    var idx = raw.lastIndexOf('): ');
    return idx < 0 ? '' : ' [' + raw.slice(idx + 3) + ']';
  }

  function messageOf(err) {
    var raw = err && err.message ? err.message : '';
    if (raw.indexOf('save the MaClawSrv URL') !== -1) return text().needConfig;
    if (raw.indexOf('base_url') !== -1) return text().badUrl;
    var t = text();
    if (raw.indexOf('HTTP 401') !== -1) return t.srvTokenRejected + srvReason(raw);
    if (raw.indexOf('HTTP 403') !== -1) return t.srvTokenForbidden + srvReason(raw);
    if (raw.indexOf('HTTP 404') !== -1 || raw.indexOf('check the MaClawSrv URL') !== -1) return t.srvNotFound + srvReason(raw);
    if (raw.indexOf('MaClawSrv rejected the instance request') !== -1) return t.srvRejected + srvReason(raw);
    if (raw.indexOf('llm provider url') !== -1) return t.llmBadURL;
    if (raw.indexOf('llm provider protocol') !== -1) return t.llmBadProtocol;
    if (raw.indexOf('llm provider name is reserved') !== -1) return t.llmReservedName;
    if (raw.indexOf('llm provider name is duplicated') !== -1) return t.llmDuplicateName;
    if (raw.indexOf('llm provider name is required') !== -1) return t.llmNeedName;
    if (raw.indexOf('llm provider limit') !== -1) return t.llmLimit;
    if (raw.indexOf('llm provider key is invalid') !== -1) return t.llmBadKey;
    if (raw.indexOf('llm provider key') !== -1) return t.llmNeedKey;
    if (raw.indexOf('llm provider model') !== -1) return t.llmNeedModel;
    if (raw.indexOf('llm models provider') !== -1) return t.llmProviderGone;
    if (raw.indexOf('llm current') !== -1) return t.llmBadCurrent;
    return raw || t.failed;
  }

  function applyBotI18n() {
    var t = text();
    var nav = byID('navBots');
    var desc = byID('navBotsDesc');
    if (nav) nav.textContent = t.nav;
    if (desc) desc.textContent = t.navDesc;
    var panel = byID('tab-bots');
    if (!panel || panel.dataset.ready !== '1') return;
    setLabel('botConnectionTitle', t.connection);
    setLabel('botConnectionHint', t.connectionHint);
    setLabel('botUrlLabel', t.url);
    setLabel('botTokenLabel', t.token);
    setLabel('botTokenHint', t.tokenHint);
    setLabel('botAdminLabel', t.adminSecret);
    setLabel('botAdminHint', t.adminHint);
    setLabel('botSave', t.save);
    setLabel('botTest', t.test);
    setLabel('botLLMTitle', t.llmTitle);
    setLabel('botLLMHint', t.llmHint);
    setLabel('botLLMAdd', t.llmAdd);
    setLabel('botLLMSave', t.llmSave);
    llmDialogLabels();
    if (panel._botView) renderLLM(panel._botView);
    setLabel('botAccessTitle', t.access);
    setLabel('botAccessHint', t.accessHint);
    setLabel('botGlobalLabel', t.global);
    setLabel('botGlobalHint', t.globalHint);
    setLabel('botGlobalOn', t.everyoneNote);
    setLabel('botGrantsTitle', t.enableList);
    setLabel('botOrgTitle', t.orgTitle);
    setLabel('botOrgHint', t.orgHint);
    var orgFilter = byID('botOrgFilter');
    if (orgFilter) {
      orgFilter.placeholder = t.orgSearch;
      if (orgFilter.setAttribute) orgFilter.setAttribute('aria-label', t.orgSearch);
    }
    setLabel('botListTitle', t.bots);
    setLabel('botListHint', t.botsHint);
    setLabel('botListBadge', t.readOnly);
    if (panel._botView) render(panel._botView);
  }

  function setLabel(id, value) {
    var el = byID(id);
    if (el) el.textContent = value;
  }

  function mount() {
    var panel = byID('tab-bots');
    if (!panel || panel.dataset.ready === '1') return;
    panel.dataset.ready = '1';
    panel.innerHTML = ''
      + '<style>'
      + '#tab-bots [hidden]{display:none !important}'
      + '#tab-bots .bot-page{display:grid;gap:14px}'
      /* One card per task, each with a title, a hint, and a live status badge. */
      + '#tab-bots .bot-section{display:grid;gap:12px;align-content:start;padding:15px 17px;border:1px solid #e4e9f2;border-radius:10px;background:#fff;box-shadow:0 6px 18px rgba(23,34,54,.035)}'
      + '#tab-bots .bot-section > .item-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin:0;padding:0 0 11px;border-bottom:1px solid #eef2f7}'
      + '#tab-bots .bot-section .item-title{margin:0;font-size:14px;font-weight:650;color:#172033}'
      + '#tab-bots .bot-section .item-meta{margin:3px 0 0;font-size:11px;line-height:1.5;color:#64748b;max-width:720px}'
      + '#tab-bots .bot-section > .item-head > .badge{flex:0 0 auto;margin-top:1px}'
      + '#tab-bots .badge:empty,#tab-bots .bot-status:empty{display:none}'
      /* Form layout: full-width row, then a two-up row. */
      + '#tab-bots .bot-form{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}'
      + '#tab-bots .bot-field{min-width:0}'
      + '#tab-bots .bot-field-wide{grid-column:1 / -1}'
      + '#tab-bots .bot-field-head{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:5px}'
      + '#tab-bots .bot-field-head label{margin-bottom:0}'
      + '#tab-bots label{display:block;margin:0 0 5px;font-size:11px;font-weight:650;letter-spacing:.04em;text-transform:uppercase;color:#64748b}'
      + '#tab-bots input:not([type=checkbox]):not([type=radio]),#tab-bots select{display:block;width:100%;height:36px;margin:0}'
      + '#tab-bots input[type=radio]{width:16px;height:16px;min-height:16px;margin:2px 0 0;flex:0 0 auto;accent-color:#2563eb}'
      + '#tab-bots .bot-llm-list{display:grid;gap:10px}'
      + '#tab-bots .bot-llm-roles{display:grid;gap:8px;margin-top:4px;padding:10px 12px;border:1px solid #e4e9f2;border-radius:10px;background:#fff}'
      + '#tab-bots .bot-llm-roles-title{margin:0;font-size:13px;font-weight:650;color:#172033}'
      + '#tab-bots .bot-llm-roles-hint{margin:0;font-size:12px;line-height:1.45;color:#64748b}'
      + '#tab-bots .bot-llm-role{display:grid;grid-template-columns:minmax(7em,0.4fr) minmax(0,0.9fr) minmax(0,1.1fr);gap:8px 12px;align-items:center}'
      + '#tab-bots .bot-llm-role label{margin:0;text-transform:none;letter-spacing:0;font-size:13px;font-weight:600;color:#172033}'
      + '#tab-bots .bot-llm-role-model{display:flex;gap:6px;align-items:center;min-width:0}'
      + '#tab-bots .bot-llm-role-model input{min-width:0}'
      + '#tab-bots .bot-llm-role-model input:disabled{background:#f4f6fa;color:#94a3b8;cursor:default}'
      + '#tab-bots .bot-llm-default-note{margin:0;padding:9px 12px;border:1px dashed #d7e3f8;border-radius:10px;background:#f8fbff;color:#475569;font-size:11px;line-height:1.5}'
      + '#tab-bots .bot-llm-row{display:flex;align-items:center;gap:10px;flex-wrap:wrap;padding:10px 12px;border:1px solid #e4e9f2;border-radius:12px;background:#fff;box-shadow:0 1px 2px rgba(23,34,54,.04);transition:border-color .15s ease}'
      + '#tab-bots .bot-llm-row:hover{border-color:#c9d5e8}'
      + '#tab-bots .bot-llm-row-name{min-width:0;flex:0 1 auto;font-size:13px;font-weight:650;color:#172033;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}'
      + '#tab-bots .bot-llm-row-url{min-width:0;flex:1 1 200px;font-size:11px;color:#64748b;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}'
      + '#tab-bots .bot-llm-row-actions{display:flex;gap:6px;flex:0 0 auto}'
      + '#tab-bots .bot-llm-row-actions .btn-secondary,#tab-bots .bot-llm-row-actions .btn-danger{height:28px;min-height:28px;padding:0 10px;font-size:12px;box-shadow:none}'
      + '#tab-bots .bot-llm-tags{display:flex;gap:6px;flex:0 0 auto;margin-left:auto}'
      + '#tab-bots .bot-llm-tag{padding:2px 8px;border-radius:999px;background:#eef5ff;color:#1d4ed8;font-size:10px;font-weight:700;letter-spacing:.02em;white-space:nowrap}'
      + '#tab-bots .bot-llm-tag-ok{background:#e8f7ef;color:#15803d}'
      + '#tab-bots .bot-llm-fetch{height:26px;min-height:26px;padding:0 10px;font-size:11px;border-radius:8px;box-shadow:none;white-space:nowrap}'
      + '#tab-bots .bot-llm-fetch:disabled,#tab-bots .bot-llm-fetch:disabled:hover{opacity:.55;cursor:default;transform:none}'
      + '#tab-bots .bot-modal-overlay{position:fixed;inset:0;z-index:60;display:flex;align-items:flex-start;justify-content:center;padding:8vh 16px 16px;background:rgba(15,23,42,.42)}'
      + '#tab-bots .bot-modal-overlay[hidden]{display:none}'
      + '#tab-bots .bot-modal{display:grid;gap:12px;width:min(680px,100%);max-height:82vh;overflow:auto;padding:16px 18px;border-radius:14px;background:#fff;box-shadow:0 24px 60px rgba(15,23,42,.25)}'
      + '#tab-bots .bot-modal-head{display:flex;align-items:center;justify-content:space-between;gap:10px;padding-bottom:10px;border-bottom:1px solid #eef2f7}'
      + '#tab-bots .bot-modal-head h4{margin:0;font-size:14px;font-weight:700;color:#172033}'
      + '#tab-bots .bot-modal-close{width:30px;height:30px;min-height:30px;padding:0;border:0;border-radius:8px;background:transparent;color:#64748b;font-size:16px;cursor:pointer}'
      + '#tab-bots .bot-modal-close:hover{background:#eef2f7;color:#172033}'
      + '#tab-bots .bot-modal-body{display:grid;gap:10px}'
      + '#tab-bots .bot-modal-foot{display:flex;justify-content:flex-end;gap:8px;padding-top:10px;border-top:1px solid #eef2f7}'
      + '#tab-bots .bot-llm-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px 12px;align-items:end}'
      + '#tab-bots .bot-field-note{margin:6px 0 0;font-size:11px;line-height:1.5;color:#64748b}'
      + '#tab-bots .bot-status{margin:0;font-size:12px;line-height:1.45;color:#64748b}'
      + '#tab-bots .bot-foot{display:flex;align-items:center;gap:12px;flex-wrap:wrap}'
      + '#tab-bots .bot-actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap}'
      + '#tab-bots .bot-actions button{height:34px;min-height:34px;padding:0 12px;font-size:12px}'
      + '#tab-bots button:disabled,#tab-bots button:disabled:hover{opacity:.55;cursor:default;transform:none}'
      /* Everyone switch. */
      + '#tab-bots label.bot-toggle{display:flex;align-items:center;justify-content:space-between;gap:14px;margin:0;padding:11px 13px;border:1px solid #dbe7ff;border-radius:10px;background:#f8fbff;cursor:pointer;text-transform:none;letter-spacing:0;font-size:13px;font-weight:400;color:inherit}'
      + '#tab-bots .bot-toggle-copy{display:grid;gap:3px;min-width:0}'
      + '#tab-bots .bot-toggle-title{font-size:13px;font-weight:650;color:#172033}'
      + '#tab-bots .bot-toggle-desc{font-size:11px;line-height:1.5;color:#64748b}'
      + '#tab-bots input[type=checkbox]{width:38px;height:22px;min-height:22px;flex:0 0 auto;margin:0;padding:0;border:0;border-radius:999px;appearance:none;background:#d7dee9;position:relative;cursor:pointer;transition:background .16s ease}'
      + '#tab-bots input[type=checkbox]::after{content:"";position:absolute;left:2px;top:2px;width:18px;height:18px;border-radius:999px;background:#fff;box-shadow:0 1px 3px rgba(15,23,42,.2);transition:transform .16s ease}'
      + '#tab-bots input[type=checkbox]:checked{background:#2563eb}'
      + '#tab-bots input[type=checkbox]:checked::after{transform:translateX(16px)}'
      + '#tab-bots input[type=checkbox]:disabled{cursor:default;opacity:.55}'
      + '#tab-bots label.bot-toggle:has(input:disabled){cursor:default}'
      /* Access: pick on the left, review what is enabled on the right. */
      + '#tab-bots .bot-split{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:12px;align-items:start}'
      + '#tab-bots .bot-pane{display:grid;gap:8px;align-content:start;min-width:0}'
      + '#tab-bots .bot-pane-title{font-size:10px;font-weight:800;letter-spacing:.06em;text-transform:uppercase;color:#64748b}'
      + '#tab-bots .bot-pane-desc{margin:0;font-size:11px;line-height:1.5;color:#64748b}'
      + '#tab-bots .bot-hint{margin:0;padding:10px 12px;border:1px solid #e4e9f2;border-radius:10px;background:#fbfcfe;color:#64748b;font-size:11px;line-height:1.5}'
      + '#tab-bots .bot-empty{margin:0;padding:14px;border:1px dashed #dbe3ee;border-radius:10px;background:#fbfcfe;color:#64748b;font-size:11px;line-height:1.5;text-align:center}'
      + '#tab-bots .bot-grant-list{display:grid;gap:6px}'
      + '#tab-bots .bot-grant{display:flex;align-items:center;justify-content:space-between;gap:10px;padding:8px 8px 8px 11px;border:1px solid #e4e9f2;border-radius:9px;background:#fff}'
      + '#tab-bots .bot-grant-kind{flex:0 0 auto;padding:2px 7px;border-radius:999px;background:#eef5ff;color:#1d4ed8;font-size:10px;font-weight:800;letter-spacing:.03em}'
      + '#tab-bots .bot-grant-name{min-width:0;flex:1;font-size:12px;color:#243244;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}'
      /* Organization tree. */
      + '#tab-bots .bot-org-tree{max-height:340px;overflow:auto;border:1px solid #e4e9f2;border-radius:10px;padding:6px;background:#fbfcfe}'
      + '#tab-bots .bot-org-row{display:flex;align-items:center;gap:8px;min-height:32px;padding:3px 8px;border-radius:8px;cursor:pointer}'
      + '#tab-bots .bot-org-row:hover{background:#eef5ff}'
      + '#tab-bots .bot-org-toggle{width:22px;min-width:22px;height:22px;min-height:22px;padding:0;border:0;background:transparent;box-shadow:none;color:#64748b;font-size:10px}'
      + '#tab-bots .bot-org-toggle:hover{transform:none;background:#e2e8f0}'
      + '#tab-bots .bot-org-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:13px}'
      /* Member rows sit under a department: recede them so the hierarchy reads at a glance. */
      + '#tab-bots .bot-org-user .bot-org-name{color:#64748b;font-size:12px}'
      + '#tab-bots .bot-org-meta,#tab-bots .bot-org-status{color:#64748b;font-size:12px}'
      /* Status lines sit both alone and between rows, so keep the spacing symmetric and small. */
      + '#tab-bots .bot-org-status{margin:4px 2px}'
      + '#tab-bots .bot-org-add{height:28px;min-height:28px;padding:0 10px;font-size:12px;box-shadow:none}'
      /* Bot list table. */
      + '#tab-bots .bot-table{display:grid;gap:6px}'
      + '#tab-bots .bot-tr{display:grid;grid-template-columns:minmax(0,1.7fr) minmax(0,1.2fr) minmax(0,1.1fr) minmax(0,1fr);gap:10px;align-items:center;padding:9px 11px;border:1px solid #e4e9f2;border-radius:9px;background:#fff}'
      + '#tab-bots .bot-th{padding:0 11px;border:0;background:transparent;box-shadow:none;color:#64748b;font-size:10px;font-weight:800;letter-spacing:.06em;text-transform:uppercase}'
      + '#tab-bots .bot-td{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12px;color:#475569}'
      + '#tab-bots .bot-td strong{display:block;font-size:13px;font-weight:650;color:#172033;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}'
      + '#tab-bots .bot-td small{display:block;color:#64748b;font-size:11px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}'
      /* Shared legacy hooks still used by the Docker service module. */
      + '#tab-bots .bot-row{display:flex;justify-content:space-between;align-items:center;gap:12px;padding:10px 0;border-top:1px solid #eef2f7}'
      + '#tab-bots .bot-row:first-child{border-top:0}'
      + '#tab-bots .bot-row > div{min-width:0}'
      + '#tab-bots .bot-row small{display:block;color:#64748b;font-size:11px}'
      + '#tab-bots .bot-fields{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px;margin:0 0 12px}'
      + '#tab-bots .bot-fields input{width:100%;margin:0}'
      /* Docker service sections mount here as cards that match the rest of the page. */
      + '#tab-bots #botDesktopHost{display:grid;gap:14px}'
      + '#tab-bots #botDesktopHost > section{display:grid;gap:12px;align-content:start;padding:15px 17px;border:1px solid #e4e9f2;border-radius:10px;background:#fff;box-shadow:0 6px 18px rgba(23,34,54,.035)}'
      + '#tab-bots #botDesktopHost > section > h3{margin:0;padding:0;font-size:14px;font-weight:650;color:#172033;line-height:1.3}'
      + '#tab-bots #botDesktopHost > section > h3::before{content:"";display:inline-block;width:6px;height:6px;margin-right:7px;border-radius:999px;background:#2563eb;vertical-align:middle}'
      + '#tab-bots #botDesktopHost > section > h3 + p{padding-bottom:11px;border-bottom:1px solid #eef2f7}'
      + '#tab-bots #desktopHint,#tab-bots #desktopAssignHint,#tab-bots #desktopRunHint{margin:0;font-size:11px;line-height:1.5;color:#64748b}'
      + '@media (max-width:860px){#tab-bots .bot-form,#tab-bots .bot-split,#tab-bots .bot-llm-grid,#tab-bots .bot-llm-role{grid-template-columns:1fr}#tab-bots .bot-fields{grid-template-columns:1fr 1fr}}'
      + '@media (max-width:760px){#tab-bots .bot-tr{grid-template-columns:1fr;gap:6px}#tab-bots .bot-th{display:none}#tab-bots .bot-td{white-space:normal;overflow:visible}#tab-bots .bot-td-main{padding-bottom:6px;border-bottom:1px solid #eef2f7}#tab-bots .bot-td-labelled{display:grid;grid-template-columns:92px minmax(0,1fr);gap:10px;align-items:baseline}#tab-bots .bot-td-labelled::before{content:attr(data-label);font-size:10px;font-weight:800;letter-spacing:.06em;text-transform:uppercase;color:#64748b;overflow:visible;text-overflow:clip}#tab-bots .bot-fields{grid-template-columns:1fr}}'
      + '</style>'
      + '<div class="bot-page">'
      + '<section class="item bot-section">'
      + '<div class="item-head"><div><h3 class="item-title" id="botConnectionTitle"></h3>'
      + '<p class="item-meta" id="botConnectionHint"></p></div><span class="badge" id="botConnBadge"></span></div>'
      + '<div class="bot-form">'
      + '<div class="bot-field bot-field-wide"><label id="botUrlLabel" for="botBaseUrl"></label>'
      + '<input id="botBaseUrl" autocomplete="off" spellcheck="false"></div>'
      + '<div class="bot-field"><div class="bot-field-head"><label id="botTokenLabel" for="botAccessToken"></label>'
      + '<span class="badge" id="botTokenState"></span></div>'
      + '<input id="botAccessToken" type="password" autocomplete="new-password">'
      + '<p class="bot-field-note" id="botTokenHint"></p></div>'
      + '<div class="bot-field"><div class="bot-field-head"><label id="botAdminLabel" for="botAdminSecret"></label>'
      + '<span class="badge" id="botAdminState"></span></div>'
      + '<input id="botAdminSecret" type="password" autocomplete="new-password">'
      + '<p class="bot-field-note" id="botAdminHint"></p></div>'
      + '</div>'
      + '<div class="bot-foot"><div class="bot-actions">'
      + '<button type="button" class="btn-primary" id="botSave"></button>'
      + '<button type="button" class="btn-secondary" id="botTest"></button></div>'
      + '<p class="bot-status" id="botConnStatus"></p></div>'
      + '</section>'
      + '<section class="item bot-section">'
      + '<div class="item-head"><div><h3 class="item-title" id="botLLMTitle"></h3>'
      + '<p class="item-meta" id="botLLMHint"></p></div><span class="badge" id="botLLMBadge"></span></div>'
      + '<div id="botLLMList" class="bot-llm-list"></div>'
      + '<div id="botLLMRoles" class="bot-llm-roles"></div>'
      + '<div id="botLLMPModal" class="bot-modal-overlay" hidden>'
      + '<div class="bot-modal" role="dialog" aria-modal="true" aria-labelledby="botLLMPModalTitle">'
      + '<div class="bot-modal-head"><h4 id="botLLMPModalTitle"></h4>'
      + '<button type="button" class="bot-modal-close" id="botLLMPClose" aria-label="close">\u00d7</button></div>'
      + '<div class="bot-modal-body"><div class="bot-llm-grid">'
      + '<div class="bot-field"><label id="botLLMPNameLabel" for="botLLMPName"></label><input id="botLLMPName" autocomplete="off"></div>'
      + '<div class="bot-field"><label id="botLLMPProtocolLabel" for="botLLMPProtocol"></label><select id="botLLMPProtocol">'
      + '<option value="openai">OpenAI</option><option value="anthropic">Anthropic</option></select></div>'
      + '<div class="bot-field bot-field-wide"><label id="botLLMPUrlLabel" for="botLLMPUrl"></label><input id="botLLMPUrl" autocomplete="off" spellcheck="false"></div>'
      + '<div class="bot-field"><div class="bot-field-head"><label id="botLLMPModelLabel" for="botLLMPModel"></label>'
      + '<button type="button" class="bot-llm-fetch" id="botLLMPFetch"></button></div>'
      + '<input id="botLLMPModel" list="botLLMPModelList" autocomplete="off" spellcheck="false">'
      + '<datalist id="botLLMPModelList"></datalist>'
      + '<p class="bot-field-note" id="botLLMPModelsStatus"></p></div>'
      + '<div class="bot-field"><label id="botLLMPKeyLabel" for="botLLMPKey"></label><input id="botLLMPKey" type="password" autocomplete="new-password">'
      + '<p class="bot-field-note" id="botLLMPKeyHint"></p></div>'
      + '</div></div>'
      + '<div class="bot-modal-foot">'
      + '<button type="button" class="btn-secondary" id="botLLMPCancel"></button>'
      + '<button type="button" class="btn-primary" id="botLLMPSave"></button></div>'
      + '</div></div>'
      + '<div class="bot-foot"><div class="bot-actions">'
      + '<button type="button" class="btn-secondary" id="botLLMAdd"></button>'
      + '<button type="button" class="btn-primary" id="botLLMSave"></button></div>'
      + '<p class="bot-status" id="botLLMStatus"></p></div>'
      + '</section>'
      + '<section class="item bot-section">'
      + '<div class="item-head"><div><h3 class="item-title" id="botAccessTitle"></h3>'
      + '<p class="item-meta" id="botAccessHint"></p></div><span class="badge" id="botAccessBadge"></span></div>'
      + '<label class="bot-toggle" for="botGlobal"><span class="bot-toggle-copy">'
      + '<span class="bot-toggle-title" id="botGlobalLabel"></span>'
      + '<span class="bot-toggle-desc" id="botGlobalHint"></span></span>'
      + '<input id="botGlobal" type="checkbox"></label>'
      + '<p class="bot-hint" id="botGlobalOn" hidden></p>'
      + '<div class="bot-split" id="botAccessBody">'
      + '<div class="bot-pane"><span class="bot-pane-title" id="botOrgTitle"></span>'
      + '<p class="bot-pane-desc" id="botOrgHint"></p>'
      + '<input id="botOrgFilter" type="search" autocomplete="off">'
      + '<div id="botOrgTree" class="bot-org-tree" role="tree"></div></div>'
      + '<div class="bot-pane"><span class="bot-pane-title" id="botGrantsTitle"></span>'
      + '<div id="botGrants" class="bot-grant-list"></div></div>'
      + '</div></section>'
      + '<div id="botDesktopHost"></div>'
      + '<section class="item bot-section">'
      + '<div class="item-head"><div><h3 class="item-title" id="botListTitle"></h3>'
      + '<p class="item-meta" id="botListHint"></p></div><span class="badge" id="botListBadge"></span></div>'
      + '<div id="botList" class="bot-table"></div></section>'
      + '</div>';
    byID('botSave').addEventListener('click', saveConnection);
    byID('botTest').addEventListener('click', testConnection);
    byID('botLLMAdd').addEventListener('click', addLLMProvider);
    byID('botLLMSave').addEventListener('click', saveLLM);
    byID('botLLMList').addEventListener('click', onLLMListClick);
    byID('botLLMRoles').addEventListener('change', onLLMRoleChange);
    byID('botLLMRoles').addEventListener('input', onLLMRoleModelEdit);
    byID('botLLMRoles').addEventListener('click', onLLMRoleClick);
    byID('botLLMPClose').addEventListener('click', closeLLMProviderDialog);
    byID('botLLMPCancel').addEventListener('click', closeLLMProviderDialog);
    byID('botLLMPSave').addEventListener('click', saveLLMProviderDialog);
    byID('botLLMPFetch').addEventListener('click', fetchLLMDialogModels);
    byID('botLLMPModal').addEventListener('input', onLLMPDialogEdit);
    byID('botLLMPModal').addEventListener('change', onLLMPDialogEdit);
    byID('botLLMPModal').addEventListener('click', onBotLLMPModalOverlayClick);
    document.addEventListener('keydown', onBotLLMPModalKeydown);
    byID('botGlobal').addEventListener('change', toggleGlobal);
    byID('botOrgFilter').addEventListener('compositionstart', function () { orgState().composing = true; });
    byID('botOrgFilter').addEventListener('compositionend', function () {
      orgState().composing = false;
      onOrgFilter();
    });
    byID('botOrgFilter').addEventListener('input', function (event) {
      if (orgState().composing || (event && event.isComposing)) return;
      onOrgFilter();
    });
    byID('botOrgTree').addEventListener('click', onOrgClick);
    if (typeof global.mountBotDesktop === 'function') global.mountBotDesktop();
    applyBotI18n();
  }

  function load() {
    mount();
    if (typeof global.loadBotDesktop === 'function') global.loadBotDesktop();
    var directory = orgState().loaded ? Promise.resolve() : loadDirectory('');
    var settings = api('/api/admin/bots/settings').then(function (view) {
      render(view || {});
    }).catch(function (err) {
      showToast(messageOf(err), 'error');
    });
    return Promise.all([settings, directory]);
  }

  function openBotTab() {
    var refresh = orgState().loaded;
    var pending = load();
    if (refresh) return Promise.all([pending, loadDirectory('refresh')]);
    return pending;
  }

  function showServerUrl(url, next) {
    if (!url) return;
    var incoming = next || '';
    var dirty = url._serverValue != null ? url.value !== url._serverValue : url.value !== '' && url.value !== incoming;
    if (dirty) {
      if (url._serverValue == null) url._serverValue = incoming;
      return;
    }
    url.value = incoming;
    url._serverValue = url.value;
  }

  function render(view) {
    var panel = byID('tab-bots');
    if (panel) panel._botView = view;
    showServerUrl(byID('botBaseUrl'), view.base_url || '');
    setCredState(byID('botTokenState'), !!view.token_set, text().tokenSet, text().tokenMissing);
    setCredState(byID('botAdminState'), !!view.admin_secret_set, text().adminSet, text().adminMissing);
    var access = grantSummary(view);
    renderConnBadge(view);
    renderLLM(view);
    renderAccess(access);
    renderBots(view);
    renderGrants(view, access);
    renderOrgTree();
  }

  function setCredState(el, set, okLabel, missingLabel) {
    if (!el) return;
    el.className = 'badge ' + (set ? 'ok' : 'warn');
    el.textContent = set ? okLabel : missingLabel;
  }

  /* One pass over the grants: the switch and the list both read it. */
  function grantSummary(view) {
    var grants = view && Array.isArray(view.grants) ? view.grants : [];
    var specific = grants.filter(function (item) { return item.scope !== 'global'; });
    return { grants: grants, specific: specific, everyone: specific.length !== grants.length };
  }

  function connState(view) {
    var v = view || {};
    var base = String(v.base_url || '').trim();
    if (!base || !v.token_set) return { level: 'warn', label: text().unconfigured };
    var panel = byID('tab-bots');
    if (panel && panel._botConnOk) return { level: 'ok', label: text().online };
    return { level: 'info', label: text().configured };
  }

  /* Each card head carries its own state, so there is one status surface per fact. */
  function renderConnBadge(view) {
    var conn = connState(view);
    setBadge('botConnBadge', conn.label, conn.level);
  }

  function setBadge(id, value, level) {
    var el = byID(id);
    if (!el) return;
    el.textContent = value;
    el.className = 'badge ' + (level || 'info');
  }

  /* Everyone switch wins: hide the picker rather than offering redundant choices. */
  function renderAccess(access) {
    var body = byID('botAccessBody');
    var note = byID('botGlobalOn');
    if (body) body.hidden = access.everyone;
    if (note) note.hidden = !access.everyone;
    var t = text();
    setBadge('botAccessBadge', access.everyone ? t.global : access.specific.length + ' ' + t.countUnit,
      access.everyone ? 'ok' : 'info');
  }

  function renderGrants(view, access) {
    var summary = access || grantSummary(view);
    var list = byID('botGrants');
    var globalBox = byID('botGlobal');
    /* While a grant request is in flight the switch shows the admin's intent,
       not the server's last known state, so leave it alone until the response lands. */
    if (globalBox && !grantLocked()) globalBox.checked = summary.everyone;
    if (!list) return;
    if (!summary.specific.length) {
      list.innerHTML = summary.grants.length ? '' : '<p class="bot-empty">' + escapeHtml(text().grantEmpty) + '</p>';
      return;
    }
    list.innerHTML = summary.specific.map(function (item) {
      return '<div class="bot-grant">'
        + '<span class="bot-grant-kind">' + escapeHtml(item.scope === 'user' ? text().user : text().department) + '</span>'
        + '<span class="bot-grant-name">' + escapeHtml(grantTargetLabel(item)) + '</span>'
        + '<button type="button" class="btn-danger" data-grant-delete="' + escapeHtml(item.id) + '"' + (grantLocked() ? ' disabled' : '') + '>'
        + escapeHtml(text().remove) + '</button></div>';
    }).join('');
    list.querySelectorAll('[data-grant-delete]').forEach(function (button) {
      button.addEventListener('click', function () { deleteGrant(button.getAttribute('data-grant-delete')); });
    });
  }

  function grantTargetLabel(item) {
    if (!item) return '';
    if (item.scope === 'department') return deptPath(item.target_id) || item.target_id || '';
    if (item.scope === 'user') return userLabel(item.target_id) || item.target_id || '';
    return item.target_id || '';
  }

  function setGrantBusy(busy) {
    var panel = byID('tab-bots');
    if (panel) panel._botGrantBusy = !!busy;
    var globalBox = byID('botGlobal');
    if (globalBox) globalBox.disabled = !!busy;
    renderOrgTree();
    var panelView = panel && panel._botView;
    if (panelView) renderGrants(panelView);
  }

  function grantLocked() {
    var panel = byID('tab-bots');
    return !!(panel && panel._botGrantBusy);
  }

  function toggleGlobal() {
    var box = byID('botGlobal');
    if (!box || grantLocked()) return;
    var panel = byID('tab-bots');
    var view = panel && panel._botView;
    var grants = view && Array.isArray(view.grants) ? view.grants : [];
    var current = null;
    grants.forEach(function (item) { if (item.scope === 'global') current = item; });
    if (box.checked && !current) {
      setGrantBusy(true);
      return api('/api/admin/bots/grants', { method: 'POST', body: JSON.stringify({ scope: 'global' }) }).then(load).catch(function (err) {
        box.checked = false;
        showToast(messageOf(err), 'error');
      }).then(function () { setGrantBusy(false); });
    }
    if (!box.checked && current) {
      setGrantBusy(true);
      return api('/api/admin/bots/grants/' + encodeURIComponent(current.id), { method: 'DELETE' }).then(load).catch(function (err) {
        box.checked = true;
        showToast(messageOf(err), 'error');
      }).then(function () { setGrantBusy(false); });
    }
  }

  function orgState() {
    var panel = byID('tab-bots');
    if (!panel._org) {
      panel._org = {
        seq: 0,
        tree: null,
        usersByEmail: {},
        usersById: {},
        members: {},
        memberLoads: {},
        loadingMembers: {},
        expanded: {},
        filter: '',
        error: '',
        loading: false,
        loaded: false,
        searching: false,
        searchLimited: false,
        searchToken: 0,
        searchTimer: 0,
        renderQueued: false,
        promise: null,
        memberFailed: {},
        collapsed: {}
      };
    }
    return panel._org;
  }

  function normalizeOrgTree(nodes, ancestry, depth) {
    var path = ancestry || {};
    var current = depth || 0;
    if (current > 24) return [];
    return (Array.isArray(nodes) ? nodes : []).reduce(function (out, node) {
      if (!node || typeof node !== 'object') return out;
      var id = String(node.id || '').trim();
      if (!id || path[id]) return out;
      var next = {};
      Object.keys(path).forEach(function (key) { next[key] = true; });
      next[id] = true;
      out.push({
        id: id,
        name: String(node.name || id).trim() || id,
        children: normalizeOrgTree(node.children, next, current + 1)
      });
      return out;
    }, []);
  }

  function indexOrgUsers(users) {
    var org = orgState();
      org.usersByEmail = {};
      org.usersById = {};
      org.hitQuery = '';
      org.hitCache = null;
    (users || []).forEach(function (user) {
      if (!user) return;
      var id = String(user.id || '').trim();
      if (!id) return;
      org.usersById[id] = user;
      [user.email].concat(user.emails || [], user.phone ? [user.phone] : [], user.phones || []).forEach(function (email) {
        var key = String(email || '').trim().toLowerCase();
        if (key && !org.usersByEmail[key]) org.usersByEmail[key] = user;
      });
    });
  }

  function userLabel(userId) {
    var user = orgState().usersById[String(userId || '')];
    if (!user) return '';
    return String(user.email || user.phone || user.id || '').trim();
  }

  function findDept(nodes, id, prefix) {
    var want = String(id || '');
    for (var i = 0; i < (nodes || []).length; i += 1) {
      var name = nodes[i].name || nodes[i].id;
      var path = prefix ? prefix + ' / ' + name : name;
      if (nodes[i].id === want) return path;
      var child = findDept(nodes[i].children, want, path);
      if (child) return child;
    }
    return '';
  }

  function deptPath(id) {
    return findDept(orgState().tree || [], id, '');
  }

  function flattenDepts(nodes, prefix, out) {
    (nodes || []).forEach(function (node) {
      var name = node.name || node.id;
      var path = prefix ? prefix + ' / ' + name : name;
      out.push({ id: node.id, label: path });
      flattenDepts(node.children, path, out);
    });
  }

  global.botOrgChoices = function () {
    var org = orgState();
    var departments = [];
    flattenDepts(org.tree || [], '', departments);
    var users = Object.keys(org.usersById || {}).map(function (id) {
      var user = org.usersById[id];
      return { id: id, label: String((user && (user.email || user.phone)) || id) };
    });
    users.sort(function (a, b) { return a.label.localeCompare(b.label); });
    return { ready: !!org.loaded, departments: departments, users: users };
  };

  function grantFor(scope, targetId) {
    var panel = byID('tab-bots');
    var grants = panel && panel._botView && Array.isArray(panel._botView.grants) ? panel._botView.grants : [];
    var want = String(targetId || '');
    for (var i = 0; i < grants.length; i += 1) {
      if (grants[i].scope === scope && String(grants[i].target_id || '') === want) return grants[i];
    }
    return null;
  }

  function loadDirectory(mode) {
    var org = orgState();
    var refresh = mode === 'refresh';
    var reload = mode === 'reload';
    if (!refresh && !reload && org.promise) return org.promise;
    org.seq += 1;
    var seq = org.seq;
    org.loading = true;
    if (reload || refresh) {
      org.members = {};
      org.memberLoads = {};
      org.loadingMembers = {};
      org.memberFailed = {};
    }
    if (reload) {
      org.loaded = false;
      org.tree = null;
      org.error = '';
    }
    if (!org.loaded) renderOrgTree();
    var usersFailed = false;
    org.promise = Promise.all([
      api('/api/admin/security/groups'),
      api('/api/admin/users').then(function (data) { return data || { users: [] }; }, function () {
        usersFailed = true;
        return { users: [] };
      })
    ]).then(function (results) {
      if (org.seq !== seq) return;
      var treeData = results[0] || {};
      var userData = results[1] || {};
      var tree = treeData.tree;
      org.tree = normalizeOrgTree(Array.isArray(tree) ? tree : (tree ? [tree] : []));
      indexOrgUsers(userData.users || []);
      org.loaded = true;
      org.error = '';
      if (usersFailed) showToast(text().orgUsersFailed, 'error');
      var open = Object.keys(org.expanded).filter(function (id) {
        return org.expanded[id] && findDept(org.tree, id, '');
      });
      if (!open.length && org.tree[0]) {
        org.expanded[org.tree[0].id] = true;
        open = [org.tree[0].id];
      }
      return Promise.all(open.slice(0, 30).map(function (id) { return loadMembers(id); }));
    }).catch(function (err) {
      if (org.seq !== seq) return;
      if (org.tree && org.tree.length) showToast(messageOf(err), 'error');
      else {
        org.tree = [];
        org.loaded = true;
        org.error = messageOf(err);
      }
    }).then(function () {
      if (org.seq !== seq) return;
      org.loading = false;
      renderOrgTree();
      notifyDirectory();
      var panel = byID('tab-bots');
      if (panel && panel._botView) {
        renderGrants(panel._botView);
        renderBots(panel._botView);
      }
      var query = String(org.filter || '').trim().toLowerCase();
      if (query) return searchMembers(query);
    });
    return org.promise;
  }

  function resolveMember(email) {
    var raw = String(email || '').trim();
    var key = raw.toLowerCase();
    if (!key) return null;
    var user = orgState().usersByEmail[key];
    return {
      email: raw,
      userId: user ? String(user.id || '').trim() : '',
      label: user ? (userLabel(user.id) || raw) : raw
    };
  }

  function loadMembers(groupId) {
    var org = orgState();
    var id = String(groupId || '').trim();
    if (!id) return Promise.resolve();
    if (org.members[id]) return Promise.resolve(org.members[id]);
    if (org.memberLoads[id]) return org.memberLoads[id];
    var seq = org.seq;
    org.loadingMembers[id] = true;
    delete org.memberFailed[id];
    var pending = api('/api/admin/security/groups/' + encodeURIComponent(id) + '/members').then(function (data) {
      if (org.seq !== seq) return;
      var seen = {};
      org.members[id] = (data && data.members || []).map(resolveMember).filter(function (member) {
        if (!member) return false;
        var key = member.email.toLowerCase();
        if (seen[key]) return false;
        seen[key] = true;
        return true;
      });
    }).catch(function () {
      if (org.seq !== seq) return;
      org.memberFailed[id] = true;
    }).then(function () {
      if (org.seq !== seq) return;
      delete org.loadingMembers[id];
      delete org.memberLoads[id];
      scheduleOrgRender();
    });
    org.memberLoads[id] = pending;
    return pending;
  }

  function namePrefix(node, query) {
    return String(node && (node.name || node.id) || '').toLowerCase().indexOf(query) === 0;
  }

  function shouldScanMembers(query) {
    if (!query) return false;
    if (query.length >= 2) return true;
    return /[\u3400-\u9fff]/.test(query);
  }

  function scanPlan(query) {
    if (!shouldScanMembers(query)) return [];
    var prefixHit = false;
    function mark(nodes) {
      (nodes || []).forEach(function (node) {
        if (!node || prefixHit) return;
        if (namePrefix(node, query)) prefixHit = true;
        else mark(node.children);
      });
    }
    mark(orgState().tree);
    var first = [];
    var rest = [];
    function walk(nodes, underPrefix, underName, depth) {
      (nodes || []).forEach(function (node) {
        if (!node || !node.id) return;
        var label = String(node.name || node.id || '').toLowerCase();
        var named = label.indexOf(query) === 0;
        var contains = label.indexOf(query) >= 0;
        if (prefixHit && (underPrefix || named)) (contains || underName ? first : rest).push(node.id);
        walk(node.children, underPrefix || (named && depth > 0), underName || contains, depth + 1);
      });
    }
    walk(orgState().tree, false, false, 0);
    return first.concat(rest);
  }

  function idsToScan(query) {
    var org = orgState();
    var windowIds = org.scanWindow;
    return scanPlan(query).filter(function (id) {
      if (org.members[id]) return false;
      return !windowIds || !!windowIds[id];
    });
  }

  function rememberScan(query) {
    var org = orgState();
    var plan = scanPlan(query);
    var windowIds = {};
    plan.slice(0, 80).forEach(function (id) { windowIds[id] = true; });
    org.scanWindow = windowIds;
    org.searchLimited = plan.length > 80;
  }

  function onOrgFilter() {
    var org = orgState();
    var input = byID('botOrgFilter');
    var next = input && input.value || '';
    var query = String(next).trim().toLowerCase();
    var previous = String(org.filter || '').trim().toLowerCase();
    org.filter = next;
    org.searchToken += 1;
    if (!query) {
      if (previous && org.expandedBeforeSearch) org.expanded = org.expandedBeforeSearch;
      org.expandedBeforeSearch = null;
      org.collapsed = {};
      org.scanWindow = null;
      org.searchLimited = false;
    } else if (!previous) {
      org.expandedBeforeSearch = Object.assign({}, org.expanded);
      org.collapsed = {};
    }
    if (query) rememberScan(query);
    org.searching = !!(query && idsToScan(query).length);
    renderOrgTree();
    if (!query) {
      if (org.searchTimer) clearTimeout(org.searchTimer);
      org.searchTimer = 0;
      return;
    }
    scheduleMemberSearch();
  }

  function scheduleMemberSearch() {
    var org = orgState();
    if (org.searchTimer) clearTimeout(org.searchTimer);
    org.searchTimer = setTimeout(function () {
      org.searchTimer = 0;
      searchMembers(String(org.filter || '').trim().toLowerCase());
    }, 200);
  }

  function searchMembers(query) {
    var org = orgState();
    org.searchToken += 1;
    var token = org.searchToken;
    if (!query) {
      org.searching = false;
      renderOrgTree();
      return Promise.resolve();
    }
    if (!org.scanWindow) rememberScan(query);
    org.searchLimited = scanPlan(query).length > 80;
    var queue = idsToScan(query);
    if (!queue.length) {
      org.searching = false;
      renderOrgTree();
      return Promise.resolve();
    }
    org.searching = true;
    var cursor = 0;
    var limit = 80;
    function pump() {
      if (token !== org.searchToken) return Promise.resolve();
      if (cursor >= queue.length) return Promise.resolve();
      if (cursor >= limit) {
        org.searchLimited = true;
        return Promise.resolve();
      }
      var id = queue[cursor];
      cursor += 1;
      return loadMembers(id).then(pump);
    }
    var jobs = [];
    for (var n = 0; n < 4; n += 1) jobs.push(pump());
    return Promise.all(jobs).then(function () {
      if (token !== org.searchToken) return;
      org.searching = false;
      renderOrgTree();
    });
  }

  function scheduleOrgRender() {
    var org = orgState();
    if (org.renderQueued) return;
    org.renderQueued = true;
    Promise.resolve().then(function () {
      org.renderQueued = false;
      renderOrgTree();
    });
  }

  function accountMatches(user, member, query) {
    if (!query) return true;
    var emails = [];
    var phones = [];
    var id = '';
    if (user) {
      if (user.email) emails.push(user.email);
      if (user.emails) emails = emails.concat(user.emails);
      if (user.phone) phones.push(user.phone);
      if (user.phones) phones = phones.concat(user.phones);
      id = String(user.id || '');
    }
    if (member) {
      if (member.email) emails.push(member.email);
      if (!id && member.userId) id = String(member.userId);
    }
    if (id && id.toLowerCase() === query) return true;
    var phoneQuery = query.replace(/[\s+\-().]/g, '');
    var queryDigits = /^\d{4,}$/.test(phoneQuery) ? phoneQuery : '';
    var i;
    for (i = 0; i < phones.length; i += 1) {
      var phone = String(phones[i] || '').trim().toLowerCase();
      if (!phone || !queryDigits) continue;
      if (phone.replace(/\D/g, '').indexOf(queryDigits) >= 0) return true;
    }
    for (i = 0; i < emails.length; i += 1) {
      var email = String(emails[i] || '').trim().toLowerCase();
      if (!email) continue;
      if (email.indexOf(query) === 0) return true;
      var at = email.indexOf('@');
      var local = at >= 0 ? email.slice(0, at) : email;
      if (local && local.indexOf(query) === 0) return true;
    }
    return false;
  }

  function memberMatches(member, query) {
    if (!query) return true;
    var user = member && member.userId ? orgState().usersById[member.userId] : null;
    return accountMatches(user, member, query);
  }

  function matchedContact(user, query) {
    var emails = [user && user.email].concat(user && user.emails || []);
    var phones = [user && user.phone].concat(user && user.phones || []);
    var i;
    for (i = 0; i < emails.length; i += 1) {
      var email = String(emails[i] || '').trim();
      if (email && accountMatches({ email: email }, null, query)) return email;
    }
    for (i = 0; i < phones.length; i += 1) {
      var phone = String(phones[i] || '').trim();
      if (phone && accountMatches({ phone: phone }, null, query)) return phone;
    }
    return '';
  }

  function directoryHits(query) {
    var org = orgState();
    if (!shouldScanMembers(query)) return [];
    if (org.hitQuery === query && org.hitCache) return org.hitCache;
    var hits = Object.keys(org.usersById).filter(function (id) {
      return accountMatches(org.usersById[id], null, query);
    }).map(function (id) {
      var user = org.usersById[id];
      var primary = userLabel(id) || id;
      var matched = matchedContact(user, query);
      var label = matched && matched.toLowerCase() !== primary.toLowerCase() ? primary + ' / ' + matched : primary;
      return {
        email: String((user && (user.email || user.phone)) || ''),
        userId: id,
        label: label
      };
    }).sort(function (a, b) { return a.label.localeCompare(b.label); });
    org.hitQuery = query;
    org.hitCache = hits;
    return hits;
  }

  function renderDirectoryHits(query, treeHtml) {
    var hits = directoryHits(query).filter(function (member) {
      return treeHtml.indexOf('data-org-add-user="' + member.userId + '"') === -1;
    });
    if (!hits.length) return '';
    var more = hits.length > 50;
    return '<div class="bot-org-status">' + escapeHtml(text().orgMatchUsers) + '</div>'
      + hits.slice(0, 50).map(function (member) { return renderUserRow(member, 0); }).join('')
      + (more ? '<div class="bot-org-status">' + escapeHtml(text().orgMatchLimited) + '</div>' : '');
  }

  function nodeMatches(node, query) {
    if (!query) return true;
    if (String(node.name || node.id || '').toLowerCase().indexOf(query) >= 0) return true;
    var members = orgState().members[node.id] || [];
    if (members.some(function (member) { return memberMatches(member, query); })) return true;
    return (node.children || []).some(function (child) { return nodeMatches(child, query); });
  }

  function orgAddButton(attr, id, label, granted, title) {
    var caption = granted ? text().orgAdded : label;
    var aria = title ? caption + ' ' + title : caption;
    return '<button type="button" class="btn-secondary bot-org-add" ' + attr + '="' + escapeHtml(id) + '" aria-label="' + escapeHtml(aria) + '"'
      + ((granted || grantLocked()) ? ' disabled' : '') + '>' + escapeHtml(caption) + '</button>';
  }

  function renderUserRow(member, pad) {
    var granted = !!(member.userId && grantFor('user', member.userId));
    var action = member.userId
      ? orgAddButton('data-org-add-user', member.userId, text().orgAddUser, granted, member.label)
      : '<span class="bot-org-meta">' + escapeHtml(text().orgNoAccount) + '</span>';
    return '<div class="bot-org-row bot-org-user" role="treeitem" style="padding-left:' + String(30 + pad) + 'px">'
      + '<span class="bot-org-name">' + escapeHtml(member.label) + '</span>' + action + '</div>';
  }

  function revealForFilter() {
    var org = orgState();
    var query = String(org.filter || '').trim().toLowerCase();
    if (!query) return;
    var scan = shouldScanMembers(query);
    var queued = org.scanWindow || {};
    function walk(nodes, underName, depth) {
      (nodes || []).forEach(function (node) {
        if (!node || !node.id) return;
        var named = namePrefix(node, query);
        var show = named || underName || (scan && nodeMatches(node, query));
        var known = !!org.members[node.id] || !!queued[node.id] || !!org.loadingMembers[node.id];
        if (!org.collapsed[node.id] && show && (known || named)) org.expanded[node.id] = true;
        walk(node.children, underName || (named && depth > 0), depth + 1);
      });
    }
    walk(org.tree, false, 0);
  }

  function renderOrgNodes(nodes, depth, query, ancestorNamed) {
    return (nodes || []).map(function (node) {
      return renderOrgNode(node, depth, query, ancestorNamed);
    }).join('');
  }

  function renderOrgNode(node, depth, query, ancestorNamed) {
    if (!node) return '';
    var selfNamed = !query || String(node.name || node.id || '').toLowerCase().indexOf(query) >= 0;
    var named = !!ancestorNamed || selfNamed;
    if (query && !named && !nodeMatches(node, query)) return '';
    var org = orgState();
    var expanded = !!org.expanded[node.id];
    var pad = depth * 16;
    var nameMatches = named;
    var html = '<div class="bot-org-row" role="treeitem" data-org-toggle="' + escapeHtml(node.id) + '" aria-expanded="' + (expanded ? 'true' : 'false') + '" style="padding-left:' + String(8 + pad) + 'px">'
      + '<button type="button" class="bot-org-toggle" data-org-toggle="' + escapeHtml(node.id) + '" aria-label="' + escapeHtml(node.name || node.id) + '">'
      + (expanded ? '\u25bc' : '\u25b6') + '</button>'
      + '<span class="bot-org-name">' + escapeHtml(node.name || node.id) + '</span>'
      + orgAddButton('data-org-add-dept', node.id, text().orgAddDepartment, !!grantFor('department', node.id), node.name || node.id)
      + '</div>';
    if (!expanded) return html;
    if (org.loadingMembers[node.id] && !org.members[node.id]) {
      html += '<div class="bot-org-status" style="padding-left:' + String(36 + pad) + 'px">' + escapeHtml(text().orgMembersLoading) + '</div>';
    } else if (org.memberFailed[node.id] && !org.members[node.id]) {
      html += '<div class="bot-org-status" style="padding-left:' + String(36 + pad) + 'px">' + escapeHtml(text().orgMembersFailed) + '</div>';
    } else if (org.searching && !org.members[node.id]) {
      html += '<div class="bot-org-status" style="padding-left:' + String(36 + pad) + 'px">' + escapeHtml(text().orgMembersLoading) + '</div>';
    } else {
      var members = (org.members[node.id] || []).filter(function (member) {
        return nameMatches || memberMatches(member, query);
      });
      html += members.map(function (member) { return renderUserRow(member, pad + 16); }).join('');
      if (org.members[node.id] && !members.length && !(node.children && node.children.length) && !query) {
        html += '<div class="bot-org-status" style="padding-left:' + String(36 + pad) + 'px">' + escapeHtml(text().orgNoMembers) + '</div>';
      }
    }
    html += renderOrgNodes(node.children || [], depth + 1, nameMatches ? '' : query, nameMatches);
    return html;
  }

  function notifyDirectory() {
    if (typeof global.refreshBotDesktopTargets === 'function') global.refreshBotDesktopTargets();
  }

  function drawOrg(host, html) {
    if (!host || host._drawn === html) return;
    var top = host.scrollTop || 0;
    host.innerHTML = html;
    host._drawn = html;
    if (top) host.scrollTop = top;
  }

  function renderOrgTree() {
    var host = byID('botOrgTree');
    if (!host) return;
    /* Everyone is enabled: the picker is hidden, so do not build its DOM at all. */
    var body = byID('botAccessBody');
    if (body && body.hidden) return;
    var org = orgState();
    var t = text();
    if (!org.loaded && org.loading) {
      drawOrg(host, '<p class="bot-org-status">' + escapeHtml(t.orgLoading) + '</p>');
      return;
    }
    if (org.error && !(org.tree && org.tree.length)) {
      drawOrg(host, '<p class="bot-org-status">' + escapeHtml(org.error || t.orgFailed) + '</p>'
        + '<button type="button" class="btn-secondary bot-org-add" id="botOrgReload">' + escapeHtml(t.orgReload) + '</button>');
      return;
    }
    if (!org.tree || !org.tree.length) {
      drawOrg(host, '<p class="bot-org-status">' + escapeHtml(t.orgEmpty) + '</p>'
        + '<button type="button" class="btn-secondary bot-org-add" id="botOrgReload">' + escapeHtml(t.orgReload) + '</button>');
      return;
    }
    revealForFilter();
    var query = String(org.filter || '').trim().toLowerCase();
    var html = renderOrgNodes(org.tree, 0, query, false);
    html = renderDirectoryHits(query, html) + html;
    if (!html) {
      var empty = org.searching ? t.orgSearching : (org.searchLimited ? t.orgSearchLimited : t.orgSearchEmpty);
      html = '<p class="bot-org-status">' + escapeHtml(empty) + '</p>';
    } else if (org.searchLimited) {
      html += '<p class="bot-org-status">' + escapeHtml(t.orgSearchLimited) + '</p>';
    }
    drawOrg(host, html);
  }

  function onOrgClick(event) {
    var node = event.target;
    var tree = byID('botOrgTree');
    var action = null;
    var kind = '';
    while (node && node !== tree) {
      if (node.getAttribute) {
        if (node.getAttribute('data-org-add-dept')) { action = node; kind = 'dept'; break; }
        if (node.getAttribute('data-org-add-user')) { action = node; kind = 'user'; break; }
        if (node.getAttribute('data-org-toggle')) { action = node; kind = 'toggle'; break; }
      }
      if (node.id === 'botOrgReload') { action = node; kind = 'reload'; break; }
      node = node.parentElement;
    }
    if (!action) return;
    if (kind === 'toggle') toggleOrg(action.getAttribute('data-org-toggle'));
    else if (kind === 'dept') addOrgGrant('department', action.getAttribute('data-org-add-dept'));
    else if (kind === 'user') addOrgGrant('user', action.getAttribute('data-org-add-user'));
    else if (kind === 'reload') loadDirectory('reload');
  }

  function toggleOrg(id) {
    var org = orgState();
    var query = String(org.filter || '').trim();
    if (org.expanded[id]) {
      delete org.expanded[id];
      if (query) org.collapsed[id] = true;
    } else {
      org.expanded[id] = true;
      if (query) delete org.collapsed[id];
      loadMembers(id);
    }
    renderOrgTree();
  }

  function addOrgGrant(scope, targetId) {
    if (grantLocked()) return;
    var target = String(targetId || '').trim();
    if (!target || grantFor(scope, target)) return;
    setGrantBusy(true);
    return api('/api/admin/bots/grants', { method: 'POST', body: JSON.stringify({ scope: scope, target_id: target }) }).then(function () {
      return load();
    }).catch(function (err) {
      showToast(messageOf(err), 'error');
    }).then(function () { setGrantBusy(false); });
  }

  function deleteGrant(id) {
    if (grantLocked()) return;
    setGrantBusy(true);
    return api('/api/admin/bots/grants/' + encodeURIComponent(id), { method: 'DELETE' }).then(load).catch(function (err) {
      showToast(messageOf(err), 'error');
    }).then(function () { setGrantBusy(false); });
  }

  function renderBots(view) {
    var list = byID('botList');
    if (!list) return;
    var bots = view && Array.isArray(view.bots) ? view.bots : [];
    if (!bots.length) {
      list.innerHTML = '<p class="bot-empty">' + escapeHtml(text().empty) + '</p>';
      return;
    }
    var t = text();
    list.innerHTML = '<div class="bot-tr bot-th">'
      + '<span>' + escapeHtml(t.botColumn) + '</span>'
      + '<span>' + escapeHtml(t.owner) + '</span>'
      + '<span>' + escapeHtml(t.instance) + '</span>'
      + '<span>' + escapeHtml(t.created) + '</span></div>'
      + bots.map(function (bot) {
        return '<div class="bot-tr">'
          + '<div class="bot-td bot-td-main"><strong>' + escapeHtml(bot.name) + '</strong>'
          + (bot.description ? '<small>' + escapeHtml(bot.description) + '</small>' : '') + '</div>'
          + labelled(t.owner, userLabel(bot.owner_user_id) || bot.owner_user_id || '')
          + labelled(t.instance, bot.instance_id || '')
          + labelled(t.created, shortTime(bot.created_at))
          + '</div>';
      }).join('');
  }

  /* Narrow screens drop the header row, so each value carries its own column label. */
  function labelled(label, value) {
    return '<div class="bot-td bot-td-labelled" data-label="' + escapeHtml(label) + '">' + escapeHtml(value) + '</div>';
  }

  function shortTime(value) {
    var raw = String(value || '').trim();
    if (!raw) return '';
    var match = raw.match(/^(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2})/);
    return match ? match[1] + ' ' + match[2] : raw;
  }

  var botOfficialGroup = 'maclaw_official_group';

  function isBotDefaultGroup(id) {
    var value = String(id || '').trim().toLowerCase();
    return !value || value === 'system-free' || value === botOfficialGroup;
  }

  function llmDraft() {
    var panel = byID('tab-bots');
    if (!panel._llmDraft) panel._llmDraft = { current: botOfficialGroup, providers: [], roles: defaultLLMRoles() };
    return panel._llmDraft;
  }

  function defaultLLMRoles() {
    return {
      foreground: { mode: 'pin', provider: botOfficialGroup, model: '' },
      background: { mode: 'inherit' },
      computer_use: { mode: 'inherit' },
      grounder: { mode: 'unused' }
    };
  }

  function knownLLMProviderID(draft, id) {
    if (isBotDefaultGroup(id)) return botOfficialGroup;
    return (draft.providers || []).some(function (item) { return item.id === id; }) ? id : '';
  }

  // The foreground role is the default route: the select shows the stored pin
  // when one exists, otherwise the settings' current provider (legacy rows
  // that predate the role table).
  function llmForegroundProvider(draft) {
    var role = draft && draft.roles && draft.roles.foreground;
    if (role && role.mode === 'pin' && knownLLMProviderID(draft, role.provider)) {
      return isBotDefaultGroup(role.provider) ? botOfficialGroup : role.provider;
    }
    var current = String(draft && draft.current || '');
    return isBotDefaultGroup(current) ? botOfficialGroup : current;
  }

  function normalizeDraftRole(raw, draft, grounder) {
    var mode = String((raw && raw.mode) || '').trim().toLowerCase();
    var provider = knownLLMProviderID(draft, raw && raw.provider);
    var model = String((raw && raw.model) || '').trim();
    if (mode === 'pin' && provider) {
      // The official route schedules its own model; a stored model on it is
      // stale and is dropped, the same rule the server applies.
      if (isBotDefaultGroup(provider)) return { mode: 'pin', provider: botOfficialGroup, model: '' };
      return { mode: 'pin', provider: provider, model: model };
    }
    // A pin whose provider is gone falls through to the inherit/unused
    // default below, the same coercion the server applies.
    if (mode === 'inherit') return { mode: 'inherit' };
    if (mode === 'unused' && grounder) return { mode: 'unused' };
    return { mode: grounder ? 'unused' : 'inherit' };
  }

  function normalizeDraftRoles(raw, draft) {
    raw = raw || {};
    return {
      foreground: normalizeDraftRole(raw.foreground, draft, false),
      background: normalizeDraftRole(raw.background, draft, false),
      computer_use: normalizeDraftRole(raw.computer_use, draft, false),
      grounder: normalizeDraftRole(raw.grounder, draft, true)
    };
  }

  // The select value for one role row. Foreground has no "follow" option: a
  // legacy row stored as inherit displays the settings' current provider.
  function roleSelectValue(draft, key, grounder) {
    var role = draft.roles && draft.roles[key];
    if (role && role.mode === 'unused' && grounder) return '__unused__';
    if (key === 'foreground') return llmForegroundProvider(draft);
    if (role && role.mode === 'pin') {
      return isBotDefaultGroup(role.provider) ? botOfficialGroup : String(role.provider || '');
    }
    return '__inherit__';
  }

  function roleFromSelect(value, key, grounder) {
    var id = String(value || '');
    if (id === '__unused__' && grounder) return { mode: 'unused' };
    // Empty, __inherit__, or an unknown id resolves to the safe default for
    // the row: the foreground pins the official group, every other row (the
    // grounder included) follows the foreground.
    if (!id || id === '__inherit__' || (!isBotDefaultGroup(id) && !knownLLMProviderID(llmDraft(), id))) {
      if (key === 'foreground') return { mode: 'pin', provider: botOfficialGroup, model: '' };
      return { mode: 'inherit' };
    }
    return { mode: 'pin', provider: isBotDefaultGroup(id) ? botOfficialGroup : id, model: '' };
  }

  function coerceDraftRoles(draft) {
    draft.roles = normalizeDraftRoles(draft.roles, draft);
  }

  function draftFromLLM(view) {
    var llm = view && view.llm || {};
    var providers = Array.isArray(llm.providers) ? llm.providers : [];
    var current = String(llm.current || '');
    var draft = {
      current: isBotDefaultGroup(current) ? botOfficialGroup : current,
      providers: providers.map(function (item) {
        return {
          id: String(item.id || ''),
          name: String(item.name || ''),
          protocol: String(item.protocol || '').toLowerCase() === 'anthropic' ? 'anthropic' : 'openai',
          url: String(item.url || ''),
          model: String(item.model || ''),
          key: '',
          key_set: !!item.key_set
        };
      })
    };
    draft.roles = normalizeDraftRoles(llm.roles, draft);
    return draft;
  }

  function newLLMProviderID() {
    var alphabet = 'abcdef0123456789';
    var out = 'lp_';
    for (var i = 0; i < 16; i += 1) out += alphabet.charAt(Math.floor(Math.random() * alphabet.length));
    return out;
  }

  function syncLLMBadge() {
    var badge = byID('botLLMBadge');
    if (!badge) return;
    var draft = llmDraft();
    var foreground = llmForegroundProvider(draft);
    var name = text().llmOfficialName;
    if (!isBotDefaultGroup(foreground)) {
      var match = (draft.providers || []).filter(function (item) { return item.id === foreground; })[0];
      name = match ? (String(match.name || '').trim() || text().llmName) : foreground;
    }
    badge.className = 'badge ' + (isBotDefaultGroup(foreground) ? 'info' : 'ok');
    badge.textContent = name;
  }

  function renderLLM(view) {
    var panel = byID('tab-bots');
    if (!panel || !byID('botLLMList')) return;
    // The live inputs already hold a newer edit. Rebuilding the list would
    // drop focus and replace those fields, even though the draft kept the values.
    if (panel._llmDirty) return;
    panel._llmDraft = draftFromLLM(view);
    paintLLM();
  }

  function paintLLM() {
    var list = byID('botLLMList');
    if (!list) return;
    var t = text();
    var draft = llmDraft();
    draft.current = llmForegroundProvider(draft);
    var rows = (draft.providers || []).map(function (item) {
      var id = escapeHtml(item.id);
      var protocol = item.protocol === 'anthropic' ? 'anthropic' : 'openai';
      return '<div class="bot-llm-row">'
        + '<strong class="bot-llm-row-name" id="botLLMUse_' + id + '">' + escapeHtml(item.name || t.llmName) + '</strong>'
        + '<span class="bot-llm-row-url">' + escapeHtml(item.url) + '</span>'
        + '<span class="bot-llm-tags"><span class="bot-llm-tag">' + (protocol === 'anthropic' ? 'Anthropic' : 'OpenAI') + '</span>'
        + (item.key_set ? '<span class="bot-llm-tag bot-llm-tag-ok">' + escapeHtml(t.llmKeySet) + '</span>' : '') + '</span>'
        + '<span class="bot-llm-row-actions">'
        + '<button type="button" class="btn-secondary" id="botLLMEdit_' + id + '">' + escapeHtml(t.llmEdit) + '</button>'
        + '<button type="button" class="btn-danger" id="botLLMRemove_' + id + '">' + escapeHtml(t.llmRemove) + '</button>'
        + '</span></div>';
    }).join('');
    list.innerHTML = '<p class="bot-empty bot-llm-default-note">' + escapeHtml(t.llmDefaultNote) + '</p>' + rows;
    paintLLMRoles();
    syncLLMBadge();
  }

  function llmModelOptionsHTML(models) {
    return (Array.isArray(models) ? models : []).map(function (item) {
      var id = String(item && item.id || '').trim();
      if (!id) return '';
      return '<option value="' + escapeHtml(id) + '"></option>';
    }).join('');
  }

  function llmModelsStatusText(cache) {
    if (!cache) return '';
    if (cache.error) return cache.error;
    var count = Array.isArray(cache.models) ? cache.models.length : 0;
    if (!count) return '';
    return text().llmModelsLoaded.replace('{n}', String(count));
  }

  function llmModelCacheStore(id, cache) {
    var panel = byID('tab-bots');
    if (!panel) return;
    panel._llmModels = panel._llmModels || {};
    panel._llmModels[id] = cache;
  }

  function llmModelCacheOf(id) {
    var panel = byID('tab-bots');
    if (!panel || !panel._llmModels) return null;
    return panel._llmModels[id] || null;
  }

  // clearLLMModelsFor drops a provider's catalog once its coordinates changed
  // or the row went away. The seq bump also retires an in-flight fetch whose
  // answer would describe the old endpoint; role pickers referencing the
  // provider lose their catalog with it.
  function clearLLMModelsFor(id) {
    var panel = byID('tab-bots');
    if (!panel) return;
    if (panel._llmModels) delete panel._llmModels[id];
    if (panel._llmFetchSeq) panel._llmFetchSeq[id] = (panel._llmFetchSeq[id] || 0) + 1;
    if (panel._llmFetchBusy) panel._llmFetchBusy[id] = false;
    refreshRoleModelPickers(id);
  }

  // llmModelsMessage names provider-fetch failures before messageOf can
  // misroute them: a provider's "(HTTP 401)" also matches the MaClawSrv
  // token-rejection line, which would tell the administrator to fix the
  // wrong credential.
  function llmModelsMessage(err) {
    var raw = err && err.message ? err.message : '';
    if (raw.indexOf('llm provider key is invalid') !== -1) return text().llmBadKey;
    if (raw.indexOf('llm provider key') !== -1) return text().llmNeedKey;
    if (raw.indexOf('llm models provider') !== -1) return text().llmProviderGone;
    if (raw.indexOf('llm provider protocol') !== -1) return text().llmBadProtocol;
    if (raw.indexOf('llm provider url') !== -1) return text().llmBadURL;
    return messageOf(err);
  }

  // setLLMFetchBusy toggles every role-row fetch button that targets the
  // given provider; the dialog fetch button manages itself.
  function setLLMFetchBusy(id, busy) {
    var t = text();
    ['foreground', 'background', 'computer_use', 'grounder'].forEach(function (key) {
      var role = (llmDraft().roles || {})[key] || {};
      if (role.mode !== 'pin' || role.provider !== id) return;
      var button = byID('botLLMRoleFetch_' + key);
      if (!button) return;
      button.disabled = !!busy;
      button.textContent = busy ? t.llmFetching : t.llmFetchModels;
    });
  }

  // fetchLLMModelsFor asks the hub to list one saved provider's models for
  // the role rows. A stored provider without a typed key omits it so the hub
  // reuses the saved credential. Never paints the cache from a response
  // older than the latest click (the seq guard).
  function fetchLLMModelsFor(id) {
    var panel = byID('tab-bots');
    var item = llmProviderFromID(id);
    if (!panel || !item) return;
    if (panel._llmFetchBusy && panel._llmFetchBusy[id]) return;
    var url = String(item.url || '').trim();
    var key = String(item.key || '').trim();
    if (!validServiceUrl(url)) {
      showToast(text().llmBadURL, 'error');
      return;
    }
    if (!key && !item.key_set) {
      showToast(text().llmFetchNeedURL, 'error');
      return;
    }
    var body = {
      protocol: item.protocol === 'anthropic' ? 'anthropic' : 'openai',
      url: url
    };
    if (item.key_set) body.provider_id = id;
    if (key) body.key = key;
    panel._llmFetchBusy = panel._llmFetchBusy || {};
    panel._llmFetchSeq = panel._llmFetchSeq || {};
    var seq = (panel._llmFetchSeq[id] || 0) + 1;
    panel._llmFetchSeq[id] = seq;
    panel._llmFetchBusy[id] = true;
    setLLMFetchBusy(id, true);
    return api('/api/admin/bots/llm/models', { method: 'POST', body: JSON.stringify(body) }).then(function (result) {
      if (panel._llmFetchSeq[id] !== seq) return;
      var models = result && Array.isArray(result.models) ? result.models : [];
      var cache = models.length ? { models: models, error: '' } : { models: null, error: text().llmModelsEmpty };
      llmModelCacheStore(id, cache);
      refreshRoleModelPickers(id);
      if (!models.length) showToast(cache.error, 'info');
    }).catch(function (err) {
      if (panel._llmFetchSeq[id] !== seq) return;
      var cache = { models: null, error: llmModelsMessage(err) };
      llmModelCacheStore(id, cache);
      refreshRoleModelPickers(id);
      showToast(cache.error, 'error');
    }).then(function () {
      panel._llmFetchBusy[id] = false;
      setLLMFetchBusy(id, false);
    });
  }

  // llmRoleOptions lists the provider choices for one role row. Foreground
  // has no follow mode; the other rows offer "follow foreground" first and
  // the grounder keeps its "do not use" entry.
  function llmRoleOptions(draft, key, grounder, selected) {
    var t = text();
    var options = [];
    if (grounder) options.push({ value: '__unused__', label: t.llmRoleUnused });
    if (key !== 'foreground') options.push({ value: '__inherit__', label: t.llmRoleInherit });
    options.push({ value: botOfficialGroup, label: t.llmOfficialName });
    (draft.providers || []).forEach(function (item) {
      var name = String(item.name || '').trim() || t.llmName;
      options.push({ value: item.id, label: name });
    });
    return options.map(function (item) {
      return '<option value="' + escapeHtml(item.value) + '"' + (item.value === selected ? ' selected' : '') + '>' + escapeHtml(item.label) + '</option>';
    }).join('');
  }

  // roleModelState decides what the model field on a role row shows: hidden
  // while the row follows the foreground (or the grounder is unused), locked
  // on the official route, and editable with the provider's catalog behind
  // it for a real provider pin.
  function roleModelState(draft, key, grounder) {
    var role = (draft.roles || {})[key] || {};
    var mode = String(role.mode || '');
    if (key !== 'foreground' && (mode === 'inherit' || (grounder && mode === 'unused'))) {
      return { hidden: true };
    }
    if (mode !== 'pin') {
      return key === 'foreground' ? { official: true } : { hidden: true };
    }
    if (isBotDefaultGroup(role.provider)) {
      return { official: true };
    }
    var provider = llmProviderFromID(role.provider);
    var fallback = provider && String(provider.model || '').trim();
    return {
      value: String(role.model || ''),
      placeholder: fallback ? text().llmRoleModelDefault.replace('{m}', fallback) : text().llmRoleModelEmpty
    };
  }

  function roleModelFieldHTML(draft, key, grounder) {
    var state = roleModelState(draft, key, grounder);
    if (state.hidden) return '';
    var disabled = state.official ? ' disabled' : '';
    var value = state.official ? '' : escapeHtml(state.value || '');
    var placeholder = state.official ? text().llmRoleModelOfficial : (state.placeholder || text().llmRoleModelEmpty);
    var provider = llmProviderFromID((draft.roles[key] || {}).provider);
    var options = llmModelOptionsHTML(provider && (llmModelCacheOf(provider.id) || {}).models);
    var fetch = '';
    if (!state.official && provider) {
      fetch = '<button type="button" class="bot-llm-fetch" id="botLLMRoleFetch_' + key + '">' + escapeHtml(text().llmFetchModels) + '</button>';
    }
    return '<div class="bot-llm-role-model">'
      + '<input id="botLLMRoleModel_' + key + '" list="botLLMRoleModelList_' + key + '" value="' + value + '" placeholder="' + escapeHtml(placeholder) + '" autocomplete="off" spellcheck="false"' + disabled + '>'
      + '<datalist id="botLLMRoleModelList_' + key + '">' + options + '</datalist>'
      + fetch
      + '</div>';
  }

  function paintLLMRoles() {
    var root = byID('botLLMRoles');
    if (!root) return;
    var t = text();
    var draft = llmDraft();
    coerceDraftRoles(draft);
    var rows = [
      { key: 'foreground', label: t.llmRoleForeground, grounder: false },
      { key: 'background', label: t.llmRoleBackground, grounder: false },
      { key: 'computer_use', label: t.llmRoleComputerUse, grounder: false },
      { key: 'grounder', label: t.llmRoleGrounder, grounder: true }
    ].map(function (row) {
      var selected = roleSelectValue(draft, row.key, row.grounder);
      return '<div class="bot-llm-role"><label for="botLLMRole_' + row.key + '">' + escapeHtml(row.label) + '</label>'
        + '<select id="botLLMRole_' + row.key + '">' + llmRoleOptions(draft, row.key, row.grounder, selected) + '</select>'
        + roleModelFieldHTML(draft, row.key, row.grounder) + '</div>';
    }).join('');
    root.innerHTML = '<div class="bot-llm-roles-title" id="botLLMRolesTitle">' + escapeHtml(t.llmRolesTitle) + '</div>'
      + '<p class="bot-llm-roles-hint" id="botLLMRolesHint">' + escapeHtml(t.llmRolesHint) + '</p>'
      + rows;
  }

  function onLLMRoleChange(event) {
    var target = event && event.target;
    if (!target || !target.id || target.id.indexOf('botLLMRole_') !== 0) return;
    var key = target.id.slice('botLLMRole_'.length);
    if (key.indexOf('Model') === 0) return; // handled by onLLMRoleModelEdit
    var grounder = key === 'grounder';
    var draft = llmDraft();
    if (!draft.roles) draft.roles = defaultLLMRoles();
    draft.roles[key] = roleFromSelect(target.value, key, grounder);
    var panel = byID('tab-bots');
    if (panel) panel._llmDirty = true;
    // The model field belongs to the previous provider: repaint the role
    // block so the input resets and the datalist follows the new provider.
    paintLLMRoles();
    syncLLMBadge();
  }

  function onLLMRoleModelEdit(event) {
    var target = event && event.target;
    if (!target || !target.id || target.id.indexOf('botLLMRoleModel_') !== 0) return;
    var key = target.id.slice('botLLMRoleModel_'.length);
    var draft = llmDraft();
    if (!draft.roles || !draft.roles[key]) return;
    draft.roles[key].model = String(target.value || '').trim();
    var panel = byID('tab-bots');
    if (panel) panel._llmDirty = true;
  }

  // onLLMRoleClick routes the per-role "fetch models" button: the row's
  // pinned provider is fetched once and every picker that references it
  // refreshes from the shared cache.
  function onLLMRoleClick(event) {
    var target = event && event.target;
    if (!target || !target.id || target.id.indexOf('botLLMRoleFetch_') !== 0) return;
    var key = target.id.slice('botLLMRoleFetch_'.length);
    var role = (llmDraft().roles || {})[key] || {};
    if (role.mode !== 'pin' || isBotDefaultGroup(role.provider)) return;
    fetchLLMModelsFor(role.provider);
  }

  // refreshRoleModelPickers re-fills the datalists of role rows pinned to the
  // given provider, so a catalog fetched on a provider card (or here) shows
  // up in every role row that references it.
  function refreshRoleModelPickers(providerID) {
    ['foreground', 'background', 'computer_use', 'grounder'].forEach(function (key) {
      var role = (llmDraft().roles || {})[key] || {};
      if (role.mode !== 'pin' || isBotDefaultGroup(role.provider) || role.provider !== providerID) return;
      var list = byID('botLLMRoleModelList_' + key);
      if (list) list.innerHTML = llmModelOptionsHTML((llmModelCacheOf(providerID) || {}).models);
    });
  }

  function llmProviderFromID(id) {
    return llmDraft().providers.filter(function (item) { return item.id === id; })[0] || null;
  }

  function onLLMListClick(event) {
    var target = event && event.target;
    if (!target || !target.id) return;
    if (target.id.indexOf('botLLMEdit_') === 0) {
      openLLMProviderDialog('edit', target.id.slice('botLLMEdit_'.length));
      return;
    }
    if (target.id.indexOf('botLLMRemove_') !== 0) return;
    var id = target.id.slice('botLLMRemove_'.length);
    var draft = llmDraft();
    draft.providers = draft.providers.filter(function (item) { return item.id !== id; });
    if (draft.current === id) draft.current = botOfficialGroup;
    coerceDraftRoles(draft);
    var panel = byID('tab-bots');
    if (panel) panel._llmDirty = true;
    // Roles pinned to the removed provider were coerced above; the catalog
    // cache and any role picker still pointing at it go with the row.
    clearLLMModelsFor(id);
    paintLLM();
  }

  // ---- Provider dialog -------------------------------------------------------
  // Providers are added and edited in one modal. The list stays a compact
  // summary; the dialog carries the fields and the catalog fetch.

  function llmDialogLabels() {
    var t = text();
    setLabel('botLLMPNameLabel', t.llmName);
    setLabel('botLLMPProtocolLabel', t.llmProtocol);
    setLabel('botLLMPUrlLabel', t.llmURL);
    setLabel('botLLMPModelLabel', t.llmModel);
    setLabel('botLLMPKeyLabel', t.llmKey);
    var fetch = byID('botLLMPFetch');
    if (fetch) fetch.textContent = t.llmFetchModels;
    var cancel = byID('botLLMPCancel');
    if (cancel) cancel.textContent = t.llmCancel;
    var save = byID('botLLMPSave');
    if (save) save.textContent = t.llmDialogSave;
    var hint = byID('botLLMPKeyHint');
    if (hint) hint.textContent = t.llmKeyKeep;
  }

  function openLLMProviderDialog(mode, providerId) {
    var panel = byID('tab-bots');
    var modal = byID('botLLMPModal');
    if (!panel || !modal) return;
    if (mode === 'edit' && !providerId) return;
    var item = mode === 'edit' ? llmProviderFromID(providerId) : null;
    if (mode === 'edit' && !item) return;
    var t = text();
    llmDialogLabels();
    panel._llmDialog = { mode: mode === 'edit' ? 'edit' : 'create', id: item ? item.id : '', seq: 0 };
    setLabel('botLLMPModalTitle', item ? t.llmEditProvider : t.llmAdd);
    byID('botLLMPName').value = item ? item.name : '';
    byID('botLLMPProtocol').value = item && item.protocol === 'anthropic' ? 'anthropic' : 'openai';
    byID('botLLMPUrl').value = item ? item.url : '';
    byID('botLLMPModel').value = item ? item.model : '';
    // A stored key never rounds-trips to the browser. A typed-but-unsaved key
    // (key_set false) lives only in this draft, so prefill it; a saved key
    // starts blank and the placeholder explains keep-on-blank.
    byID('botLLMPKey').value = item && !item.key_set ? (item.key || '') : '';
    byID('botLLMPKey').placeholder = item && item.key_set ? t.llmKeyKeep : '';
    var cache = item ? llmModelCacheOf(item.id) : null;
    byID('botLLMPModelList').innerHTML = llmModelOptionsHTML(cache && cache.models);
    byID('botLLMPModelsStatus').textContent = llmModelsStatusText(cache);
    var fetch = byID('botLLMPFetch');
    if (fetch) fetch.disabled = false;
    modal.hidden = false;
    var name = byID('botLLMPName');
    if (name && name.focus) name.focus();
  }

  function closeLLMProviderDialog() {
    var modal = byID('botLLMPModal');
    if (modal) modal.hidden = true;
    var panel = byID('tab-bots');
    if (panel) panel._llmDialog = null;
  }

  // Clicking the dimmed backdrop (not the panel) dismisses the dialog.
  function onBotLLMPModalOverlayClick(event) {
    if (event && event.target === byID('botLLMPModal')) closeLLMProviderDialog();
  }

  function onBotLLMPModalKeydown(event) {
    if (!event || event.key !== 'Escape') return;
    var modal = byID('botLLMPModal');
    if (modal && !modal.hidden) closeLLMProviderDialog();
  }

  // onLLMPDialogEdit retires an in-flight dialog fetch once its coordinates
  // change: the answer would describe an endpoint the form no longer holds.
  function onLLMPDialogEdit(event) {
    var target = event && event.target;
    if (!target || !target.id) return;
    if (target.id !== 'botLLMPUrl' && target.id !== 'botLLMPProtocol' && target.id !== 'botLLMPKey') return;
    var panel = byID('tab-bots');
    var dialog = panel && panel._llmDialog;
    if (!dialog) return;
    dialog.seq = (dialog.seq || 0) + 1;
    var list = byID('botLLMPModelList');
    if (list) list.innerHTML = '';
    var status = byID('botLLMPModelsStatus');
    if (status) status.textContent = '';
    var fetch = byID('botLLMPFetch');
    if (fetch && fetch.disabled) {
      fetch.disabled = false;
      fetch.textContent = text().llmFetchModels;
    }
  }

  // validateLLMProviderDraft enforces the same shape rules the server applies
  // to one provider row, plus the per-draft name rules.
  function validateLLMProviderDraft(item, draft) {
    var t = text();
    var name = String(item.name || '').trim();
    if (!name) return t.llmNeedName;
    var keyName = name.toLowerCase();
    if (keyName === 'hub-llm' || keyName === 'system-free' || keyName === botOfficialGroup) return t.llmReservedName;
    var duplicated = (draft.providers || []).some(function (other) {
      return other.id !== item.id && llmNamesConflict(String(other.name || '').trim().toLowerCase(), keyName);
    });
    if (duplicated) return t.llmDuplicateName;
    if (item.protocol !== 'openai' && item.protocol !== 'anthropic') return t.llmBadProtocol;
    if (!validServiceUrl(item.url)) return t.llmBadURL;
    if (!String(item.model || '').trim()) return t.llmNeedModel;
    if (!String(item.key || '').trim() && !item.key_set) return t.llmNeedKey;
    return '';
  }

  function saveLLMProviderDialog() {
    var panel = byID('tab-bots');
    var modal = byID('botLLMPModal');
    if (!panel || !modal || modal.hidden) return;
    var dialog = panel._llmDialog || { mode: 'create' };
    var existing = dialog.mode === 'edit' ? llmProviderFromID(dialog.id) : null;
    if (dialog.mode === 'edit' && !existing) return;
    var draft = llmDraft();
    var item = {
      id: existing ? existing.id : newLLMProviderID(),
      name: (byID('botLLMPName').value || '').trim(),
      protocol: (byID('botLLMPProtocol').value || 'openai').toLowerCase() === 'anthropic' ? 'anthropic' : 'openai',
      url: (byID('botLLMPUrl').value || '').trim(),
      model: (byID('botLLMPModel').value || '').trim(),
      key: (byID('botLLMPKey').value || '').trim(),
      key_set: existing ? !!existing.key_set : false
    };
    var error = validateLLMProviderDraft(item, draft);
    if (error) {
      showToast(error, 'error');
      return;
    }
    if (!existing) {
      if ((draft.providers || []).length >= 8) {
        showToast(text().llmLimit, 'error');
        return;
      }
      draft.providers.push(item);
    } else {
      var coordinatesChanged = existing.protocol !== item.protocol ||
        existing.url !== item.url ||
        String(existing.key || '') !== item.key;
      draft.providers = draft.providers.map(function (other) { return other.id === item.id ? item : other; });
      if (coordinatesChanged) clearLLMModelsFor(item.id);
    }
    panel._llmDirty = true;
    closeLLMProviderDialog();
    paintLLM();
  }

  // fetchLLMDialogModels fetches the catalog for the coordinates typed in the
  // dialog, which may differ from the saved provider yet. An edit reuses the
  // provider id so the hub can fall back to the stored key; the answer fills
  // the dialog datalist and, for an edit, every role picker on that provider.
  function fetchLLMDialogModels() {
    var panel = byID('tab-bots');
    var modal = byID('botLLMPModal');
    if (!panel || !modal || modal.hidden) return;
    var dialog = panel._llmDialog || { mode: 'create' };
    var url = (byID('botLLMPUrl').value || '').trim();
    var key = (byID('botLLMPKey').value || '').trim();
    var protocol = (byID('botLLMPProtocol').value || 'openai').toLowerCase() === 'anthropic' ? 'anthropic' : 'openai';
    if (!validServiceUrl(url)) {
      showToast(text().llmBadURL, 'error');
      return;
    }
    var stored = dialog.mode === 'edit' ? llmProviderFromID(dialog.id) : null;
    var keySet = !!stored && !!stored.key_set;
    if (!key && !keySet) {
      showToast(text().llmFetchNeedURL, 'error');
      return;
    }
    var body = { protocol: protocol, url: url };
    if (stored) body.provider_id = stored.id;
    if (key) body.key = key;
    dialog.seq = (dialog.seq || 0) + 1;
    var seq = dialog.seq;
    var fetch = byID('botLLMPFetch');
    if (fetch) {
      fetch.disabled = true;
      fetch.textContent = text().llmFetching;
    }
    api('/api/admin/bots/llm/models', { method: 'POST', body: JSON.stringify(body) }).then(function (result) {
      if (!panel._llmDialog || panel._llmDialog.seq !== seq) return;
      var models = result && Array.isArray(result.models) ? result.models : [];
      var cache = models.length ? { models: models, error: '' } : { models: null, error: text().llmModelsEmpty };
      var list = byID('botLLMPModelList');
      if (list) list.innerHTML = llmModelOptionsHTML(models);
      var status = byID('botLLMPModelsStatus');
      if (status) status.textContent = llmModelsStatusText(cache);
      if (stored) {
        llmModelCacheStore(stored.id, cache);
        refreshRoleModelPickers(stored.id);
      }
      if (!models.length) showToast(cache.error, 'info');
    }).catch(function (err) {
      if (!panel._llmDialog || panel._llmDialog.seq !== seq) return;
      var message = llmModelsMessage(err);
      var status = byID('botLLMPModelsStatus');
      if (status) status.textContent = message;
      showToast(message, 'error');
    }).then(function () {
      if (!panel._llmDialog || panel._llmDialog.seq !== seq) return;
      if (fetch) {
        fetch.disabled = false;
        fetch.textContent = text().llmFetchModels;
      }
    });
  }

  function addLLMProvider() {
    var draft = llmDraft();
    if ((draft.providers || []).length >= 8) {
      showToast(text().llmLimit, 'error');
      return;
    }
    openLLMProviderDialog('create');
  }

  // MaClaw treats these display names as one provider. The same set is
  // MaclawLLMProviderNameEqual on the server.
  var llmSameProviderNames = {
    '\u667a\u8c31\u7f16\u7a0b': true,
    '\u667a\u8c31 glm (coding)': true,
    'zhipu glm coding': true
  };

  function llmNamesConflict(left, right) {
    if (left === right) return true;
    return !!llmSameProviderNames[left] && !!llmSameProviderNames[right];
  }

  function llmDraftError(draft) {
    var t = text();
    var providers = draft.providers || [];
    for (var i = 0; i < providers.length; i += 1) {
      var error = validateLLMProviderDraft(providers[i], draft);
      if (error) return error;
    }
    // A role model override only exists on a real provider pin, and the same
    // shape rules the server enforces.
    var pinnedRoles = ['foreground', 'background', 'computer_use'];
    for (var r = 0; r < pinnedRoles.length; r += 1) {
      var role = (draft.roles || {})[pinnedRoles[r]];
      var roleModel = String((role && role.model) || '').trim();
      if (role && role.mode === 'pin' && roleModel && (roleModel.length > 128 || /[\r\n]/.test(roleModel))) {
        return t.llmRoleModelBad;
      }
    }
    var current = llmForegroundProvider(draft);
    if (!isBotDefaultGroup(current) && !providers.some(function (item) { return item.id === current; })) return t.llmBadCurrent;
    return '';
  }

  function llmRoleRequest(role, key) {
    // Foreground is the default route: a legacy row stored as inherit pins
    // the settings' current provider instead of the official group.
    if (key === 'foreground') {
      var pinned = llmForegroundProvider(llmDraft());
      var out = { mode: 'pin', provider: isBotDefaultGroup(pinned) ? botOfficialGroup : pinned };
      var pinnedModel = role && role.mode === 'pin' ? String(role.model || '').trim() : '';
      if (!isBotDefaultGroup(out.provider) && pinnedModel) out.model = pinnedModel;
      return out;
    }
    var mode = role && role.mode ? role.mode : 'inherit';
    var request = { mode: mode };
    if (mode === 'pin') {
      var provider = isBotDefaultGroup(role.provider) ? botOfficialGroup : String(role.provider || '');
      request.provider = provider;
      // An empty model keeps the provider's default; only a real provider
      // pin carries an override.
      if (!isBotDefaultGroup(provider) && String(role.model || '').trim()) {
        request.model = String(role.model).trim();
      }
    }
    return request;
  }

  function llmRequestBody(draft) {
    var roles = normalizeDraftRoles(draft && draft.roles, draft || {});
    var current = llmForegroundProvider(draft);
    return {
      current: isBotDefaultGroup(current) ? botOfficialGroup : current,
      providers: (draft.providers || []).map(function (item) {
        var out = {
          id: item.id,
          name: String(item.name || '').trim(),
          protocol: String(item.protocol || '').toLowerCase(),
          url: String(item.url || '').trim(),
          model: String(item.model || '').trim()
        };
        var key = String(item.key || '').trim();
        if (key) out.key = key;
        return out;
      }),
      roles: {
        foreground: llmRoleRequest(roles.foreground, 'foreground'),
        background: llmRoleRequest(roles.background, 'background'),
        computer_use: llmRoleRequest(roles.computer_use, 'computer_use'),
        grounder: llmRoleRequest(roles.grounder, 'grounder')
      }
    };
  }

  function saveLLM() {
    var panel = byID('tab-bots');
    if (panel && panel._llmSaving) return;
    var draft = llmDraft();
    var error = llmDraftError(draft);
    if (error) {
      showToast(error, 'error');
      return;
    }
    var sent = JSON.stringify(llmRequestBody(draft));
    if (panel) panel._llmSaving = true;
    var button = byID('botLLMSave');
    if (button) button.disabled = true;
    return api('/api/admin/bots/llm', { method: 'PUT', body: sent }).then(function (view) {
      // The request stored one draft. A keystroke after it left is another
      // draft, so the success line must not say the form now on screen is saved.
      var same = !!(panel && JSON.stringify(llmRequestBody(llmDraft())) === sent);
      if (same) panel._llmDirty = false;
      render(view || {});
      showToast(same ? text().llmSaved : text().llmSavedStale, same ? 'success' : 'info');
    }).catch(function (err) {
      showToast(messageOf(err), 'error');
    }).then(function () {
      if (panel) panel._llmSaving = false;
      if (button) button.disabled = false;
    });
  }

  function validServiceUrl(value) {
    var raw = String(value || '').trim();
    var parsed;
    try { parsed = new URL(raw); } catch (err) { return false; }
    return (parsed.protocol === 'http:' || parsed.protocol === 'https:') && !!parsed.host && !parsed.username && !parsed.password;
  }

  function saveConnection() {
    var panel = byID('tab-bots');
    if (panel && panel._connSaving) return;
    var base = (byID('botBaseUrl').value || '').trim();
    if (!base) {
      showToast(text().needUrl, 'error');
      return;
    }
    if (!validServiceUrl(base)) {
      showToast(text().badUrl, 'error');
      return;
    }
    var body = { base_url: base };
    var token = byID('botAccessToken').value || '';
    if (token !== '') body.access_token = token;
    var adminSecret = byID('botAdminSecret').value || '';
    if (adminSecret !== '') body.admin_secret = adminSecret;
    if (panel) panel._connSaving = true;
    var button = byID('botSave');
    if (button) button.disabled = true;
    return api('/api/admin/bots/settings', { method: 'PUT', body: JSON.stringify(body) }).then(function (view) {
      byID('botAccessToken').value = '';
      byID('botAdminSecret').value = '';
      var urlInput = byID('botBaseUrl');
      if (urlInput) urlInput._serverValue = urlInput.value;
      if (panel) panel._botConnOk = false;
      /* The last test result describes the settings that were just replaced. */
      setLabel('botConnStatus', '');
      render(view || {});
      showToast(text().saved, 'success');
    }).catch(function (err) {
      showToast(messageOf(err), 'error');
    }).then(function () {
      if (panel) panel._connSaving = false;
      if (button) button.disabled = false;
    });
  }

  function testConnection() {
    var panel = byID('tab-bots');
    if (panel && panel._connTesting) return;
    var view = panel && panel._botView || {};
    var typedUrl = (byID('botBaseUrl').value || '').trim().replace(/\/+$/, '');
    var savedUrl = String(view.base_url || '').trim().replace(/\/+$/, '');
    if (typedUrl !== savedUrl || (byID('botAccessToken').value || '') !== '') {
      showToast(text().saveBeforeTest, 'error');
      return;
    }
    if (panel) panel._connTesting = true;
    var button = byID('botTest');
    if (button) button.disabled = true;
    var status = byID('botConnStatus');
    if (status) status.textContent = text().testing;
    return api('/api/admin/bots/connection/test', { method: 'POST', body: '{}' }).then(function (result) {
      var count = result && result.instance_count != null ? result.instance_count : 0;
      var msg = text().connected.replace('{count}', String(count));
      if (panel) panel._botConnOk = true;
      byID('botConnStatus').textContent = msg;
      renderConnBadge(panel && panel._botView);
      showToast(msg, 'success');
    }).catch(function (err) {
      if (panel) panel._botConnOk = false;
      byID('botConnStatus').textContent = messageOf(err);
      renderConnBadge(panel && panel._botView);
      showToast(messageOf(err), 'error');
    }).then(function () {
      if (panel) panel._connTesting = false;
      if (button) button.disabled = false;
    });
  }

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') {
    global.AdminTabRegistry.registerTab({
      id: 'bots',
      title: function () { return text().title; },
      subtitle: function () { return text().subtitle; },
      onOpen: openBotTab
    });
  }
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(applyBotI18n);
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', applyBotI18n);
  } else {
    applyBotI18n();
  }
})(window);
