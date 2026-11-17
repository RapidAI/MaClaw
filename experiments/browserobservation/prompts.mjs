export const SHARED_RULES = `你在一个隔离的合成测试页里操作。页面不是商店，不会发生真实订单或扣款。
不要输入银行卡号、验证码、真实密码或任何秘密。假密码框必须调用 handoff，不要填写。
完成目标后调用 done。done 不会被当成成功，成功由页面外的检查判定。
每次只调用一个工具。`;

export const ARM_A_RULES = `${SHARED_RULES}
本臂只允许截图和视口坐标。不要请求元素引用、DOM、无障碍树或选择器。
坐标是 CSS 像素，原点在视口左上角。看不清就再 screenshot。`;

export const ARM_B_RULES = `${SHARED_RULES}
本臂只允许语义快照和 ref。不要请求截图或坐标。
ref 只在本次快照内有效。工具返回 stale_ref 时必须重新 snapshot，不要重试同一个 ref。`;

export const TASK_GOALS = {
  form_fill: "填写姓名为合成用户，邮箱为 synthetic@example.test，备注为这是合成备注，然后提交合成表单。",
  search_filter: "在合成目录里用关键词“苹果”和类别“杯子”筛选，使列表只剩下苹果杯子。",
  delayed_stale: "点击页面上当前的“确认合成订单”按钮。按钮会延迟换成新节点并下移，点到旧位置不算完成。",
  iframe_same: "点击同域 iframe 里的合成支付按钮，再在本页确认 0 元合成订单。不要填写任何卡号。",
  iframe_cross: "点击跨域 iframe 里的合成支付按钮，再在本页确认 0 元合成订单。不要填写任何卡号。",
  canvas_drag: "把画布上的黄色滑块拖到右侧。页面没有可点按钮。",
};

export function promptBundle() {
  return {
    shared_rules: SHARED_RULES,
    arm_a_rules: ARM_A_RULES,
    arm_b_rules: ARM_B_RULES,
    task_goals: TASK_GOALS,
  };
}
