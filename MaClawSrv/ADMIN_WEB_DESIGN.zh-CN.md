# MaClawSrv Admin Web 绠＄悊闈㈣璁?

## 1. 鑳屾櫙

`MaClawSrv` 宸茬粡鍏峰闈㈠悜鐢ㄦ埛瀹炰緥鐨?REST 鑳藉姏锛屽寘鎷?tenant銆乽ser銆乧redential銆乮nstance銆乻ession銆乺un銆丮CP銆乻kill銆乲nowledge 绛夎祫婧愮鐞嗐€傞殢鐫€鏈湴 bash銆佹湰鍦?MCP server銆乻kill 鎵ц銆乻cheduler銆乻andbox銆佹棩蹇楀拰澶囦唤鑳藉姏澧炲锛屾湇鍔¤繕闇€瑕佷竴涓彧闈㈠悜杩愮淮绠＄悊鍛樼殑绠＄悊闈€?

鏈枃璁捐鐨?`Admin Web` 涓嶆槸 Maclaw 鐢ㄦ埛宸ヤ綔鍙帮紝涔熶笉鏄煇涓敤鎴峰疄渚嬬殑閰嶇疆椤碉紝鑰屾槸 `maclawsrv` 杩涚▼鑷韩鐨勬帶鍒跺彴銆傚畠鐢ㄤ簬瀹夎閮ㄧ讲銆佽繍琛岃瘖鏂€佸畨鍏ㄧ瓥鐣ャ€佸叏灞€鑳藉姏寮€鍏炽€佹棩蹇楁煡鐪嬨€佸浠芥仮澶嶃€乻andbox 绠＄悊鍜屾湇鍔＄骇鍛婅銆?

## 2. 鐩爣

- 缁欑鐞嗗憳鎻愪緵缁熶竴鍏ュ彛锛岀鐞?`MaClawSrv` 鏈嶅姟绾ч厤缃€?
- 鎶婄敤鎴峰疄渚嬮厤缃拰鏈嶅姟杩涚▼鑷韩閰嶇疆鍒嗙锛岄伩鍏嶉珮鍗辫繍缁磋兘鍔涙毚闇茬粰鏅€?bearer token 鐢ㄦ埛銆?
- 鎶?sandbox 鐨勬娴嬨€佸畨瑁呭缓璁€侀厤缃€佸惎鍋滈獙璇佸拰鎵ц瀹¤绾冲叆 admin 鑳藉姏銆?
- 鎻愪緵閮ㄧ讲銆佸崌绾с€佹晠闅滄帓鏌ヨ兘鍔涳紝鍖呮嫭閿欒鏃ュ織銆佽繍琛屾棩蹇椼€佸璁′簨浠躲€佹寚鏍囥€乺eady 妫€鏌ュ拰 async job 鐘舵€併€?
- 淇濇寔浣庝镜鍏ワ細浼樺厛澶嶇敤宸叉湁 admin API銆乺eadiness銆乵etrics銆乤udit銆乻napshot銆乻kill-source銆乲nowledge admin 鎺ュ彛銆?
- 鏀寔鏃?Web UI 鐨勮嚜鍔ㄥ寲鍦烘櫙锛欰dmin Web 鎵€鏈夎兘鍔涢兘搴旀湁瀵瑰簲 `/api/v1/admin/...` JSON API銆?

## 3. 闈炵洰鏍?

- 涓嶆妸鏅€氱敤鎴风殑 agent 鑱婂ぉ銆乻ession 娴忚銆乺un 浜や簰鎼繘 admin web銆?
- 涓嶈 admin web 鐩存帴淇濆瓨鐢ㄦ埛 LLM API Key锛岄櫎闈炶繘鍏ユ槑纭殑 tenant/user provisioning 娴佺▼銆?
- 涓嶉粯璁よ嚜鍔ㄦ墽琛?`sudo apt install` 绛夌郴缁熺骇瀹夎鍔ㄤ綔銆傚畨瑁呭彲浠ヨ鏄惧紡瑙﹀彂锛屼絾榛樿搴斾互妫€娴嬪拰寤鸿涓轰富銆?
- 涓嶈姹?sandbox 鏀寔 Windows/macOS銆傜涓€闃舵 sandbox 绠＄悊鑳藉姏鍙互闄愬畾 Linux銆?
- 涓嶆妸浼犵粺瀹瑰櫒杩愯鏃朵綔涓洪粯璁ゆ柟妗堬紱Docker/Podman 鍙綔涓烘湭鏉ユ墿灞曞悗绔€?

## 4. 鏉冮檺妯″瀷

Admin Web 闇€瑕佹敮鎸佺涓€娆＄櫥褰曞垵濮嬪寲锛屾ā寮忓弬鑰?`hub` / `hubcenter` 鐨勯鍚?setup 娴佺▼銆傚畨鍏ㄩ槻鎶ゆ部鐢ㄧ幇鏈?MaClawSrv 瀹夊叏妯″瀷锛歛dmin API 缁х画浣跨敤 `X-MaClaw-Admin-Secret`銆佸惎鍔ㄦ湡寮哄埗鏍￠獙 `MACLAW_ADMIN_SECRET` / `MACLAW_TOKEN_SECRET`銆乤uth limiter銆乴oopback/TLS 浼犺緭绾︽潫銆佹晱鎰熷瓧娈佃劚鏁忓拰 audit 璁板綍銆侫dmin UI 鏈韩鍙槸 Admin API client锛屼笉寮曞叆缁曡繃鐜版湁妯″瀷鐨勬柊鏉冮檺閫氶亾銆?

### 4.1 Bootstrap 鐘舵€?

鏈嶅姟鍚姩鍚庡厛鍒ゆ柇鏄惁宸茬粡鍒濆鍖?admin 韬唤锛?

```text
initialized = 瀛樺湪 admin user/session 閰嶇疆锛屼笖鑷冲皯涓€涓?owner 璐﹀彿鍙敤
```

寤鸿鏂板 bootstrap state锛?

```text
MACLAW_DATA_ROOT/
  state/
    admin_bootstrap.json
    admin_users.json
    admin_sessions.json
```

`admin_bootstrap.json` 鍙繚瀛樺垵濮嬪寲鐘舵€併€佹椂闂淬€佸垵濮嬪寲鐗堟湰銆乻etup token hash锛屼笉淇濆瓨鏄庢枃瀵嗙爜鎴栨槑鏂?token銆?

### 4.2 棣栨鍒濆鍖栧叆鍙?

鏈垵濮嬪寲鏃讹細

- Admin Web 鍙紑鏀?setup 椤甸潰銆乭ealth/livez/readyz銆乥ootstrap status 鍜?bootstrap initialize API銆?
- 鎵€鏈夋櫘閫?`/api/v1/admin/...` 鍐欒兘鍔涜繑鍥?`423 Locked` 鎴?`403 setup_required`銆?
- UI 鏄剧ず鈥滃垵濮嬪寲 MaClawSrv Admin鈥濓紝瑕佹眰鍒涘缓绗竴涓?owner 璐﹀彿銆?
- 鍒濆鍖栧畬鎴愬悗绔嬪嵆绂佺敤 bootstrap initialize API銆?

寤鸿 API锛?

```text
GET  /api/v1/admin/bootstrap/status
POST /api/v1/admin/bootstrap/initialize
POST /api/v1/admin/auth/login
POST /api/v1/admin/auth/logout
GET  /api/v1/admin/auth/me
POST /api/v1/admin/auth/change-password
```

`GET /api/v1/admin/bootstrap/status` 鍦ㄦ湭鐧诲綍鏃朵篃鍙闂紝浣嗗彧鑳借繑鍥為潪鏁忔劅鐘舵€侊細

```json
{
  "initialized": false,
  "setup_required": true,
  "setup_token_required": true,
  "password_policy": {
    "min_length": 12,
    "require_mixed_classes": true
  }
}
```

`POST /api/v1/admin/bootstrap/initialize`锛?

```json
{
  "setup_token": "one-time-token-from-console-or-env",
  "owner": {
    "username": "admin",
    "display_name": "Administrator",
    "password": "..."
  },
  "service_config": {
    "sandbox": {
      "mode": "auto",
      "strict": false
    }
  }
}
```

鍝嶅簲锛?

```json
{
  "initialized": true,
  "owner_id": "admin_xxx",
  "session": {
    "expires_at": "2026-05-18T00:00:00Z"
  },
  "next_steps": ["run_sandbox_detect", "review_security_posture"]
}
```

### 4.3 Setup Token

涓轰簡閬垮厤鏈垵濮嬪寲绐楀彛琚繙绋嬫姠鍗狅紝bootstrap initialize 蹇呴』瑕佹眰涓€娆℃€?setup token銆傛帹鑽愭潵婧愪紭鍏堢骇锛?

```text
MACLAW_ADMIN_SETUP_TOKEN -> 棣栨鍚姩鑷姩鐢熸垚骞舵墦鍗板埌 console/log -> installer 鍐欏叆鏈満 only-readable 鏂囦欢
```

鑷姩鐢熸垚鏃讹細

