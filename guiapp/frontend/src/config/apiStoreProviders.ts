export type ApiStoreProvider = {
    name: string;
    url: string;
    isRelay?: boolean;
    hasSubscription?: boolean;
    isBilling?: boolean;
};

export const apiStoreProviders: ApiStoreProvider[] = [
    { name: 'ChatFire', url: 'https://api.chatfire.cn/register?aff=jira', isRelay: true },
    { name: '\u667a\u8c31', url: 'https://bigmodel.cn/glm-coding', hasSubscription: true },
    { name: '\u6708\u4e4b\u6697\u9762', url: 'https://www.kimi.com/membership/pricing?from=upgrade_plan&track_id=1d2446f5-f45f-4ae5-961e-c0afe936a115', hasSubscription: true },
    { name: '\u8c46\u5305', url: 'https://www.volcengine.com/activity/codingplan', hasSubscription: true },
    { name: '\u817e\u8baf\u4e91', url: 'https://cloud.tencent.com/act/pro/codingplan', hasSubscription: true },
    { name: '\u8baf\u98de\u661f\u8fb0', url: 'https://www.xfyun.cn/doc/spark/CodingPlan.html', hasSubscription: true },
    { name: 'MiniMax', url: 'https://platform.minimaxi.com/user-center/payment/coding-plan', hasSubscription: true },
    { name: '\u767e\u5ea6\u5343\u5e06', url: 'https://cloud.baidu.com/product/codingplan.html', hasSubscription: true },
    { name: 'DeepSeek', url: 'https://platform.deepseek.com/api_keys', isBilling: true },
    { name: '\u5c0f\u7c73', url: 'https://platform.xiaomimimo.com/#/console/api-keys', isBilling: true },
    { name: '\u6469\u5c14\u7ebf\u7a0b', url: 'https://code.mthreads.com/', hasSubscription: true },
    { name: '\u5feb\u624b', url: 'https://www.streamlake.com/marketing/coding-plan', hasSubscription: true },
    { name: '\u963f\u91cc\u4e91', url: 'https://coding.dashscope.aliyuncs.com/', hasSubscription: true },
];
