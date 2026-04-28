# MaClawSrv 缂哄彛鍒嗘瀽



杩欎唤鏂囨。鐢ㄤ簬绯荤粺姊崇悊 `MaClawSrv` 褰撳墠宸茬粡鍏峰鐨勮兘鍔涖€佽繕缂哄皯鐨勫姛鑳斤紝浠ュ強鎺ュ彛涓€鑷存€т笂杩橀渶瑕佺户缁敹鍙ｇ殑鍦版柟锛屾柟渚垮悗缁寜浼樺厛绾ф帹杩涖€?



## 褰撳墠鐘舵€?



`MaClawSrv` 浣滀负澶氱鎴?Maclaw Agent REST 鏈嶅姟锛屽凡缁忎笉鏄€滃彧鏈夐鏋垛€濈殑闃舵浜嗭紝涓婚摼璺凡缁忔瘮杈冨畬鏁淬€?



鐩墠宸茬粡姣旇緝鎵庡疄鐨勯儴鍒嗭細



- 绉熸埛銆佺敤鎴枫€佸嚟璇佺殑绠＄悊绔熀纭€鐢熷懡鍛ㄦ湡

- tenant / user 鍒犻櫎鎺ュ彛

- credential 鐨勫垱寤恒€佸垪琛ㄣ€佸崟椤规煡璇€佹洿鏂般€乺otate secret銆佸悐閿€

- `api_key + api_secret -> bearer token` 鐨勭敤鎴烽壌鏉冮摼璺?

- 鐢ㄦ埛鍏变韩閰嶇疆鐨?schema / 鑾峰彇 / 鏇存柊 / 鏍￠獙 / 娴嬭瘯

- instance / session / message / run 鐨勮繍琛屾椂閾捐矾

- run 浜嬩欢 SSE 鎺ㄩ€?

- skill 绠＄悊 REST 鎺ュ彛

- MCP 绠＄悊 REST 鎺ュ彛

- usage / audit / alerts / dashboard / overview / tenant summary

- `/openapi.json` 鏈哄櫒鍙鎻忚堪