- token 鍙湪 loopback 鐩戝惉鏃跺厑璁镐娇鐢ㄣ€?
- token 鏄庢枃鍙墦鍗颁竴娆°€?
- 鏂囦欢鏉冮檺蹇呴』 owner-only銆?
- 鍒濆鍖栨垚鍔熷悗鍒犻櫎 token 鏂囦欢骞舵竻绌?bootstrap pending 鐘舵€併€?
- 濡傛灉鏈嶅姟鐩戝惉闈?loopback 涓旀湭鍚敤 TLS锛岀姝㈤€氳繃 Web 瀹屾垚棣栨鍒濆鍖栥€?

寤鸿 token 鏂囦欢锛?

```text
MACLAW_DATA_ROOT/state/admin_setup_token
```

### 4.4 Admin API 瀹夊叏妯″瀷

Admin UI 閫氳繃鐜版湁 Admin API 瀹屾垚鎵€鏈夋搷浣溿€傜涓€闃舵涓嶆柊澧炵嫭绔嬫祻瑙堝櫒鏉冮檺妯″瀷锛屼笉瑕佹眰鏇挎崲 `X-MaClaw-Admin-Secret`銆傚鏋滈渶瑕佺櫥褰曢〉浣撻獙锛屽彲浠ョ敱 UI 鏀堕泦 admin secret 骞舵崲鍙栫煭鏈?admin UI session锛屼絾璇?session 浠嶅簲鐢卞悗绔槧灏勫埌鐜版湁 admin 鏉冮檺妯″瀷锛屼笉鎵╁ぇ鏉冮檺鑼冨洿銆?
蹇呴』淇濇寔锛?
- `MACLAW_ADMIN_SECRET` 浠嶆槸 admin 鎺у埗闈㈢殑鏍?secret锛屽惎鍔ㄦ椂蹇呴』寮烘牎楠屻€?- `MACLAW_TOKEN_SECRET` 浠嶇敤浜庣敤鎴?bearer token 绛惧彂鍜屾牎楠屻€?- Admin API 缁х画鎺ュ彈 `X-MaClaw-Admin-Secret`锛岀敤浜?CLI銆佸畨瑁呭櫒銆佽嚜鍔ㄥ寲鑴氭湰鍜?Admin UI 鍚庣璋冪敤銆?- 鐧诲綍澶辫触銆乤dmin secret 閿欒銆乥ootstrap token 閿欒閮借繘鍏ョ幇鏈?auth limiter銆?- 杩滅▼璁块棶缁х画閬靛畧 TLS/loopback 瑙勫垯锛氶潪 loopback 鏄庢枃 HTTP 榛樿鎷掔粷銆?- 鎵€鏈夐珮鍗?admin 鍐欐搷浣滃繀椤诲啓 audit锛屽苟璁板綍 actor銆乻ource IP銆乺equest id銆乺eason銆乥efore/after 鎽樿銆?- Admin UI 涓嶇洿鎺ヨ鍐欐枃浠躲€佷笉鐩存帴鎵ц鍛戒护銆佷笉缁曡繃 `/api/v1/admin/...`銆?
鍙€?Admin UI session锛?
```text
POST /api/v1/admin/auth/login
POST /api/v1/admin/auth/logout
GET  /api/v1/admin/auth/me
```

璇?session 鍙槸涓€灞?UI 渚垮埄灏佽锛宑ookie 蹇呴』浣跨敤 `HttpOnly`銆乣SameSite=Lax`锛孴LS 涓嬪惎鐢?`Secure`銆侫PI client 浠嶅彲鐩存帴浣跨敤 `X-MaClaw-Admin-Secret`銆?
### 4.5 鏉冮檺灞傜骇

寤鸿鏉冮檺灞傜骇锛?

- `viewer`锛氬彧璇绘煡鐪嬬姸鎬併€佹棩蹇椼€侀厤缃€佸憡璀︺€乻andbox 妫€娴嬬粨鏋溿€?
- `operator`锛氬彲鎵ц readiness refresh銆乴og rotate銆乻andbox smoke test銆丮CP stop/start銆乻napshot create/prune銆?
- `admin`锛氬彲淇敼鏈嶅姟绾ч厤缃€乻andbox 绛栫暐銆佸叏灞€ skill source銆乻cheduler銆乀LS銆乴ocal bash 绛栫暐銆?
- `owner`锛氬彲杞崲 admin secret銆佹仮澶?snapshot銆佹墽琛屽畨瑁呭懡浠ゃ€佷慨鏀瑰嵄闄╁紑鍏炽€佺鐞?admin 鐢ㄦ埛銆?

绗竴闃舵濡傛灉浠嶄繚鐣?`adminSecret`锛孉PI 璁捐涔熷簲棰勭暀 `required_role` 瀛楁锛屼究浜庡悗缁紨杩涖€?

### 4.6 棣栨璁剧疆鍚戝

棣栨 owner 鍒涘缓鍚庤繘鍏?setup wizard锛?

1. 妫€鏌?data root銆乴og root銆乻napshot root 鏉冮檺銆?
2. 璁剧疆鏈嶅姟鍚嶇О銆乸ublic base URL銆乀LS/insecure HTTP 绛栫暐銆?
3. 閰嶇疆 sandbox锛氭墽琛?detect锛岄€夋嫨 `auto|landlock|bwrap|nsjail|none`锛岀敓鎴?install plan銆?
4. 閰嶇疆 local execution policy锛歭ocal bash銆乴ocal MCP銆乻kill step 鏄惁鍏佽锛屼互鍙?strict fallback銆?
5. 閰嶇疆 snapshot retention 鍜?async job retention銆?
6. 灞曠ず security posture锛岃姹傜‘璁ら珮椋庨櫓椤广€?

wizard 鐨勬瘡涓€姝ラ兘搴斿彲璺宠繃锛屼絾璺宠繃浼氬湪 Overview 鍜?Security 涓繚鐣?warning銆?

## 5. 閰嶇疆杈圭晫

### 5.1 鐢ㄦ埛绾ч厤缃?

鐢ㄦ埛绾ч厤缃户缁€氳繃鐜版湁鎺ュ彛绠＄悊锛?

- `GET /api/v1/config/schema`
- `GET /api/v1/config`
- `PUT /api/v1/config`
- `POST /api/v1/config/validate`
- `POST /api/v1/config/test`

杩欎簺閰嶇疆灞炰簬 tenant/user锛屼緥濡?LLM provider銆丼SH host label銆丮CP server銆乻kill 鍜岀敤鎴峰伐浣滄祦鍋忓ソ銆?

### 5.2 鏈嶅姟绾ч厤缃?

鏈嶅姟绾ч厤缃睘浜?`maclawsrv` 杩涚▼锛屼笉缁戝畾鍗曚釜鐢ㄦ埛銆傚缓璁柊澧?`service_config.json`锛屽瓨鏀惧湪锛?

```text
MACLAW_DATA_ROOT/
  state/
    service_config.json
