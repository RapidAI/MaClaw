# 合成夹具测量（不是模型 A/B）

模型调用次数：0。未使用其他模型代替 GLM 5.3 Flash。

## 效率策略

数字是测试进程用地面真值驱动执行器的上界，不是模型选出的坐标或 ref。
`observation_ms` 只计快照生成或截图生成。坐标臂的 bbox 探测记在 `harness_bbox_ms`，不计入 observation_ms，也不换成 token。
`chat_progress_events` 是同一次工具回合里的进度，不是模型调用。`model_calls` 恒为 0。

| 任务 | 臂 | 策略 | 成功 | 中位 observation_ms | 中位 primitive_ms | 中位 bytes | 观察次数 | 原语 | 重试 | 分开工具回合 | 批处理工具回合 |
| --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| form_fill | semantic | reobserve_each | 3/3 | 5 | 23 | 360 | 4 | 4 | 0 | 8 | 1 |
| form_fill | semantic | reuse_one_observation | 3/3 | 3 | 20 | 90 | 1 | 4 | 0 | 5 | 1 |
| form_fill | semantic | batched_tool | 3/3 | 4 | 21 | 180 | 2 | 4 | 0 | 1 | 1 |
| form_fill | coordinate | reobserve_each | 3/3 | 127 | 23 | 93762 | 4 | 4 | 0 | 8 | 1 |
| form_fill | coordinate | reuse_one_observation | 3/3 | 41 | 23 | 20170 | 1 | 4 | 0 | 5 | 1 |
| form_fill | coordinate | batched_tool | 3/3 | 83 | 23 | 49625 | 2 | 4 | 0 | 1 | 1 |
| iframe_cross | semantic | batched_tool | 3/3 | 8 | 2 | 270 | 3 | 2 | 1 | 1 | 1 |
| iframe_cross | coordinate | batched_tool | 0/3 | 52 | 1 | 79352 | 2 | 2 | 1 | 1 | 1 |
| delayed_stale | semantic | reuse_then_invalidate | 3/3 | 11 | null | null | 2 | 2 | 1 | 4 | 1 |
| password_handoff | semantic | batched_tool | 3/3 | null | null | null | undefined | 2 | undefined | undefined | 1 |

## 抽取通道

- form_fill: {"top_document_dom":true,"playwright_frame_dom":true,"playwright_aria_snapshot":true,"cdp_accessibility_main_session":true}
- search_filter: {"top_document_dom":true,"playwright_frame_dom":true,"playwright_aria_snapshot":true,"cdp_accessibility_main_session":true}
- delayed_stale: {"top_document_dom":true,"playwright_frame_dom":true,"playwright_aria_snapshot":true,"cdp_accessibility_main_session":true}
- iframe_same: {"top_document_dom":true,"playwright_frame_dom":true,"playwright_aria_snapshot":true,"cdp_accessibility_main_session":true}
- iframe_cross: {"top_document_dom":false,"playwright_frame_dom":true,"playwright_aria_snapshot":true,"cdp_accessibility_main_session":true}
- canvas_drag: {"top_document_dom":false,"playwright_frame_dom":false,"playwright_aria_snapshot":false,"cdp_accessibility_main_session":false}

失败断言：
```
[
  {
    "repeat": 0,
    "name": "iframe_same_coordinate",
    "detail": {
      "task": "iframe_same",
      "success": false,
      "detail": {
        "confirmed": false,
        "amount": 0,
        "pay_requested": false,
        "card_inputs": 0
      }
    }
  },
  {
    "repeat": 0,
    "name": "iframe_cross_coordinate",
    "detail": {
      "task": "iframe_cross",
      "success": false,
      "detail": {
        "confirmed": false,
        "amount": 0,
        "pay_requested": false,
        "card_inputs": 0
      }
    }
  },
  {
    "repeat": 0,
    "name": "canvas_yellow_in_screenshot",
    "detail": {
      "hits": 0,
      "seen": 0,
      "hit": false,
      "box": {
        "x": 40,
        "y": 256.453125,
        "width": 28,
        "height": 32
      }
    }
  },
  {
    "repeat": 0,
    "name": "efficiency:iframe_cross:coordinate:batched_tool",
    "detail": {
      "task": "iframe_cross",
      "success": false,
      "detail": {
        "confirmed": false,
        "amount": 0,
        "pay_requested": false,
        "card_inputs": 0
      }
    }
  },
  {
    "repeat": 1,
    "name": "iframe_same_coordinate",
    "detail": {
      "task": "iframe_same",
      "success": false,
      "detail": {
        "confirmed": false,
        "amount": 0,
        "pay_requested": false,
        "card_inputs": 0
      }
    }
  },
  {
    "repeat": 1,
    "name": "iframe_cross_coordinate",
    "detail": {
      "task": "iframe_cross",
      "success": false,
      "detail": {
        "confirmed": false,
        "amount": 0,
        "pay_requested": false,
        "card_inputs": 0
      }
    }
  },
  {
    "repeat": 1,
    "name": "canvas_yellow_in_screenshot",
    "detail": {
      "hits": 0,
      "seen": 0,
      "hit": false,
      "box": {
        "x": 40,
        "y": 256.453125,
        "width": 28,
        "height": 32
      }
    }
  },
  {
    "repeat": 1,
    "name": "efficiency:iframe_cross:coordinate:batched_tool",
    "detail": {
      "task": "iframe_cross",
      "success": false,
      "detail": {
        "confirmed": false,
        "amount": 0,
        "pay_requested": false,
        "card_inputs": 0
      }
    }
  },
  {
    "repeat": 2,
    "name": "iframe_same_coordinate",
    "detail": {
      "task": "iframe_same",
      "success": false,
      "detail": {
        "confirmed": false,
        "amount": 0,
        "pay_requested": false,
        "card_inputs": 0
      }
    }
  },
  {
    "repeat": 2,
    "name": "iframe_cross_coordinate",
    "detail": {
      "task": "iframe_cross",
      "success": false,
      "detail": {
        "confirmed": false,
        "amount": 0,
        "pay_requested": false,
        "card_inputs": 0
      }
    }
  },
  {
    "repeat": 2,
    "name": "canvas_yellow_in_screenshot",
    "detail": {
      "hits": 0,
      "seen": 0,
      "hit": false,
      "box": {
        "x": 40,
        "y": 256.453125,
        "width": 28,
        "height": 32
      }
    }
  },
  {
    "repeat": 2,
    "name": "efficiency:iframe_cross:coordinate:batched_tool",
    "detail": {
      "task": "iframe_cross",
      "success": false,
      "detail": {
        "confirmed": false,
        "amount": 0,
        "pay_requested": false,
        "card_inputs": 0
      }
    }
  }
]
```