- `/health`銆乣/livez`銆乣/readyz`銆乣/version` 杩愮淮鎺㈤拡



鎵€浠ョ幇鍦ㄧ殑閲嶇偣锛屼笉鍐嶆槸鈥滄湁娌℃湁鍩虹鑳藉姏鈥濓紝鑰屾槸鈥滄槸鍚﹀凡缁忔垚涓轰竴涓畬鏁淬€佺ǔ瀹氥€佸澶栧弸濂界殑鎺у埗骞抽潰鈥濄€?



## 杩樼己浠€涔?



### 1. 绠＄悊鐢熷懡鍛ㄦ湡杩樺彲浠ョ户缁垚鐔熷寲



铏界劧 tenant 鍜?user 鍒犻櫎鎺ュ彛宸茬粡琛ヤ笂锛屼絾鍚庣画杩樺€煎緱缁х画鍋氾細



- 杞垹闄?/ 鍥炴敹绔欒涔?

- 鍒犻櫎淇濇姢绛栫暐

- 鍒犻櫎鍓嶅鍑?

- 鏇存槑纭殑閫€褰规祦绋?



### 2. 鍑瘉鐢熷懡鍛ㄦ湡杩樻病瀹屽叏鎵撶（瀹?



鐜板湪宸叉湁鐨勬槸锛氬垪琛ㄣ€佸垱寤恒€佸崟椤规煡璇€佹洿鏂般€乺otate secret銆佸悐閿€銆?



杩樼己灏戯細



- API key 鑷韩鐨勮疆鎹㈣兘鍔?

- 姣?active / revoked 鏇寸粏鐨?suspend / expire 鐢熷懡鍛ㄦ湡

- 鏇村畬鏁寸殑涓€娆℃€?secret 鐢熸垚涓庡洖鏄炬ā鍨?



涔熷氨鏄锛宑redential 杩欏潡宸茬粡浠庘€滃彧鏈夊熀纭€鑳藉姏鈥濊繘鍏モ€滃熀鏈彲鐢ㄢ€濓紝浣嗚繕娌℃湁瀹屽叏鎵撶（瀹屻€?



### 3. 绠＄悊绔悳绱?杩囨护鑳藉姏鍋忓急



铏界劧宸茬粡鏈夊垎椤碉紝浣嗗鐪熷疄杩愯惀鍦烘櫙杩樹笉澶熴€?



寤鸿琛ュ厖鐨勮繃婊よ兘鍔涳細



- tenant 鎸?`status`

- tenant 鎸?`name`

- user 鎸?`status`

- user 鎸?`name`

- user 鎸?`email`

- 璺?tenant 鐨?user 鎼滅储



鍚﹀垯涓€鏃︾鎴峰拰鐢ㄦ埛瑙勬ā涓婃潵锛岀函鍒嗛〉娴忚浼氭瘮杈冮毦鐢ㄣ€?



### 4. 缂哄皯瀵煎嚭銆佸鍏ャ€佸浠姐€佽縼绉绘帴鍙?



褰撳墠杩樻病鏈?REST 鑳藉姏鏉ュ仛锛?



- 瀵煎嚭鏈嶅姟鐘舵€?

- 瀵煎叆鏈嶅姟鐘舵€?

- tenant 绾ф暟鎹揩鐓?

- 鐜闂磋縼绉?



杩欎細褰卞搷杩愮淮銆佸浠芥仮澶嶏紝浠ュ強浼佷笟鍖栨帴鍏ャ€?



### 5. 缂哄皯寮傛浠诲姟妯″瀷



鐜板湪鏈変簺鎿嶄綔鍏跺疄宸茬粡涓嶅啀閫傚悎鈥滃悓姝ヨ姹?鍚屾杩斿洖鈥濇ā鍨嬶紝姣斿锛?



- skill install

- skill import

- skill upload

- MCP start

- MCP health-check



鍚庣画鏇村悎鐞嗙殑鏂瑰悜锛屾槸澧炲姞 job 璧勬簮锛屼緥濡傦細



- `POST /api/v1/jobs`

- `GET /api/v1/jobs/{jobId}`

- `GET /api/v1/jobs/{jobId}/events`

- `POST /api/v1/jobs/{jobId}/cancel`



杩欐牱鑳芥洿濂藉鐞嗚秴鏃躲€侀噸璇曘€佽繘搴﹀拰鍘嗗彶鏌ヨ銆?



### 6. 缂哄皯鏈嶅姟绾?webhook / 浜嬩欢璁㈤槄



鐜板湪鍙湁 run SSE銆?



杩樼己灏戠殑浜嬩欢闈㈠寘鎷細



- 绠＄悊闈簨浠惰闃?

- tenant / user 鐢熷懡鍛ㄦ湡 webhook

- run 瀹屾垚閫氱煡

- skill / MCP 鎿嶄綔瀹屾垚閫氱煡



濡傛灉澶栭儴骞冲彴瑕佸洿缁?`MaClawSrv` 鑷姩鍖栫紪鎺掞紝杩欎竴灞備細寰堥噸瑕併€?



### 7. 杩愮淮鎺ュ彛杩樺樊鏈€鍚庝竴鎴?



褰撳墠闄や簡 `GET /health`锛屽凡缁忚ˉ涓婏細



- `GET /readyz`

- `GET /livez`

- `GET /version`



杩樼己鐨勬槸锛?



- `GET /metrics`



杩欐牱鎵嶈兘鏇存柟渚挎帴 Kubernetes銆丳rometheus銆佽礋杞藉潎琛″櫒鍜屾爣鍑嗚繍缁翠綋绯汇€?



### 8. 缂哄皯鏇村己鐨勮仛鍚堝垎鏋愭帴鍙?



宸叉湁 overview/dashboard 宸茬粡鏈夊府鍔╋紝浣嗚繕涓嶇畻寮鸿繍钀ヨ瑙掋€?



鍚庣画鍙€冭檻锛?



- 鐑棬 tenant

- 瓒呴 tenant

- 闀挎湡涓嶆椿璺?tenant / user

- 閿欒鐜囪秼鍔?

- skill 浣跨敤鍒嗘瀽

- MCP 浣跨敤鍒嗘瀽



## 鎺ュ彛涓€鑷存€ц繕宸粈涔?



### 1. action 鍨嬫帴鍙ｉ鏍奸渶瑕佹寔缁槑纭?



鐜板湪鎺ュ彛椋庢牸鏈韩鏄悎鐞嗙殑锛屼絾鏈€濂芥寔缁槑纭憡璇夋帴鍏ユ柟锛?



- 璧勬簮鍨嬫搷浣滆蛋 `GET / POST / PATCH / PUT / DELETE`

- 鐘舵€佸垏鎹㈡垨鍛戒护鍨嬫搷浣滆蛋 `/stop`銆乣/resume`銆乣/archive`銆乣/restore`銆乣/health-check` 杩欑 action 璺敱



杩欐牱澶栭儴璋冪敤鏂逛笉浼氳鐚滄煇涓姩浣滄槸涓嶆槸搴旇鐢?`PATCH status=...`銆?



### 2. 鍒嗛〉鏀寔鑼冨洿瑕佸拰瀹為檯瀹炵幇瀹屽叏瀵归綈



鐩墠鐪熸鏀寔鍒嗛〉鐨勬湁锛?



- admin tenants

- admin users

- admin credentials

- admin audit-events

- MCP servers

- skills

- instances

- sessions

- messages

- runs



濡傛灉鏂囨。婕忔帀 MCP 鎴?skills锛孲DK 浣滆€呭氨寰堝鏄撴寜閿欐柟寮忔帴鍏ャ€?



### 3. OpenAPI 搴旇琚涓烘渶缁堢湡鐩?



`openapi.go` 鐜板湪姣?prose 鏂囨。鏇存暣娲侊紝涔熸洿閫傚悎浣滀负璺敱鐪熺浉鏉ユ簮銆?



鍚庣画寤鸿鍥哄畾娴佺▼锛?



1. 鍏堟敼 `http.go`

2. 鍐嶅悓姝?`openapi.go`

3. 鏈€鍚庢洿鏂?README 鍜屾墜鍐?



### 4. 閲嶆搷浣滄帴鍙ｇ幇鍦ㄥ湪 HTTP 杈圭晫涓婁粛鏄惧緱鈥滆繃浜庡悓姝モ€?



杩欎笉绠?bug锛屼絾浠庢帴鍙ｆ垚鐔熷害鐪嬶紝鍍?skill install銆丮CP health-check 杩欑被鎿嶄綔锛屽悗缁洿閫傚悎缁熶竴鎶借薄鎴?job锛岃€屼笉鏄户缁爢 action endpoint銆?



## 寤鸿鐨勪笅涓€姝ヤ紭鍏堢骇



### 绗竴浼樺厛绾?



- 鎸佺画鏀跺彛 README / OpenAPI / 鎵嬪唽涓€鑷存€?

- 澧炲己 tenant / user 鎼滅储杩囨护

- 琛ユ洿缁嗙殑鎸囨爣缁村害涓庡憡璀︽寚鏍?



### 绗簩浼樺厛绾?



- 澧炲姞鏈嶅姟绾у鍑哄鍏ユ帴鍙?

- 澧炲姞鏇存垚鐔熺殑 delete / retire 绛栫暐

- 缁х画瀹屽杽 credential 鐢熷懡鍛ㄦ湡



### 绗笁浼樺厛绾?



- 澧炲姞寮傛 job 妯″瀷

- 澧炲姞 webhook / 浜嬩欢璁㈤槄妯″瀷

- 澧炲姞鏇村己鐨勭鐞嗗垎鏋愭帴鍙?



## 缁撹



濡傛灉鐩爣鏄 `MaClawSrv` 鎴愪负涓€涓澶栫ǔ瀹氥€佸ソ鎺ャ€佸ソ杩愯惀鐨?agent 鎺у埗骞抽潰锛屽缓璁寜涓嬮潰椤哄簭鎺ㄨ繘锛?



1. 鍏堟妸鏂囨。鍜?OpenAPI 褰诲簳瀵归綈銆?

2. 鍐嶈ˉ鎼滅储銆佽繃婊ゅ拰 metrics銆?

3. 鍐嶈ˉ瀵煎嚭瀵煎叆鍜屾洿鎴愮啛鐨勭敓鍛藉懆鏈熺瓥鐣ャ€?

4. 鏈€鍚庡紩鍏?job 鍜?webhook 杩欑被骞冲彴鍖栬兘鍔涖€?



杩欐牱鏀剁泭鏈€澶э紝涔熶笉浼氭妸褰撳墠宸茬粡鎴愬瀷鐨勪富閾捐矾鎺ㄧ炕閲嶅仛銆?






## ?????????

- ?????????????????
- ???????????????????? `delete-check` ?????????????????????? `409`?
- ????????????????????????

## ????????????

- ????? credential ????????
- ????? credential ????? `api_key` ?/? `api_secret`?
- ????? secret ??????????????????????????

## ?????Credential ????

- Credential ???????? admin alerts ???
- ?????? `kind=credential_expiring` ? `kind=credential_expired` ???????? `credential_expiry_window_days` ???????
- ???? credential ????????

## ?????credential overview ??

- Admin overview ??? credential ???????
- ????????????????????? total?active?suspended?revoked?expired ? expiring credential ???

## 鏈€杩戝畬鎴愶細tenant summary 鍑瘉璁℃暟

- Tenant summary 宸插湪绉熸埛绾у拰鐢ㄦ埛绾у悓鏃惰繑鍥?credential 鐢熷懡鍛ㄦ湡璁℃暟銆?- 绠＄悊鎺у埗鍙版棤闇€閫愪釜鐢ㄦ埛鎷夊彇 credential 鍒楄〃锛屽氨鑳藉睍绀?total銆乤ctive銆乻uspended銆乺evoked銆乪xpired銆乪xpiring 绛夎鏁般€?

## 鏈€杩戝畬鎴愶細credential metrics

- `/metrics` 宸茶緭鍑?credential 鎬绘暟銆佺姸鎬併€佸凡杩囨湡銆佸嵆灏嗚繃鏈熺瓑 Prometheus gauge銆?- 杩愮淮渚у彲浠ョ洿鎺ュ熀浜?Prometheus 鍛婅 credential 鐢熷懡鍛ㄦ湡椋庨櫓锛屼笉蹇呴澶栬疆璇?admin JSON 鎺ュ彛銆?

## 鏈€杩戝畬鎴愶細usage summary 鍑瘉璁℃暟

- 宸茶璇佺敤鎴风幇鍦ㄥ彲浠ラ€氳繃 `/api/v1/usage/summary` 鏌ョ湅鑷繁鐨?credential 鐢熷懡鍛ㄦ湡鑱氬悎璁℃暟銆?- 璇ユ帴鍙ｅ彧杩斿洖璁℃暟锛屼笉杩斿洖浠讳綍 credential 瀵嗛挜鏉愭枡锛屼繚鎸侀粯璁ゅ畨鍏ㄣ€?

## 鏈€杩戝畬鎴愶細credential 鍒涘缓鏃惰缃繃鏈熸椂闂?
- Credential 鍒涘缓鎺ュ彛鐜板湪鏀寔鐩存帴浼犲叆 `expires_at`銆?- 绠＄悊绔彲浠ュ湪涓€娆¤姹備腑鍒涘缓鑷姩鐢熸垚鐨勫嚟璇佸苟鍚屾椂璁剧疆杩囨湡鏃堕棿锛岄伩鍏嶅厛鍒涘缓鏃犻檺鍒跺嚟璇佸啀 PATCH 鐨勭煭鏆傜獥鍙ｃ€?

## 鏈€杩戝畬鎴愶細credential 鍒楄〃杩囨护

- Credential 鍒楄〃鎺ュ彛鐜板湪鏀寔鍦ㄥ垎椤靛墠鎸?`status`銆乣expired`銆乣expiring` 杩囨护銆?- 绠＄悊宸ュ叿鍙互鐩存帴瀹氫綅 suspended銆乺evoked銆乪xpired 鎴栧嵆灏嗚繃鏈熺殑鍑瘉锛屼笉闇€瑕佸鎴风鍏ㄩ噺鎷夊彇鍚庡啀绛涢€夈€?

## 鏈€杩戝畬鎴愶細audit event 璧勬簮杩囨护

- Audit events 鐜板湪鏀寔鎸?`resource_id` 鍜?`actor_type` 杩囨护銆?- 杩愮淮鎴栫鐞嗗伐鍏峰彲浠ョ洿鎺ヨ拷韪煇涓?credential銆乺un銆乽ser 鎴?tenant 鐨勪簨浠堕摼璺紝涓嶅繀涓嬭浇鏇村ぇ鐨勫璁″垎椤靛悗鍐嶆湰鍦扮瓫閫夈€?

## 鏈€杩戝畬鎴愶細audit event 鏃堕棿绐楀彛

- Audit events 鐜板湪鏀寔鍦ㄥ垎椤靛墠鎸?`since` 鍜?`until` 杩囨护銆?- 澶栭儴宸ュ叿鍙互鎷夊彇鏈€杩戠獥鍙ｆ垨鎺掓煡浜嬫晠鏃堕棿娈靛唴鐨勪簨浠讹紝鑰屼笉闇€瑕佷笅杞芥棤鍏冲巻鍙层€?


琛ュ厖锛氱鐞嗙蹇収鎺ュ彛宸查€氳繃 /api/v1/admin/snapshots 鎻愪緵锛屽彲鍒涘缓銆佸垪鍑恒€佽鍙栧拰鍒犻櫎鎸佷箙鍖栧鍑哄揩鐓э紱鍚庣画杩樺彲缁х画澧炲己瀹氭椂蹇収銆佷繚鐣欑瓥鐣ュ拰鍥炴粴缂栨帓銆?