```

鐜鍙橀噺缁х画浣滀负鍚姩鏈?override銆傛帹鑽愯鍒欙細

```text
effective = defaults < service_config.json < environment overrides
```

瀵瑰繀椤诲惎鍔ㄥ墠鐢熸晥鐨勯厤缃紝渚嬪 listen addr銆乀LS 璇佷功銆乤dmin/token secret锛孉dmin Web 鍙互灞曠ず鍜屾牎楠岋紝浣嗕慨鏀瑰悗鏍囪涓?`restart_required=true`銆?

## 6. Admin Web 淇℃伅鏋舵瀯

### 6.1 Internationalization

Admin Web 闇€瑕佹敮鎸佷腑鏂囧拰鑻辨枃鍙岃瑷€锛屽苟鍏佽绠＄悊鍛橀殢鏃跺垏鎹€傝瑷€鍒囨崲鏄?Admin Web 鐨勫熀纭€鑳藉姏锛屼笉搴斿奖鍝?API 瀛楁鍚嶅拰瀛樺偍缁撴瀯銆?

#### 6.1.1 璇█鑼冨洿

绗竴闃舵鏀寔锛?

```text
zh-CN | en-US
```

UI 涓墍鏈夊彲瑙佹枃鏈兘蹇呴』璧?i18n key锛屽寘鎷細

- 瀵艰埅銆侀〉闈㈡爣棰樸€佹寜閽€佽〃鍗?label銆乸laceholder銆?
- 閿欒鎻愮ず銆佺‘璁ゅ脊绐椼€丏anger Zone 璀﹀憡銆?
- sandbox backend 鐘舵€佽鏄庛€乮nstall plan 鎻愮ず銆乻moke test 缁撴灉鏂囨銆?
- logs銆乻ecurity posture銆乺eadiness銆乨elete-check銆乺etire-plan 鐨勭敤鎴峰彲璇昏鏄庛€?
- setup wizard 鐨勬楠ゆ爣棰樺拰甯姪璇存槑銆?

API 杩斿洖鐨勬満鍣ㄥ瓧娈典繚鎸佽嫳鏂?snake_case锛屼笉鍋氭湰鍦板寲锛涢潰鍚?UI 灞曠ず鐨?`message`銆乣title`銆乣suggested_action` 鍙互鏀寔鏈湴鍖栥€?

#### 6.1.2 鍒囨崲鍜屾寔涔呭寲

璇█浼樺厛绾э細

```text
鐢ㄦ埛鎵嬪姩閫夋嫨 -> admin user preference -> cookie/localStorage -> Accept-Language -> service default -> zh-CN
```

寤鸿閰嶇疆瀛楁锛?

```json
{
  "admin_web": {
    "default_locale": "zh-CN",
    "enabled_locales": ["zh-CN", "en-US"],
    "locales": [
      {"locale": "zh-CN", "label": "简体中文"},
      {"locale": "en-US", "label": "English"}
    ]
  }
}
```

寤鸿 API锛?

```text
GET  /api/v1/admin/i18n/locales
GET  /api/v1/admin/i18n/messages?locale=zh-CN
# locale 接受 zh-CN/zh_CN/zh/zh-Hans/en-US/en_US/en 等别名；不支持的 locale 返回 400 和 enabled_locales。
PUT  /api/v1/admin/auth/preferences
```

`PUT /api/v1/admin/auth/preferences` 绀轰緥锛?

```json
{
  "locale": "en-US",
  "timezone": "Asia/Shanghai"
}
```

鏈櫥褰曠殑 bootstrap/setup 椤甸潰涔熷繀椤诲厑璁歌瑷€鍒囨崲锛屾鏃惰瑷€閫夋嫨淇濆瓨鍦?cookie/localStorage锛屽垵濮嬪寲 owner 鍚庡彲浠ュ啓鍏?admin user preference銆?

#### 6.1.3 鏂囨缁勭粐

鍓嶇寤鸿浣跨敤 namespace 绠＄悊鏂囨锛?

```text
common
nav
setup
service_config
sandbox
logs
security
tenants
users
snapshots
audit
scheduler
diagnostics
errors
```

绀轰緥锛?

```json
{
  "sandbox.switch.title": "Switch sandbox mode",
  "sandbox.switch.none_warning": "Sandbox protection will be disabled for new local executions.",
  "tenants.delete.confirmation": "Type DELETE {id} to confirm permanent deletion."
}
```

涓枃锛?

```json
{
  "sandbox.switch.title": "鍒囨崲娌欑妯″紡",
  "sandbox.switch.none_warning": "鏂扮殑鏈湴鎵ц灏嗕笉鍐嶅彈娌欑淇濇姢銆?,
  "tenants.delete.confirmation": "璇疯緭鍏?DELETE {id} 纭姘镐箙鍒犻櫎銆?
}
```

#### 6.1.4 鍚庣鏈湴鍖?

鍚庣閿欒鍝嶅簲寤鸿鍚屾椂杩斿洖绋冲畾閿欒鐮佸拰榛樿鑻辨枃 message锛?

```json
{
  "error": "sandbox smoke test failed",
  "code": "SANDBOX_SMOKE_TEST_FAILED",
  "message_key": "errors.sandbox_smoke_test_failed",
  "details": {}
}
```

鍓嶇浼樺厛鏍规嵁 `message_key` 娓叉煋鏈湴鍖栨枃妗堬紱娌℃湁瀵瑰簲 key 鏃跺洖閫€鍒?`error`銆?

瀵逛簬 audit event銆乴og event銆乻andbox event锛屽瓨鍌ㄥ眰淇濈暀绋冲畾 action/code锛屼緥濡?`sandbox.backend.switched`锛沀I 鍐嶆牴鎹?action/code 鏈湴鍖栨樉绀恒€?

#### 6.1.5 娴嬭瘯瑕佹眰

- 棣栧惎 setup 椤甸潰鍦ㄦ湭鐧诲綍鐘舵€佸彲浠ュ垏鎹腑鑻辨枃銆?
- 鐧诲綍鍚庤瑷€鍋忓ソ璺?session 淇濈暀銆?
- Danger Zone銆佸垹闄ょ‘璁ゃ€乻andbox `none` 璀﹀憡蹇呴』鏈変腑鑻辨枃鏂囨銆?
- 椤甸潰甯冨眬瑕侀獙璇佷腑鑻辨枃闀垮害宸紓锛岄伩鍏嶆寜閽€佽〃鏍煎垪銆佸脊绐楁枃瀛楁孩鍑恒€?

### 6.2 Overview

棣栭〉灞曠ず鏈嶅姟鎬昏锛?

- service version銆乥uild commit銆佸惎鍔ㄦ椂闂淬€佽繘绋?PID銆佽繍琛岀敤鎴枫€?
- data root銆乻tate path銆乴og path銆乻napshot path銆?
- readiness 鐘舵€併€?
- sandbox 褰撳墠妯″紡鍜屽仴搴风姸鎬併€?
- tenant/user/instance/session/run 璁℃暟銆?
- 鏈€杩戦敊璇棩蹇椼€佹渶杩戦珮鍗?audit銆佹渶杩?failed run銆?
- scheduler 鐘舵€併€?

澶嶇敤鐜版湁鎺ュ彛锛?

- `GET /health`
- `GET /livez`
- `GET /readyz`
- `GET /version`
- `GET /metrics`
- `GET /api/v1/admin/system/readiness`
- `GET /api/v1/admin/overview`
- `GET /api/v1/admin/dashboard`
- `GET /api/v1/admin/alerts`

### 6.3 Service Config

绠＄悊 `MaClawSrv` 杩涚▼绾ц缃細

- HTTP listen addr銆乀LS 鐘舵€併€乮nsecure HTTP 绛栫暐銆?
- data root銆乻napshot retention銆乤sync job retention銆?
- local bash 鎬诲紑鍏冲拰 scoped tenant/user銆?
- direct SSH 鎬诲紑鍏冲拰 file transfer 鎬诲紑鍏炽€?
- scheduler 寮€鍏炽€佸苟鍙戙€佽秴鏃躲€乯ob 淇濈暀銆?
- web search銆乲nowledge銆乻kill source 鍏ㄥ眬绛栫暐銆?
- debug flags锛屼緥濡?tool call debug銆乼race retention銆?
- secret 鐘舵€佸睍绀猴紝渚嬪宸查厤缃€侀暱搴﹀悎瑙勩€佹渶鍚庤疆鎹㈡椂闂达紝浣嗕笉鏄剧ず鏄庢枃銆?

寤鸿 API锛?

```text
GET  /api/v1/admin/service-config/schema
GET  /api/v1/admin/service-config
PUT  /api/v1/admin/service-config
POST /api/v1/admin/service-config/validate
POST /api/v1/admin/service-config/reload
GET  /api/v1/admin/tenants
POST /api/v1/admin/tenants
GET  /api/v1/admin/tenants/{tenantId}/users
POST /api/v1/admin/tenants/{tenantId}/users
GET  /api/v1/admin/tenants/{tenantId}/delete-check
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}/delete-check
GET  /api/v1/admin/tenants/{tenantId}/retire-plan
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}/retire-plan
GET  /api/v1/admin/service-config/effective
```

鍝嶅簲涓瘡涓瓧娈靛缓璁甫涓婏細

```json
{
  "value": "auto",
  "source": "file|env|default",
  "restart_required": false,
  "sensitive": false,
  "mutable_at_runtime": true
}
```

### 6.4 Sandbox

Sandbox 鏄?Admin Web 鐨勪竴绛夎兘鍔涳紝鐢ㄤ簬绠＄悊 `bash`銆佹湰鍦?MCP server銆乻kill step 绛夋湰鍦版墽琛屽叆鍙ｃ€?

#### 6.4.1 鍚庣妯″瀷

鎺ㄨ崘鏀寔锛?

- `none`锛氫笉鍚敤 sandbox銆?
- `auto`锛氳嚜鍔ㄦ娴嬪苟閫夋嫨鍙敤鍚庣銆?
- `landlock`锛氫娇鐢?`sandlock`銆乣landrun`銆乣rstrict`銆乣sandboxec` 绛?Landlock wrapper銆?
- `bwrap`锛氫娇鐢?bubblewrap 鍋氱敤鎴风骇 namespace sandbox銆?
- `nsjail`锛氶珮闅旂妯″紡锛岄€傚悎楂橀闄╂墽琛屻€?

鎺ㄨ崘榛樿浼樺厛绾э細

```text
landlock wrapper -> bwrap -> nsjail -> none
```

濡傛灉鐩爣鏇村亸鈥滆交瀹瑰櫒瑙嗗浘鈥濓紝鍙妸 `bwrap` 鏀惧湪 `landlock` 鍓嶉潰銆傝浼樺厛绾у簲鍙厤缃€?

#### 6.4.2 閰嶇疆瀛楁

绠＄悊鍛樺彲浠ュ湪 Admin Web 涓粺涓€鍒囨崲褰撳墠 sandbox 妯″瀷锛屾柟渚胯皟璇曘€佸吋瀹规€ч獙璇佹垨鎺掗櫎 sandbox 鐩稿叧鏁呴殰銆傚垏鎹㈠簲鏄湇鍔＄骇閰嶇疆锛屽奖鍝嶅悗缁柊鐨?`local_bash`銆乣local_mcp`銆乣skill_step` 鎵ц锛涘凡缁忓惎鍔ㄧ殑鏈湴 MCP 杩涚▼涓嶅簲琚潤榛樻浛鎹紝闇€瑕佹彁绀虹鐞嗗憳閲嶅惎瀵瑰簲 MCP server 鎴栨墽琛?`restart_affected=true`銆?

