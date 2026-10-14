# Per-bot virtual desktops (desktop_mode = isolated)

Status: MVP. Default for every bot is `shared` (the old behaviour: all bots of
a person drive that person's one desktop, display `:20`).

## Model

An isolated bot drives its **own X display** (`:21`+) inside its owner's
existing desktop container (no extra container). Each bot display runs only
Xvfb + xfwm4 and its own Chromium with its own profile
(`/desktops/<botkey>/profile`, kept across stops; no login is copied from the
owner). fcitx5 starts only when the person takes over.

* Lazy start on the bot's first desktop action.
* Idle stop after ~10 min without activity (`MACLAW_BOT_IDLE_SECONDS`); the
  profile (cookies/logins) stays.
* At most `MACLAW_DESKTOP_MAX_BOTS` (default 3) bot displays run per
  container; the least recently used idle one is stopped. The owner's `:20`,
  a bot with a running task, and a bot under takeover are never evicted.
* VNC (x11vnc + websockify) for a bot display starts only when someone opens
  its view.
* Memory: about 70 MB per idle display, ~0.25 GB per bot Chromium; plan
  ~0.5 GB per active bot.

Internally the bot's "desktop user" is `<owner user id>::desk::<bot id>`.
Hub and MaClawSrv key all per-desktop state (holds, views, handoffs,
keyboard) by it, so two bots of one person never share a lock or a picture.
The display is always derived by desktopd from the bot id; a client cannot
name a display for a bot.

## Admin: switch a bot

```
PUT /api/admin/bots/{bot_id}/desktop-mode     (tenant admin)
{"desktop_mode": "isolated"}   # or "shared"
-> 200 {"id": "...", "desktop_mode": "isolated"}
```

Takes effect on the bot's next command.

## GUI (person) API

Unchanged endpoints, now per bot:

* `GET  /api/v1/bots/{id}/desktop` — watch; `POST` — hold the view.
  Response adds `"desktop_mode": "shared"|"isolated"`. For an isolated bot,
  `novnc_url` is **that bot's** display (same token gate as before). A GUI
  switcher can simply list the person's bots and call this per bot; for
  shared bots all URLs show the same desktop.
* Bot replies keep the existing `desktop_handoff` / `attention_reason`
  fields. When the bot needs a login, 2FA or captcha it calls the desktop
  action `request_takeover(reason)`; the reply then has `desktop_handoff=true`
  and `attention_reason=<reason>` and the GUI should open `novnc_url`.

New:

```
POST /api/v1/bots/{id}/desktop/handback
{"message": "optional text", "phase": "execute", "resume": true}
-> 202 {"handed_back": true, "accepted": true, "run_id": "...", "status": "running", "client_message_id": "..."}
   (or 200 {"handed_back": true, "reply": {...}} when settled immediately;
    "resume": false -> 200 {"handed_back": true} and no new message)
```

Ends the takeover on the bot's display, returns the keyboard to the bot, and
(by default) sends a continuation message so the bot resumes the task. Poll
the run like any other bot message.

Auto-yield: while a takeover is open, or within ~20 s
(`MACLAW_BOT_HUMAN_YIELD_SECONDS`) of a key press / click / paste coming
through that display's noVNC, the bot's desktop actions are refused with a
"the person is using this desktop" error, and the model is told to stop.

## Service APIs

* desktopd `/v1/desktops/*`: optional `bot_id` (JSON body or query). Without
  it nothing changes (`:20`). `stop` for a bot is a no-op (idle timer stops
  it); an owner stop while one of its bots is busy only stops `:20`.
  Ctl ops `bot_state`, `takeover_begin`, `takeover_end` need a bot.
* Hub `POST /api/v1/desktop-services/session`: optional `bot_id` (bot id or
  instance id). Honoured only for this user's bot that is isolated; otherwise
  the user's own desktop.