```json
{
  "sandbox": {
    "mode": "auto",
    "active_backend": "landlock",
    "previous_backend": "bwrap",
    "strict": false,
    "install_policy": "suggest",
    "preferred_backends": ["landlock", "bwrap", "nsjail"],
    "backend_bins": {
      "sandlock": "",
      "landrun": "",
      "rstrict": "",
      "sandboxec": "",
      "bwrap": "",
      "nsjail": ""
    },
    "profile": "default",
    "allow_network": false,
    "allowed_hosts": [],
    "workspace_write": true,
    "tmp_write": true,
    "home_read": false,
    "extra_read_paths": [],
    "extra_write_paths": [],
    "resource_limits": {
      "timeout_seconds": 240,
      "max_processes": 128,
      "memory_mb": 0,
      "cpu_seconds": 0,
      "output_bytes": 131072
    },
    "apply_to": {
      "local_bash": true,
      "local_mcp": true,
      "skill_steps": true
    }
  }
}
```

#### 6.4.3 妫€娴嬭兘鍔?

Admin Web 搴斿睍绀猴細

- OS銆乲ernel version銆乤rchitecture銆?
- user namespace 鏄惁鍙敤銆?
- Landlock ABI 鏄惁鍙敤銆?
- `bwrap`銆乣nsjail`銆乣sandlock`銆乣landrun`銆乣rstrict`銆乣sandboxec` 鏄惁瀛樺湪銆?
- 姣忎釜鍚庣鐨?smoke test 鏄惁閫氳繃銆?
- 褰撳墠 `effective_backend` 鍜?fallback 鍘熷洜銆?
- 鍝簺鎵ц鍏ュ彛宸插彈淇濇姢銆?

寤鸿 API锛?

```text
GET  /api/v1/admin/sandbox/status
POST /api/v1/admin/sandbox/detect
POST /api/v1/admin/sandbox/smoke-test
POST /api/v1/admin/sandbox/diagnose
GET  /api/v1/admin/sandbox/reports
GET  /api/v1/admin/sandbox/reports/{reportId}
POST /api/v1/admin/sandbox/switch
POST /api/v1/admin/sandbox/rollback
GET  /api/v1/admin/sandbox/profiles
PUT  /api/v1/admin/sandbox/profiles/{profileName}
POST /api/v1/admin/sandbox/profiles/{profileName}/validate
GET  /api/v1/admin/sandbox/install-plan
POST /api/v1/admin/sandbox/install
GET  /api/v1/admin/sandbox/events
```

`install-plan` 鍙敓鎴愬缓璁懡浠わ紝渚嬪锛?

```json
{
  "platform": "debian",
  "commands": [
    "sudo apt-get update",
    "sudo apt-get install -y bubblewrap"
  ],
  "requires_privilege": true,
  "will_execute": false
}
```

`install` 蹇呴』瑕佹眰鏄惧紡纭锛?

```json
{
  "backend": "bwrap",
  "confirm": true,
  "mode": "run|print_only"
}
```

榛樿 `install_policy=suggest` 鏃讹紝Admin Web 鍙睍绀哄懡浠わ紝涓嶆墽琛屻€?

#### 6.4.4 娌欑鍋ュ悍妫€娴嬫姤鍛?

娌欑鍚敤鍚庯紝Admin Web 蹇呴』鎻愪緵涓€閿娴嬪姛鑳斤紝鐢ㄤ簬纭褰撳墠 sandbox 鏄惁鐪熸鐢熸晥锛屽苟杈撳嚭绠＄悊鍛樺彲璇绘姤鍛娿€傝妫€娴嬩笉浠呮鏌?binary 鏄惁瀛樺湪锛岃繕瑕侀獙璇侀殧绂荤瓥鐣ユ槸鍚︽寜棰勬湡宸ヤ綔銆?

寤鸿鍏ュ彛锛?

```text
POST /api/v1/admin/sandbox/diagnose
GET  /api/v1/admin/sandbox/reports
GET  /api/v1/admin/sandbox/reports/{reportId}
```

妫€娴嬪簲瑕嗙洊锛?

- 褰撳墠妯″紡銆乪ffective backend銆乸rofile銆乻trict/fallback 鐘舵€併€?
- 鍚庣 binary 璺緞鍜岀増鏈€?
- OS銆乲ernel銆乽ser namespace銆丩andlock ABI銆乻eccomp銆乧group 鑳藉姏銆?
- smoke test锛氭墽琛?`/bin/true` 鎴栫瓑浠峰懡浠ゃ€?
- workspace 璇诲啓锛氬厑璁稿啓鍏?workspace 鐨勪复鏃舵祴璇曟枃浠讹紝骞舵竻鐞嗐€?
- forbidden path锛氬皾璇曡鍙栨湭鎺堟潈璺緞锛屼緥濡?`/etc/shadow`锛屾湡鏈涘け璐ャ€?
- tmp 鍐欏叆锛氭寜 profile 楠岃瘉 `/tmp` 鎴栫鏈?tmpfs銆?
- network锛氭寜 `allow_network` 楠岃瘉鏂綉鎴栧厑璁歌闂紱濡傛灉閰嶇疆浜?allowed hosts锛岄獙璇佸厑璁稿拰鎷掔粷鍚勪竴涓牱渚嬨€?
- process isolation锛氶獙璇佸彲瑙?`/proc` 鑼冨洿銆乸id namespace 琛屼负锛宐wrap/nsjail 妯″紡涓嬪繀椤绘鏌ャ€?
- env sanitization锛氶獙璇佹晱鎰熺幆澧冨彉閲忔槸鍚﹁娓呯悊鎴栨寜绛栫暐浼犻€掋€?
- MCP stdio compatibility锛氬彲閫夊惎鍔ㄤ竴涓?echo MCP probe锛岀‘璁?stdin/stdout 娌¤ wrapper 鐮村潖銆?
- resource limits锛氬彲閫夐獙璇?timeout銆佽緭鍑烘埅鏂€佽繘绋嬫暟闄愬埗銆?

璇锋眰绀轰緥锛?

```json
{
  "profile": "default",
  "include_network_tests": false,
  "include_mcp_stdio_test": true,
  "include_resource_limit_tests": false,
  "write_report": true
}
```

鎶ュ憡缁撴瀯锛?

```json
{
  "report_id": "sandbox_report_xxx",
  "generated_at": "2026-05-17T12:00:00Z",
  "status": "pass|warn|fail",
  "summary": "Sandbox is active and core isolation checks passed.",
  "mode": "auto",
  "effective_backend": "bwrap",
  "profile": "default",
  "strict": false,
  "checks": [
    {
      "id": "forbidden_path_read",
      "title": "Forbidden path is blocked",
      "status": "pass",
      "expected": "read denied",
      "actual": "permission denied",
      "severity": "critical",
      "duration_ms": 18
    }
  ],
  "warnings": [
    "Network is not isolated because allow_network=true."
  ],
  "recommendations": [
    "Run MCP stdio test before enabling sandbox for all local MCP servers."
  ],
  "raw": {
    "redacted_stdout": "...",
    "redacted_stderr": "..."
  }
}
```

鐘舵€佸垽瀹氾細

- `pass`锛氭牳蹇冮殧绂绘祴璇曢€氳繃锛屽綋鍓嶉厤缃彲鐢ㄤ簬鍙椾繚鎶ゆ墽琛屻€?
- `warn`锛氭矙绠卞彲杩愯锛屼絾瀛樺湪寮遍殧绂汇€佽烦杩囨祴璇曘€乫allback銆佺綉缁滄湭闅旂绛夐闄┿€?
- `fail`锛氬悗绔笉鍙敤銆乻moke test 澶辫触銆佺姝㈣矾寰勫彲璇汇€乸rofile 鏃犳硶鍔犺浇銆乻tdio 琚牬鍧忕瓑銆?

Admin Web 灞曠ず瑕佹眰锛?

- Overview 鍜?Sandbox 椤甸潰鏄剧ず鏈€杩戜竴娆℃娴嬬姸鎬併€佹椂闂村拰鎶ュ憡閾炬帴銆?
- 姣忎釜 check 鐢?`pass/warn/fail/skipped` 灞曠ず锛屽苟缁欏嚭鈥滄湡鏈?瀹為檯/寤鸿鈥濄€?
- `fail` 鏃舵彁渚涚洿鎺ユ搷浣滐細鍒囨崲妯″紡銆乺ollback銆佹煡鐪嬫棩蹇椼€佺敓鎴?install plan銆?
- 鎶ュ憡鍙笅杞?JSON锛屼絾榛樿鑴辨晱 stdout/stderr銆佽矾寰勫拰鐜鍙橀噺銆?
- 妫€娴嬫姤鍛婂啓鍏?audit锛歚sandbox.diagnose.started`銆乣sandbox.diagnose.completed`銆乣sandbox.diagnose.failed`銆?

妫€娴嬮鐜囧缓璁細

- 鎵嬪姩鍒囨崲 sandbox 鍚庤嚜鍔ㄨ窇涓€娆¤交閲?diagnose銆?
- 鏈嶅姟鍚姩鍚庡鏋?sandbox enabled锛屽彲寮傛璺戜竴娆¤交閲?diagnose 骞剁紦瀛樼粨鏋溿€?
- 绠＄悊鍛樺彲鎵嬪姩杩愯瀹屾暣 diagnose銆?
- 鎶ュ憡淇濈暀鏈€杩?20 浠斤紝鎴栨寜 `service_config.sandbox.report_retention` 鎺у埗銆?

#### 6.4.5 缁熶竴鍒囨崲鍜屾晠闅滄帓闄?

Sandbox 椤甸潰闇€瑕佹彁渚涘叏灞€妯″紡鍒囨崲鍣細

```text
Auto | Landlock | bwrap | nsjail | None
```

鍒囨崲娴佺▼锛?

1. 绠＄悊鍛橀€夋嫨鐩爣妯″紡銆?
2. 鏈嶅姟鎵ц鐩爣鍚庣 detect 鍜?smoke test銆?
3. 灞曠ず褰卞搷鑼冨洿锛歭ocal bash銆乴ocal MCP銆乻kill step锛屼互鍙婇渶瑕侀噸鍚殑鏈湴 MCP server 鏁伴噺銆?
4. 绠＄悊鍛樼‘璁ゅ悗鍐欏叆 `service_config.json`銆?
5. 鏂版墽琛岃姹傜珛鍗充娇鐢ㄦ柊妯″紡锛涘凡杩愯鐨勬湰鍦?MCP server 缁存寔鏃фā寮忕洿鍒伴噸鍚€?
6. 鍐欏叆 audit 鍜?sandbox event銆?

寤鸿璇锋眰锛?

```json
{
  "mode": "bwrap",
  "profile": "default",
  "reason": "debug mcp startup failure",
  "run_smoke_test": true,
  "restart_affected_mcp": false,
  "fallback_if_unavailable": false,
  "confirm": true
}
```

鍝嶅簲锛?

```json
{
  "previous_mode": "landlock",
  "current_mode": "bwrap",
  "effective_backend": "bwrap",
  "restart_required": false,
  "affected": {
    "local_mcp_running": 3,
    "local_mcp_needs_restart": true
  },
  "smoke_test": {
    "status": "passed",
    "duration_ms": 42
  },
  "audit_event_id": "audit_xxx"
}
```

`none` 妯″紡鏄嵄闄╄皟璇曟ā寮忥細

- UI 蹇呴』鏄剧ず绾㈣壊璀﹀憡锛岃鏄庢湰鍦版墽琛屽皢涓嶅啀琚?sandbox 淇濇姢銆?
- 闇€瑕?`admin` 鎴栨洿楂樻潈闄愶紱濡傛灉 `strict=true`锛岄渶瑕?`owner` 鏉冮檺鎴栧厛鍏抽棴 strict銆?
- 鍙互瑕佹眰濉啓 `reason`銆?
- 鍙互鏀寔 `expires_at` 鎴?`ttl_minutes`锛屽埌鏈熻嚜鍔ㄦ仮澶嶄笂涓€妯″紡銆?

寤鸿 rollback 琛屼负锛?

```text
POST /api/v1/admin/sandbox/rollback
```

鐢ㄤ簬蹇€熸仮澶嶄笂涓€鍙敤妯″紡銆傛瘡娆?switch 搴斾繚瀛?`previous_backend`銆乣previous_profile`銆佸垏鎹汉鍜屽垏鎹㈡椂闂淬€?

#### 6.4.6 Sandbox 浜嬩欢鍜屽璁?

鎵€鏈?sandbox 鐩稿叧鍔ㄤ綔閮藉簲鍐欏叆 audit锛?

- `sandbox.detected`
- `sandbox.config.updated`
- `sandbox.smoke_test.succeeded`
- `sandbox.smoke_test.failed`
- `sandbox.diagnose.started`
- `sandbox.diagnose.completed`
- `sandbox.diagnose.failed`
- `sandbox.install_plan.generated`
- `sandbox.install.started`
- `sandbox.install.failed`
- `sandbox.backend.selected`
- `sandbox.backend.switched`
- `sandbox.backend.rollback`
- `sandbox.execution.blocked`

鎵ц鏃惰繕搴旇褰曡交閲忎簨浠讹紝涓嶄繚瀛樺畬鏁村懡浠ゆ晱鎰熷弬鏁帮細

```json
{
  "backend": "bwrap",
  "entrypoint": "local_mcp|local_bash|skill_step",
  "profile": "default",
  "workspace": "/srv/workspaces/example",
  "allowed_network": false,
  "exit_code": 0,
  "duration_ms": 238
}
```

### 6.5 Logs

鏃ュ織鏌ョ湅鏄?Admin Web 鐨勬牳蹇冭繍缁磋兘鍔涖€?

寤鸿鏃ュ織鍒嗙被锛?

- `service`锛歮aclawsrv 鏍囧噯杩愯鏃ュ織銆?
- `error`锛歴tderr 鎴?error-level 鏃ュ織銆?
- `access`锛欻TTP access 鏃ュ織锛岀涓€闃舵鍙€夈€?
- `audit`锛歛dmin/user 璧勬簮鎿嶄綔瀹¤锛屽鐢ㄧ幇鏈?audit events銆?
- `sandbox`锛歴andbox 妫€娴嬨€侀€夋嫨銆佹墽琛屽拰闃绘柇浜嬩欢銆?
- `security_risks`锛氭敮鎸佸寘鍐呭簲鍖呭惈椋庨櫓鎽樿銆乣generated_at`銆佹爣鍑嗗寲 `filters`銆乻everity/kind counts 鍜屾渶杩戦闄╀簨浠讹紱`filters.source` 鍥哄畾涓?`sandbox_support_bundle`锛屾柟渚跨鐞嗗憳鎶婃矙绠辨晠闅滃拰瀹夊叏濮挎€佷竴璧峰彂閫佺粰鏀寔鏂广€?
- 鏀寔鍖呭繀椤婚伩鍏嶆毚闇插畬鏁存湰鏈鸿矾寰勶紱渚嬪 data root 鍙繑鍥?`data_root_name`銆乣data_root_redacted=true` 鍜?`redactions` 鍒楄〃锛屼笉杩斿洖瀹屾暣 `data_root`銆?
- `scheduler`锛氬畾鏃朵换鍔℃棩蹇椼€?
- `mcp`锛氭湰鍦?MCP server 鍚仠銆乭ealth check銆乼ools/list 閿欒銆?
- `agent`锛歳un 绾ч敊璇憳瑕侊紝涓嶅寘鍚敤鎴锋晱鎰熷唴瀹广€?

寤鸿 API锛?

```text
GET  /api/v1/admin/logs/sources
GET  /api/v1/admin/logs/{source}
GET  /api/v1/admin/logs/{source}/tail
POST /api/v1/admin/logs/{source}/rotate
POST /api/v1/admin/logs/search
GET  /api/v1/admin/logs/errors/recent
```

鏌ヨ鍙傛暟锛?

```text
level=debug|info|warn|error
since=2026-05-17T00:00:00Z
until=2026-05-17T01:00:00Z
limit=200
cursor=...
q=...
follow=true
```

瀹夊叏瑕佹眰锛?

- 鏃ュ織璇诲彇蹇呴』闄愬埗鍦ㄥ彈淇′换 log root 涓嬶紝绂佹浠绘剰璺緞璇诲彇銆?
- 榛樿瀵?secrets銆乥earer token銆丄PI key銆丄uthorization header 鍋氳劚鏁忋€?
- UI 榛樿灞曠ず鏈€杩?200 琛岋紝涓嬭浇瀹屾暣鏃ュ織闇€瑕佹洿楂樻潈闄愩€?
- tail/follow 搴旀湁杩炴帴鏁板拰鏃堕棿闄愬埗銆?

### 6.6 Runtime Controls

鏈嶅姟杩愯鎺у埗鍖呮嫭锛?

- 鏌ョ湅杩涚▼鐘舵€併€乬oroutine 鏁般€佸唴瀛樸€佺鐩樸€佹墦寮€鏂囦欢鏁般€?
- graceful shutdown 鎴?restart 璇锋眰銆?
- reload runtime config銆?
- 娓呯悊杩囨湡 async jobs銆?
- 娓呯悊涓存椂鏂囦欢銆?
- 鎵ц readiness refresh銆?

寤鸿 API锛?

```text
GET  /api/v1/admin/runtime/status
POST /api/v1/admin/runtime/reload
POST /api/v1/admin/runtime/shutdown
POST /api/v1/admin/runtime/restart
POST /api/v1/admin/runtime/cleanup
```

`shutdown` 鍜?`restart` 榛樿鍙厛鍙璁紝涓嶅疄鐜帮紱濡傛灉瀹炵幇锛屽簲瑕佹眰浜屾纭鍜?owner 鏉冮檺銆?

### 6.7 Security

瀹夊叏椤靛睍绀哄拰绠＄悊锛?

- admin secret 鐘舵€佸拰杞崲鎻愰啋銆?
- token secret 鐘舵€佸拰杞崲鎻愰啋銆?
- TLS 鐘舵€併€佽瘉涔︽湁鏁堟湡銆佽瘉涔﹂摼妫€鏌ャ€?
- insecure HTTP 椋庨櫓鎻愮ず銆?
- local bash 鏄惁鍚敤銆佺粦瀹?tenant/user 鏄惁瀹屾暣銆?
- direct SSH 鏄惁鍚敤銆乫ile transfer 鏄惁鍚敤銆?
- sandbox 鏄惁鍚敤銆?
- 鏈€杩戦珮鍗卞璁′簨浠躲€?
- auth limiter 鐘舵€併€?

寤鸿 API锛?

```text
GET  /api/v1/admin/security/posture
POST /api/v1/admin/security/rotate-admin-secret
POST /api/v1/admin/security/rotate-token-secret
POST /api/v1/admin/security/check-tls
GET  /api/v1/admin/security/risk-events
```

secret 杞崲鍙互绗竴闃舵浠呰緭鍑烘搷浣滆鍒掞紝鍥犱负鐜鍙橀噺鎴?service manager 寰€寰€鏄疄闄?secret 鏉ユ簮銆?

#### 6.7.1 椋庨櫓浜嬩欢绛涢€変笌鎶ュ憡

椋庨櫓浜嬩欢椤电敤浜庢妸 sandbox銆佸畨鍏ㄥЭ鎬佸拰楂樺嵄 admin 鎿嶄綔涓叉垚鍙帓鏌ユ椂闂寸嚎銆俇I 蹇呴』鍙€氳繃 Admin API 鏌ヨ锛屼笉鐩存帴璇?audit 鏂囦欢銆傚缓璁涓€闃舵钀藉湴浠ヤ笅鑳藉姏锛?
```text
GET /api/v1/admin/security/summary?since=...&until=...
GET /api/v1/admin/security/risk-events?severity=high&kind=auth_failed&since=...&until=...&limit=50
```

绛涢€夎鍒欙細

- `severity` 鍙帴鍙?`high|medium|low`銆?- `kind` 浣跨敤绋冲畾椋庨櫓绫诲瀷锛屼緥濡?`sandbox_disabled`銆乣sandbox_failed`銆乣auth_failed`銆乣insecure_http`銆?- `since` 鍜?`until` 閮芥槸 RFC3339锛涗袱鑰呭悓鏃舵彁渚涙椂蹇呴』婊¤冻 `since <= until`銆?- UI 鎻愪緵 `1h`銆乣24h`銆乣7d` 鍜?`All` 蹇嵎鏃堕棿鑼冨洿锛屾柟渚夸簨鏁呭洖鏀俱€?- API 鍝嶅簲蹇呴』鍥炴樉 `generated_at` 鍜屾爣鍑嗗寲鍚庣殑 `filters`锛屼究浜?UI銆佹敮鎸佸寘鍜屽璁″鐩樺鐜板悓涓€浠芥姤鍛娿€?
- UI 灞曠ず `generated_at`銆乻everity counts銆乲ind counts銆佸綋鍓嶇瓫閫夋潯浠跺拰 total锛涚偣鍑?count chip 鍙弽鍚戝～鍏呯瓫閫夋潯浠躲€?
椋庨櫓鏉ユ簮鑷冲皯鍖呮嫭锛?
- 褰撳墠 posture锛歴andbox disabled銆乻andbox 闈?strict銆乮nsecure HTTP銆?- auth limiter锛歛dmin secret 閿欒銆佺櫥褰曞け璐ャ€乺ate limit銆?- sandbox锛歞etect/install/diagnose/switch/rollback 澶辫触鎴栭檷绾с€?- 澶囦唤鎭㈠锛氬鍑?secrets銆侀潪 dry-run import銆侀潪 dry-run restore銆佸惈 secrets snapshot銆?
#### 6.7.2 楂橀闄╂搷浣滅‘璁?
Admin Web 蹇呴』瀵圭牬鍧忔€ф垨鏁忔劅鎿嶄綔鍋氭樉寮忕‘璁わ紝涓旂‘璁ょ姸鎬佸繀椤讳紶缁?Admin API锛岀敱鍚庣鍐嶆鏍￠獙锛屼笉鑳藉彧渚濊禆鍓嶇寮圭獥銆?
绗竴闃舵寤鸿鍥哄畾纭鐭锛?
- 瀵煎嚭 secrets锛歚EXPORT SECRETS`锛孉PI 闇€瑕?`confirm=true`銆?- 闈?dry-run import锛歚IMPORT STATE`锛孉PI 闇€瑕?`confirm=true`銆?- 闈?dry-run restore锛歚RESTORE SNAPSHOT`锛孉PI 闇€瑕?`confirm=true`銆?- 鍒涘缓鍚?secrets snapshot锛歚SNAPSHOT SECRETS`锛孉PI 闇€瑕?`confirm=true`銆?- 鎵ц sandbox install锛歚INSTALL SANDBOX`锛孉PI 闇€瑕?`confirm=true`銆?- 鍒囨崲鍒?`none` sandbox锛歚DISABLE SANDBOX`锛孉PI 闇€瑕?`confirm=true`銆?
杩欎簺鎿嶄綔閮藉繀椤诲啓 audit锛屽苟杩涘叆 security risk event 瑙嗗浘锛屾柟渚夸簨鍚庡璁°€?
### 6.8 Backup and Migration

澶嶇敤鐜版湁 export/import/snapshot 鑳藉姏锛屽苟鍦?Admin Web 鍋氭垚鎿嶄綔鍚戝锛?

- 鍒涘缓鍏ㄩ噺 snapshot銆?
- 鍒涘缓 tenant/user scoped snapshot銆?
- 鍒楄〃銆佷笅杞姐€佸垹闄ゃ€佹仮澶嶃€?
- prune retention銆?
- import precheck銆?
- restore dry-run銆?

澶嶇敤鎺ュ彛锛?

- `GET /api/v1/admin/export`
- `POST /api/v1/admin/import`
- `GET /api/v1/admin/snapshots`
- `POST /api/v1/admin/snapshots`
- `POST /api/v1/admin/snapshots/prune`
- `GET /api/v1/admin/snapshots/{snapshotId}`
- `POST /api/v1/admin/snapshots/{snapshotId}/restore`
- `DELETE /api/v1/admin/snapshots/{snapshotId}`

### 6.9 Tenants & Users

Admin Web 蹇呴』鎻愪緵澶氱鎴风鐞嗚鍥俱€傜鐞嗗憳鍙互鏌ョ湅鍏ㄩ儴绉熸埛銆佹煡鐪嬫瘡涓鎴蜂笅鐨勭敤鎴凤紝骞跺绉熸埛鎴栫敤鎴锋墽琛屾柊澧炪€佹殏鍋溿€佹仮澶嶃€佸垹闄ょ瓑鐢熷懡鍛ㄦ湡鎿嶄綔銆?

#### 6.9.1 绉熸埛鍒楄〃鍜岃鎯?

绉熸埛鍒楄〃灞曠ず锛?

- tenant id銆乶ame銆乻tatus銆乧reated_at銆乽pdated_at銆?
- 鐢ㄦ埛鏁般€乧redential 鏁般€乮nstance 鏁般€乻ession/run/message 缁熻銆?
- 鏁版嵁鍗犵敤浼扮畻锛歝onfig銆乵emory銆乻kills銆丮CP銆乲nowledge銆乺ecords銆乻napshots銆?
- 鏈€杩戞椿鍔ㄦ椂闂淬€佹渶杩戝け璐?run銆佹渶杩戦珮鍗?audit銆?
- delete protection / managed 鏍囪銆?

澶嶇敤鎴栨墿灞曠幇鏈?API锛?

```text
GET  /api/v1/admin/tenants
POST /api/v1/admin/tenants
GET  /api/v1/admin/tenants/{tenantId}
PATCH /api/v1/admin/tenants/{tenantId}
GET  /api/v1/admin/tenants/{tenantId}/summary
GET  /api/v1/admin/tenants/{tenantId}/users
GET  /api/v1/admin/tenants/{tenantId}/delete-check
GET  /api/v1/admin/tenants/{tenantId}/retire-plan
DELETE /api/v1/admin/tenants/{tenantId}
```

寤鸿鏂板鏄惧紡鐘舵€佸姩浣滐紝閬垮厤浠呴潬 PATCH 琛ㄨ揪楂橀闄╃敓鍛藉懆鏈燂細

```text
POST /api/v1/admin/tenants/{tenantId}/pause
POST /api/v1/admin/tenants/{tenantId}/resume
POST /api/v1/admin/tenants/{tenantId}/archive
POST /api/v1/admin/tenants/{tenantId}/restore
```

#### 6.9.2 鐢ㄦ埛鍒楄〃鍜岃鎯?

鍦ㄧ鎴疯鎯呬笅灞曠ず鐢ㄦ埛鍒楄〃銆備篃淇濈暀璺ㄧ鎴风敤鎴锋悳绱€?

鐢ㄦ埛鍒楄〃灞曠ず锛?

- user id銆乪mail銆乨isplay name銆乻tatus銆乧reated_at銆乽pdated_at銆?
- credential 鏁般€乮nstance 鏁般€乻ession/run/message 缁熻銆?
- config 鏄惁瀹屾暣銆丩LM provider 鏄惁鍙敤銆佹渶鍚庝竴娆?ready 鐘舵€併€?
- skill/MCP/knowledge/record 鏁版嵁澶у皬浼扮畻銆?
- 鏈€杩戞椿鍔ㄦ椂闂淬€佹渶杩?failed run銆佹渶杩?audit銆?

澶嶇敤鎴栨墿灞曠幇鏈?API锛?

```text
GET  /api/v1/admin/users
GET  /api/v1/admin/tenants/{tenantId}/users
POST /api/v1/admin/tenants/{tenantId}/users
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}
PATCH /api/v1/admin/tenants/{tenantId}/users/{userId}
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}/delete-check
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}/retire-plan
DELETE /api/v1/admin/tenants/{tenantId}/users/{userId}
```

寤鸿鏂板鏄惧紡鐘舵€佸姩浣滐細

```text
POST /api/v1/admin/tenants/{tenantId}/users/{userId}/pause
POST /api/v1/admin/tenants/{tenantId}/users/{userId}/resume
POST /api/v1/admin/tenants/{tenantId}/users/{userId}/archive
POST /api/v1/admin/tenants/{tenantId}/users/{userId}/restore
```

#### 6.9.3 鏆傚仠璇箟

鏆傚仠绉熸埛锛?

- 璇ョ鎴蜂笅鎵€鏈夌敤鎴锋棤娉曠鍙戞柊 bearer token銆?
- 宸叉湁 token 鍙互绔嬪嵆澶辨晥锛屾垨鎸夐厤缃繘鍏ュ闄愭湡銆?
- 鏂?run 涓嶅厑璁稿垱寤猴紝宸叉湁 run 鍙€夋嫨 cancel 鎴?allow-to-finish銆?
- Admin 浠嶅彲鏌ョ湅鍜屽鍑鸿绉熸埛鏁版嵁銆?

鏆傚仠鐢ㄦ埛锛?

- 璇ョ敤鎴锋棤娉曠鍙戞柊 bearer token銆?
- 鏂?instance/session/run 鍐欐搷浣滆鎷掔粷銆?
- Admin 浠嶅彲鏌ョ湅銆佸鍑恒€佹仮澶嶆垨鍒犻櫎璇ョ敤鎴锋暟鎹€?

寤鸿鏆傚仠璇锋眰浣擄細

```json
{
  "reason": "billing overdue",
  "revoke_active_tokens": true,
  "cancel_running_runs": false
}
```

#### 6.9.4 鍒犻櫎鍜屾暟鎹竻鐞?

鍒犻櫎绉熸埛鎴栫敤鎴峰繀椤绘槸寮烘彁绀恒€佸彲棰勬銆佸彲 dry-run 鐨勫嵄闄╂搷浣溿€傚垹闄ゅ悗闇€瑕佹竻鐞嗗搴旀暟鎹洰褰曞拰绱㈠紩璁板綍銆?

鍒犻櫎鐢ㄦ埛搴旀竻鐞嗭細

- user metadata銆乧redentials銆乧onfig銆?
- instances銆乻essions銆乵essages銆乺uns銆乺un events銆?
- memory銆乻kills銆丮CP config/runtime state銆乲nowledge sources/index銆乻tructured records銆?
- async jobs銆乽ploads銆乼emporary files銆?
- user scoped snapshots锛岄粯璁や繚鐣欐垨鍒犻櫎搴旂敱璇锋眰鍙傛暟鍐冲畾銆?
- audit events 榛樿淇濈暀鑴辨晱鎽樿锛屼笉寤鸿鐗╃悊鍒犻櫎锛涘彲閫氳繃 `purge_audit=true` 鏄惧紡娓呯悊銆?

鍒犻櫎绉熸埛搴旀竻鐞嗭細

- tenant metadata銆?
- 璇ョ鎴蜂笅鎵€鏈?users 鍙婂叾涓婅堪鍏ㄩ儴鏁版嵁銆?
- tenant scoped knowledge銆乻kill-source override銆乸olicy override銆乻napshots銆?
- async jobs銆乽ploads銆乼emporary files銆?
- audit events 榛樿淇濈暀鑴辨晱鎽樿銆?

鍒犻櫎璇锋眰蹇呴』鍏堣皟鐢?`delete-check` 鎴?`retire-plan`锛孶I 鏄剧ず褰卞搷鑼冨洿锛?

```json
{
  "resource": "tenant/tenant_xxx",
  "can_delete": true,
  "blocked_by": [],
  "warnings": ["2 running runs will be cancelled"],
  "counts": {
    "users": 12,
    "credentials": 18,
    "instances": 42,
    "sessions": 310,
    "messages": 12048,
    "skills": 37,
    "mcp_servers": 9,
    "knowledge_sources": 86,
    "snapshots": 4
  },
  "estimated_bytes": 2147483648
}
```

鍒犻櫎璇锋眰浣撳缓璁細

```json
{
  "confirm": true,
  "confirmation_text": "DELETE tenant_xxx",
  "dry_run": false,
  "create_snapshot_before_delete": true,
  "delete_snapshots": false,
  "purge_audit": false,
  "cancel_running_runs": true,
  "reason": "tenant offboarding"
}
```

瀹夊叏瑕佹眰锛?

- UI 蹇呴』鏄剧ず绾㈣壊鍗遍櫓鍖哄煙銆佸奖鍝嶆暟閲忋€佹暟鎹笉鍙仮澶嶆彁绀恒€?
- 蹇呴』杈撳叆纭鐭锛屼緥濡?`DELETE tenant_xxx` 鎴?`DELETE user_xxx`銆?
- 榛樿寤鸿鍏堝垱寤?snapshot銆?
- 鍒犻櫎搴旇繘鍏?async job锛岄伩鍏?HTTP 璇锋眰闀挎椂闂撮樆濉炪€?
- 鍒犻櫎杩囩▼涓簲鍒嗛樁娈佃褰曡繘搴︼紝鍙仮澶嶅湴鏍囪 `deleting` 鐘舵€併€?
- 鍒犻櫎瀹屾垚鍚庡啓鍏?audit锛歚tenant.deleted`銆乣user.deleted`銆乣tenant.data_purged`銆乣user.data_purged`銆?
- 濡傛灉閮ㄥ垎娓呯悊澶辫触锛岃祫婧愯繘鍏?`delete_failed` 鐘舵€侊紝骞跺睍绀哄墿浣欒矾寰勫拰琛ユ晳鍔ㄤ綔銆?

#### 6.9.5 UI 浜や簰

`Tenants & Users` 椤甸潰寤鸿缁撴瀯锛?

```text
Tenants list -> Tenant detail -> Users tab -> User detail
```

绉熸埛璇︽儏 tabs锛?

```text
Overview | Users | Credentials | Instances | Usage | Data | Audit | Danger Zone
```

鐢ㄦ埛璇︽儏 tabs锛?

```text
Overview | Credentials | Instances | Config Status | Skills & MCP | Knowledge | Data | Audit | Danger Zone
```

`Danger Zone` 鍖呭惈鏆傚仠銆佹仮澶嶃€佸綊妗ｃ€佸垹闄ゃ€傚垹闄ゆ寜閽粯璁ょ鐢紝蹇呴』鍏堝畬鎴?delete-check 骞跺睍寮€褰卞搷娓呭崟銆?

### 6.10 Global Skill and MCP Governance

Admin Web 搴旇兘绠＄悊闈炵敤鎴峰疄渚嬬鏈夌殑 skill/MCP 绛栫暐锛?

- 鍏ㄥ眬 skill source銆?
- tenant/user skill source override銆?
- capability market policy銆?
- 绂佺敤楂樺嵄 skill action銆?
- 鏈湴 MCP server 榛樿鏄惁鍏佽銆?
- MCP 鍚姩鏄惁蹇呴』璧?sandbox銆?
- MCP server 鍏ㄥ眬 allowlist/denylist銆?

鐜版湁 skill source admin API 鍙洿鎺ュ鐢ㄣ€?

寤鸿鏂板绛栫暐 API锛?

```text
GET  /api/v1/admin/execution-policy
PUT  /api/v1/admin/execution-policy
POST /api/v1/admin/execution-policy/validate
```

### 6.11 Scheduler and Jobs

绠＄悊 scheduler 涓?async jobs锛?

- scheduler enabled銆乼ick interval銆亀orker 鏁般€?
- 褰撳墠 scheduled tasks銆?
- 涓婃鎵ц鏃堕棿銆佷笅娆℃墽琛屾椂闂淬€佸け璐ユ鏁般€?
- async jobs 鍒楄〃銆佸彇娑堛€佸垹闄ゃ€佹竻鐞嗐€?

寤鸿 API锛?

```text
GET  /api/v1/admin/scheduler/status
GET  /api/v1/admin/scheduler/tasks
POST /api/v1/admin/scheduler/tasks/{taskId}/pause
POST /api/v1/admin/scheduler/tasks/{taskId}/resume
POST /api/v1/admin/scheduler/tasks/{taskId}/run-now
GET  /api/v1/admin/jobs
POST /api/v1/admin/jobs/cleanup
```

鐢ㄦ埛 bearer token 鐨?`/api/v1/jobs` 缁х画鍙湅褰撳墠鐢ㄦ埛 jobs锛沘dmin jobs 鍙互璺?tenant/user 鏌ョ湅銆?

## 7. 椤甸潰缁撴瀯

寤鸿 Admin Web 宸︿晶瀵艰埅锛?

```text
Overview
Service Config
Sandbox
Logs
Security
Tenants & Users
Credentials
Execution Policy
MCP & Skills
Scheduler & Jobs
Snapshots
Audit
Metrics
Diagnostics
```

鍏抽敭椤甸潰璇存槑锛?

- `Overview`锛氬彧璇讳负涓伙紝灞曠ず鍋ュ悍鐘舵€併€佸憡璀︺€佹渶杩戦敊璇€?
- `Service Config`锛氳〃鍗?+ effective source + restart required 鏍囪銆?
- `Sandbox`锛氭娴嬬粨鏋溿€佸綋鍓嶅悗绔€乸rofile 缂栬緫銆乮nstall plan銆乻moke test銆?
- `Logs`锛歴ource selector銆乴evel filter銆乼ail銆乻earch銆乨ownload銆乺otate銆?
- `Security`锛氶闄╁Э鎬佸拰 secret/TLS/local execution 妫€鏌ャ€?
- `Language`锛氫腑鑻辨枃鍒囨崲锛屾湭鐧诲綍 setup 椤甸潰鍜岀櫥褰曞悗绠＄悊椤甸潰鍧囧彲浣跨敤銆?
- `Execution Policy`锛歭ocal bash銆丮CP銆乻kill銆丼SH銆乶etwork 绛栫暐鎬昏銆?
- `Diagnostics`锛歳eady report銆乨ependency check銆乫ilesystem check銆乻andbox smoke test銆丩LM provider quick check銆?

## 8. Admin API 鍛藉悕瑙勮寖

- 鎵€鏈夋湇鍔＄骇鑳藉姏鏀惧湪 `/api/v1/admin/...`銆?
- 璧勬簮鍚嶄娇鐢ㄥ鏁帮紝渚嬪 `/logs`銆乣/snapshots`銆乣/sandbox/profiles`銆?
- 鍔ㄤ綔鐢ㄥ瓙璺緞锛屼緥濡?`/reload`銆乣/rotate`銆乣/smoke-test`銆乣/validate`銆?
- 楂橀闄╁啓鎿嶄綔蹇呴』鏀寔 `dry_run` 鎴?`print_only`銆?
- 楂橀闄╁啓鎿嶄綔蹇呴』杩斿洖 `audit_event_id`銆?

缁熶竴閿欒鍝嶅簲锛?

```json
{
  "error": "sandbox smoke test failed",
  "code": "SANDBOX_SMOKE_TEST_FAILED",
  "details": {
    "backend": "bwrap",
    "stderr": "...redacted..."
  }
}
```

## 9. 瀛樺偍璁捐

寤鸿鏂板锛?

```text
MACLAW_DATA_ROOT/
  state/
    service_config.json
    sandbox_profiles.json
    sandbox_status_cache.json
  logs/
    maclawsrv.log
    maclawsrv.err.log
    sandbox.log
    scheduler.log
    access.log
```

濡傛灉鍦?macOS pkg 鎴?Linux systemd 閮ㄧ讲涓棩蹇楀啓鍏?`/Library/Logs/MaClawSrv` 鎴?`/var/log/maclawsrv`锛孉dmin Web 搴旈€氳繃 service config 鏆撮湶瀹為檯 log root銆?

## 10. Sandbox Runner 闆嗘垚鐐?

浠ｇ爜灞傞潰寤鸿鍙娊涓€灞?runner锛屼笉鏀瑰彉涓婂眰 tool 鍗忚銆?

闇€瑕佹帴鍏ョ殑鐜版湁鎵ц鐐癸細

- 鏈湴 bash锛歚corelib/agent/tools_local.go` 鐨?`ToolBash`銆?
- 鏈湴 MCP server锛歚corelib/agentservice/mcp.go` 鐨?`localMCPClient.Start`銆?
- Skill bash step锛歚corelib/agentservice/skill_integration.go` 鐨?`executeBashCommand`銆?

鎺ㄨ崘鎺ュ彛锛?

```go
type CommandRunner interface {
    CommandContext(ctx context.Context, spec CommandSpec) (*exec.Cmd, error)
}

type CommandSpec struct {
    Entrypoint string // local_bash, local_mcp, skill_step
    Command    string
    Args       []string
    Dir        string
    Env        []string
    Workspace  string
    Profile    string
}
```

`DirectRunner` 淇濇寔鐜扮姸锛宍SandboxRunner` 鍙敼鍙樻渶缁?argv锛屼緥濡傛妸鐪熷疄鍛戒护鍖呮垚 `bwrap ... -- real-command` 鎴?`sandlock run ... -- real-command`銆?

## 11. 瀹夊叏鍘熷垯

- Admin Web 榛樿鍙洃鍚?loopback锛涜繙绋嬭闂繀椤诲惎鐢?TLS 鎴栧弽鍚戜唬鐞嗚璇併€?
- 鎵€鏈?admin 鍐欐搷浣滃啓 audit銆?
- 鎵€鏈?shell/install 绫昏兘鍔涢粯璁?`print_only`锛屽繀椤绘樉寮忕‘璁ゆ墠鎵ц銆?
- 鏃ュ織鍜岄敊璇緭鍑虹粺涓€鑴辨晱銆?
- Sandbox strict 妯″紡涓嬶紝濡傛灉妫€娴嬩笉鍒板彲鐢ㄥ悗绔紝鏈湴 bash銆佹湰鍦?MCP銆乻kill step 搴?fail closed銆?
- 闈?strict 妯″紡涓嬶紝鍙互 fallback 鍒?direct锛屼絾 UI 蹇呴』鏄剧ず绾㈣壊椋庨櫓鐘舵€併€?
- 鏈嶅姟绾ч厤缃洿鏂拌杩斿洖 diff銆乻ource銆乺estart_required銆?

## 12. 鍒嗛樁娈佃鍒?

### Phase 1: 鍙 Admin Web + Sandbox Doctor

- Overview銆丷eadiness銆丮etrics銆丄lerts銆?
- Service Config effective view銆?
- Sandbox detect/status/smoke-test銆?
- Logs sources + recent errors + tail銆?
- 澶嶇敤鐜版湁 tenants/users/credentials/audit/snapshots 椤甸潰銆?

### Phase 2: 鍙啓鏈嶅姟绾ч厤缃?

- `service_config.json`銆?
- service-config schema/get/put/validate/effective銆?
- sandbox profiles get/put/validate銆?
- execution policy get/put/validate銆?
- 鎵€鏈夊啓鎿嶄綔 audit銆?

### Phase 3: Sandbox Runner 鐢熸晥

- 鎺ュ叆 local bash銆?
- 鎺ュ叆 skill step銆?
- 鎺ュ叆 local MCP server銆?
- 澧炲姞 sandbox execution events銆?
- strict/fallback 绛栫暐鐢熸晥銆?

### Phase 4: 杩愮淮澧炲己

- log rotate/download/search銆?
- scheduler admin銆?
- admin jobs 璺ㄧ敤鎴疯鍥俱€?
- runtime reload/cleanup銆?
- secret rotation plan銆?

## 13. 鏈€灏?API 娓呭崟

绗竴闃舵寤鸿鑷冲皯瀹炵幇锛?

```text
GET  /api/v1/admin/bootstrap/status
POST /api/v1/admin/bootstrap/initialize
POST /api/v1/admin/auth/login
POST /api/v1/admin/auth/logout
GET  /api/v1/admin/auth/me
GET  /api/v1/admin/tenants
POST /api/v1/admin/tenants
GET  /api/v1/admin/tenants/{tenantId}/users
POST /api/v1/admin/tenants/{tenantId}/users
GET  /api/v1/admin/tenants/{tenantId}/delete-check
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}/delete-check
GET  /api/v1/admin/tenants/{tenantId}/retire-plan
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}/retire-plan
GET  /api/v1/admin/service-config/effective
GET  /api/v1/admin/sandbox/status
POST /api/v1/admin/sandbox/detect
POST /api/v1/admin/sandbox/smoke-test
POST /api/v1/admin/sandbox/diagnose
GET  /api/v1/admin/sandbox/install-plan
GET  /api/v1/admin/logs/sources
GET  /api/v1/admin/logs/errors/recent
GET  /api/v1/admin/logs/{source}/tail
GET  /api/v1/admin/security/posture
```

杩欎簺鎺ュ彛瓒充互鏀拺涓€涓湁浠峰€肩殑 Admin Web 棣栫増锛屽悓鏃朵笉浼氱珛鍒绘敼鍙?`MaClawSrv` 鐨勬墽琛岃矾寰勩€?

## 14. 鍐崇瓥寤鸿

Admin Web 搴斿厛鍋氣€滆瀵熷拰璇婃柇鈥濓紝鍐嶅仛鈥滃啓閰嶇疆鈥濓紝鏈€鍚庡仛鈥滄墽琛岃矾寰勬帴绠♀€濄€係andbox 涔熷簲鎸夎繖涓『搴忚惤鍦帮細

```text
detect/status -> install-plan -> profile validate -> smoke-test -> runner integration -> strict mode
```

杩欐牱鍙互鎶婇闄╂帶鍒跺湪鍙洖婊氳寖鍥村唴锛屽苟涓旇绠＄悊鍛樺湪鍚敤 sandbox 鍓嶇湅鍒板綋鍓嶆満鍣ㄥ埌搴曟敮鎸佷粈涔堛€?














