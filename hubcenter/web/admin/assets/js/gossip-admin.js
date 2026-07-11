// Gossip moderation and policy configuration.
// Gossip Management
    const GOSSIP_TEXT={"en":{"comments":"Comments","lock":"Lock","unlock":"Unlock","delete":"Delete","noComments":"No comments","postDeleted":"Post deleted","postLocked":"Post locked","postUnlocked":"Post unlocked","commentDeleted":"Comment deleted","loadFailed":"Load gossip failed: {error}","loadCommentsFailed":"Load comments failed: {error}","locked":"Locked","open":"Open","machine":"Machine","email":"Email","score":"Score","votes":"Votes","id":"ID","unknownUser":"Unknown","na":"n/a","page":"Page {current} / {total}"},"zh":{"comments":"\u8bc4\u8bba","lock":"\u9501\u5b9a","unlock":"\u89e3\u9501","delete":"\u5220\u9664","noComments":"\u6682\u65e0\u8bc4\u8bba","postDeleted":"\u5df2\u5220\u9664\u5410\u69fd","postLocked":"\u5df2\u9501\u5b9a\u5410\u69fd","postUnlocked":"\u5df2\u89e3\u9501\u5410\u69fd","commentDeleted":"\u5df2\u5220\u9664\u8bc4\u8bba","loadFailed":"\u52a0\u8f7d\u5410\u69fd\u5217\u8868\u5931\u8d25\uff1a{error}","loadCommentsFailed":"\u52a0\u8f7d\u8bc4\u8bba\u5931\u8d25\uff1a{error}","locked":"\u5df2\u9501\u5b9a","open":"\u5f00\u653e","machine":"\u673a\u5668","email":"\u90ae\u7bb1","score":"\u5206\u503c","votes":"\u6295\u7968","id":"\u7f16\u53f7","unknownUser":"\u672a\u77e5\u7528\u6237","na":"\u65e0","page":"\u7b2c {current} / {total} \u9875"}};
    const gtr=(key,vars={})=>((GOSSIP_TEXT[currentLang]||GOSSIP_TEXT.en)[key]||GOSSIP_TEXT.en[key]||key).replace(/\{(\w+)\}/g,(_,n)=>vars[n]??'');
    // Values are embedded in double-quoted inline event attributes. Escape the
    // JSON string for HTML first, otherwise its surrounding double quotes end
    // the attribute and the browser evaluates a truncated handler.
    const jsArg=value=>JSON.stringify(String(value??''))
      .replace(/&/g,'&amp;').replace(/"/g,'&quot;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
    const gossipDomToken=value=>Array.from(String(value??''),ch=>ch.codePointAt(0).toString(16)).join('_')||'empty';
    const gossipCommentRootID=postId=>'gossip-comments-'+gossipDomToken(postId);
    let gossipPage=1;const gossipPageSize=100;const gossipCommentPageSize=100;const gossipCommentPages={};
    async function loadGossipList(page){if(page)gossipPage=page;try{const data=await api('/api/admin/gossip?page='+gossipPage);const posts=data.posts||[];const total=data.total||0;const root=document.getElementById('gossipList');const totalPages=Math.max(1,Math.ceil(total/gossipPageSize));if(!posts.length){if(total>0&&gossipPage>totalPages){gossipPage=totalPages;return loadGossipList(gossipPage)}root.innerHTML='<div class="hint">'+tr('gossipEmpty')+'</div>';document.getElementById('gossipPager').classList.remove('is-visible');return}root.innerHTML=posts.map(p=>{const nickname=escapeHtml(p.nickname||gtr('unknownUser'));const category=escapeHtml(p.category||'-');const content=escapeHtml(p.content||'');const machine=escapeHtml(p.machine_id||gtr('na'));const email=escapeHtml(p.user_email||gtr('na'));const createdAt=escapeHtml(p.created_at||'-');const score=Number(p.score||0);const votes=Number(p.votes||0);const postId=jsArg(p.id);const commentsId=escapeHtml(gossipCommentRootID(p.id));return `<div class="item gossip-card"><div class="item-head"><div class="gossip-title"><span class="item-title gossip-name" title="${nickname}">${nickname}</span><span class="badge ${p.locked?'warn':'info'}">${p.locked?gtr('locked'):gtr('open')}</span></div><span class="badge info">${category}</span></div><div class="item-meta gossip-text" title="${content}">${content||'-'}</div><div class="item-meta mono gossip-meta"><span>${gtr('machine')}: ${machine}</span><span>${gtr('email')}: ${email}</span><span>${gtr('score')}: ${score}</span><span>${gtr('votes')}: ${votes}</span></div><div class="item-meta mono gossip-detail" title="${createdAt}">${createdAt}</div><div class="actions"><button class="btn-ghost" onclick="loadGossipComments(${postId})">${gtr('comments')}</button><button class="btn-secondary" onclick="toggleGossipLock(${postId},${!p.locked})">${p.locked?gtr('unlock'):gtr('lock')}</button><button class="btn-danger" onclick="deleteGossipPost(${postId})">${gtr('delete')}</button></div><div id="${commentsId}" class="list gossip-comment-list"></div></div>`}).join('');document.getElementById('gossipPager').classList.add('is-visible');document.getElementById('gossipPageInfo').textContent=gtr('page',{current:gossipPage,total:totalPages});document.getElementById('gossipPrevBtn').disabled=gossipPage<=1;document.getElementById('gossipNextBtn').disabled=gossipPage>=totalPages}catch(err){setOutput(gtr('loadFailed',{error:err.message}))}}
    function changeGossipPage(delta){loadGossipList(gossipPage+delta)}
    async function deleteGossipPost(id){try{await api('/api/admin/gossip',{method:'DELETE',body:JSON.stringify({id})});showToast(gtr('postDeleted'),'success');loadGossipList()}catch(err){showToast(err.message,'error')}}
    async function toggleGossipLock(id,locked){try{await api('/api/admin/gossip/lock',{method:'POST',body:JSON.stringify({id,locked})});showToast(locked?gtr('postLocked'):gtr('postUnlocked'),'success');loadGossipList()}catch(err){showToast(err.message,'error')}}
    function renderGossipCommentPager(postId,currentPage,totalPages){if(totalPages<=1)return '';const postArg=jsArg(postId);return `<div class="pager pager-compact"><div class="pager-meta">${gtr('page',{current:currentPage,total:totalPages})}</div><div class="pager-actions"><button class="btn-ghost compact-btn" type="button" aria-label="Previous comments page" onclick="changeGossipCommentPage(${postArg},-1)" ${currentPage<=1?'disabled':''}>&larr;</button><button class="btn-ghost compact-btn" type="button" aria-label="Next comments page" onclick="changeGossipCommentPage(${postArg},1)" ${currentPage>=totalPages?'disabled':''}>&rarr;</button></div></div>`;}
    async function loadGossipComments(postId,page){try{const root=document.getElementById(gossipCommentRootID(postId));if(!root)return;const currentPage=page||gossipCommentPages[postId]||1;gossipCommentPages[postId]=currentPage;const data=await api('/api/admin/gossip/comments?post_id='+encodeURIComponent(postId)+'&page='+currentPage);const comments=data.comments||[];const total=Number(data.total||0);const totalPages=Math.max(1,Math.ceil(total/gossipCommentPageSize));if(!comments.length&&total>0&&currentPage>totalPages){gossipCommentPages[postId]=totalPages;return loadGossipComments(postId,totalPages)}if(!comments.length){root.innerHTML='<div class="hint hint-pad">'+gtr('noComments')+'</div>';return}const html=comments.map(c=>{const content=escapeHtml(c.content||'');const nickname=escapeHtml(c.nickname||gtr('unknownUser'));const machine=escapeHtml(c.machine_id||gtr('na'));const email=escapeHtml(c.user_email||gtr('na'));const createdAt=escapeHtml(c.created_at||'-');const rating=c.rating?'<span class="badge info">&#9733;'+escapeHtml(String(c.rating))+'</span>':'';return `<div class="item gossip-comment-card"><div class="item-meta" title="${content}">${content||'-'} ${rating}</div><div class="mono gossip-detail" title="${nickname} | ${machine} | ${email} | ${createdAt}">${nickname} | ${machine} | ${email} | ${createdAt}</div><div class="actions"><button class="btn-danger tiny-btn" onclick="deleteGossipComment(${jsArg(c.id)},${jsArg(c.post_id)})">${gtr('delete')}</button></div></div>`}).join('');root.innerHTML=html+renderGossipCommentPager(postId,currentPage,totalPages)}catch(err){setOutput(gtr('loadCommentsFailed',{error:err.message}))}}
    function changeGossipCommentPage(postId,delta){const next=(gossipCommentPages[postId]||1)+delta;if(next<1)return;loadGossipComments(postId,next)}
    async function deleteGossipComment(id,postId){try{await api('/api/admin/gossip/comments',{method:'DELETE',body:JSON.stringify({id,post_id:postId})});showToast(gtr('commentDeleted'),'success');loadGossipComments(postId,gossipCommentPages[postId]||1)}catch(err){showToast(err.message,'error')}}
    function escapeHtml(s){if(!s)return'';const m={'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'};return String(s).replace(/[&<>"']/g,c=>m[c])}
    // SkillHub Management
    // Import from URL
    function applyImportI18n(){
      document.getElementById('importTitle').textContent=shtr('importTitle');
      document.getElementById('importDesc').textContent=shtr('importDesc');
      document.getElementById('importUrlInput').placeholder=shtr('importPlaceholder');
      document.getElementById('importBtn').textContent=shtr('import');
      const searchTitle=document.getElementById('catalogSearchTitle');if(searchTitle)searchTitle.textContent=shtr('searchTitle');
      const searchDesc=document.getElementById('catalogSearchDesc');if(searchDesc)searchDesc.textContent=shtr('searchDesc');
      const searchInput=document.getElementById('catalogSearchInput');if(searchInput)searchInput.placeholder=shtr('searchPlaceholder');
      const searchBtn=document.getElementById('catalogSearchBtn');if(searchBtn)searchBtn.textContent=shtr('search');
    }
    async function importFromURL(){
      const url=document.getElementById('importUrlInput').value.trim();
      if(!url){showToast(shtr('enterUrl'),'error');return}
      const btn=document.getElementById('importBtn');
      const resultEl=document.getElementById('importResult');
      btn.disabled=true;btn.textContent=shtr('importing');
      resultEl.textContent='';
      try{
        const data=await api('/api/admin/skillhub/import-url',{method:'POST',body:JSON.stringify({url})});
        const msgs=[];
        if(data.published&&data.published.length)msgs.push(shtr('importedPrefix')+data.published.join(', '));
        if(data.errors&&data.errors.length)msgs.push(shtr('errorsPrefix')+data.errors.join('; '));
        if(!data.published||!data.published.length)msgs.push(shtr('noImportable'));
        resultEl.textContent=msgs.join(' | ');
        if(data.published&&data.published.length){showToast(shtr('importedCount',{count:data.published.length}),'success');loadSkillHubList()}
      }catch(err){resultEl.textContent=err.message;showToast(err.message,'error')}
      finally{btn.disabled=false;btn.textContent=shtr('import')}
    }
    async function catalogSearchExternal(){
      const query=document.getElementById('catalogSearchInput').value.trim();
      if(!query){showToast(shtr('searchEnterKeyword'),'error');return}
      const btn=document.getElementById('catalogSearchBtn');
      const root=document.getElementById('catalogSearchResults');
      btn.disabled=true;btn.textContent=shtr('searching');
      root.innerHTML='';
      try{
        const data=await api('/api/admin/capability-market/external-search?type=skill&q='+encodeURIComponent(query));
        const items=data.items||[];
        if(!items.length){root.innerHTML='<div class="hint grid-span-all">'+shtr('searchNoResults')+'</div>';return}
        root.innerHTML=items.map((s,idx)=>{const desc=escapeHtml((s.description||'').slice(0,80));const src=escapeHtml(s.source||'');const ver=s.version?`<span class="badge info skillhub-badge">v${escapeHtml(s.version)}</span>`:'';return `<div class="item skillhub-card"><div class="item-head"><div class="item-title skillhub-title" title="${escapeHtml(s.name||s.display_name||s.id)}">${escapeHtml(s.name||s.display_name||s.id)}</div>${ver}</div><span class="badge ${src==='github'?'warn':'ok'} skillhub-badge">${src}</span><div class="item-meta skillhub-desc" title="${desc}">${desc}</div><div class="actions"><button class="btn-primary" onclick="catalogImportSearchResult(${idx})">${shtr('import')}</button></div></div>`}).join('');
        window._catalogSearchResults=items;
      }catch(err){root.innerHTML='<div class="hint grid-span-all">'+escapeHtml(err.message)+'</div>';showToast(err.message,'error')}
      finally{btn.disabled=false;btn.textContent=shtr('search')}
    }
    async function catalogImportSearchResult(idx){
      const item=(window._catalogSearchResults||[])[idx];if(!item)return;
      const source=item.source||'';const installRef=item.install_ref||item.id||item.repo_url||'';
      if(!installRef){showToast('No install reference','error');return}
      try{
        const data=await api('/api/admin/capability-market/import',{method:'POST',body:JSON.stringify({capability_type:'skill',source:source,install_ref:installRef,display_name:item.name||item.display_name||'',description:item.description||''})});
        showToast(shtr('importedCount',{count:1}),'success');loadSkillHubList();
      }catch(err){showToast(err.message,'error')}
    }
    const SKILLHUB_TEXT_EN={"searchTitle":"Search External Skills","searchDesc":"Search ClawHub and GitHub for skills to import into the catalog.","searchPlaceholder":"Enter keyword to search...","search":"Search","searching":"Searching...","searchEnterKeyword":"Please enter a keyword","searchNoResults":"No results found","importTitle":"Import Skill from URL","importDesc":"Enter a GitHub repo URL or raw skill.yaml URL to import free Skills.","importPlaceholder":"https://github.com/user/repo or raw skill.yaml URL","import":"Import","importing":"Importing...","enterUrl":"Please enter a URL","importedPrefix":"Imported: ","errorsPrefix":"Errors: ","noImportable":"No importable skills found","importedCount":"Imported {count} skill(s)","visible":"Visible","hidden":"Hidden","noDescription":"No description","show":"Show","hide":"Hide","delete":"Delete","visibilityUpdated":"Visibility updated","deleteConfirm":"Are you sure you want to delete skill \"{name}\"? This cannot be undone.","deleted":"Skill deleted","loadFailed":"Load SkillHub failed: {error}","page":"Page {current} / {total}","source":"Source","downloads":"Downloads","rating":"Rating","updated":"Updated","trust":"Trust","unknown":"Unknown","author":"Author","recommend":"Recommend","uploadToMarket":"Upload to Market"}; const SKILLHUB_TEXT={en:SKILLHUB_TEXT_EN,zh:Object.assign({},SKILLHUB_TEXT_EN,{searchTitle:"\u641c\u7d22\u5916\u90e8\u6280\u80fd",searchDesc:"\u4ece ClawHub \u548c GitHub \u641c\u7d22\u6280\u80fd\u5e76\u5bfc\u5165\u5230\u76ee\u5f55\u3002",searchPlaceholder:"\u8f93\u5165\u5173\u952e\u8bcd\u641c\u7d22...",search:"\u641c\u7d22",searching:"\u641c\u7d22\u4e2d...",searchEnterKeyword:"\u8bf7\u8f93\u5165\u5173\u952e\u8bcd",searchNoResults:"\u672a\u627e\u5230\u7ed3\u679c",importTitle:"\u4ece\u94fe\u63a5\u5bfc\u5165\u6280\u80fd",importDesc:"\u8f93\u5165 GitHub \u4ed3\u5e93\u94fe\u63a5\u6216\u539f\u59cb skill.yaml \u94fe\u63a5\uff0c\u7528\u4e8e\u5bfc\u5165\u514d\u8d39\u6280\u80fd\u3002",importPlaceholder:"https://github.com/user/repo \u6216\u539f\u59cb skill.yaml \u94fe\u63a5",import:"\u5bfc\u5165",importing:"\u5bfc\u5165\u4e2d...",enterUrl:"\u8bf7\u8f93\u5165\u94fe\u63a5",importedPrefix:"\u5df2\u5bfc\u5165\uff1a",errorsPrefix:"\u9519\u8bef\uff1a",noImportable:"\u672a\u53d1\u73b0\u53ef\u5bfc\u5165\u7684\u6280\u80fd",importedCount:"\u5df2\u5bfc\u5165 {count} \u4e2a\u6280\u80fd",visible:"\u53ef\u89c1",hidden:"\u9690\u85cf",noDescription:"\u6682\u65e0\u63cf\u8ff0",show:"\u663e\u793a",hide:"\u9690\u85cf",delete:"\u5220\u9664",visibilityUpdated:"\u53ef\u89c1\u6027\u5df2\u66f4\u65b0",deleteConfirm:"\u786e\u5b9a\u5220\u9664\u6280\u80fd\u300c{name}\u300d\uff1f\u8fd9\u4e2a\u64cd\u4f5c\u4e0d\u53ef\u64a4\u9500\u3002",deleted:"\u5df2\u5220\u9664",loadFailed:"\u52a0\u8f7d\u6280\u80fd\u76ee\u5f55\u5931\u8d25\uff1a{error}",page:"\u7b2c {current} / {total} \u9875",source:"\u6765\u6e90",downloads:"\u4e0b\u8f7d",rating:"\u8bc4\u5206",updated:"\u66f4\u65b0",trust:"\u4fe1\u4efb",unknown:"\u672a\u77e5",author:"\u4f5c\u8005",recommend:"\u8bbe\u4e3a\u63a8\u8350",uploadToMarket:"\u4e0a\u4f20\u5e02\u573a"})};
    const shtr=(key,vars={})=>((SKILLHUB_TEXT[currentLang]||SKILLHUB_TEXT.en)[key]||SKILLHUB_TEXT.en[key]||key).replace(/\{(\w+)\}/g,(_,n)=>vars[n]??'');
    function applySkillHubI18n(){
      const set=(id,value)=>{const el=document.getElementById(id); if(el) el.textContent=value;};
      set('importTitle',shtr('importTitle'));
      set('importDesc',shtr('importDesc'));
      set('importBtn',shtr('import'));
      set('catalogSearchTitle',shtr('searchTitle'));
      set('catalogSearchDesc',shtr('searchDesc'));
      set('catalogSearchBtn',shtr('search'));
      const input=document.getElementById('importUrlInput'); if(input) input.placeholder=shtr('importPlaceholder');
      const searchInput=document.getElementById('catalogSearchInput'); if(searchInput) searchInput.placeholder=shtr('searchPlaceholder');
      // Catalog sub-tab labels
      var isZh = currentLang === 'zh';
      set('catalogSubTabAll', isZh ? '\u5168\u90e8' : 'All');
      set('catalogSubTabSkill', 'Skill');
      set('catalogSubTabMCP', 'MCP');
      set('catalogSubTabWorkflow', isZh ? '\u5ba1\u6279\u5de5\u4f5c\u6d41' : 'Workflow');
      set('catalogSubTabApp', 'MaClaw APP');
    }
        let skillhubPage=1;const skillhubPageSize=20;let _skillhubSkills=[];
    function fmtDate(d){if(!d)return'-';try{const t=new Date(d);if(isNaN(t))return d;return t.toLocaleDateString(currentLang==='zh'?'zh-CN':'en-US',{year:'numeric',month:'2-digit',day:'2-digit'})+' '+t.toLocaleTimeString(currentLang==='zh'?'zh-CN':'en-US',{hour:'2-digit',minute:'2-digit'})}catch(e){return d}}
    function classifySkillCategory(s) {
      if (s.capability_type === 'mcp') return 'mcp';
      if (s.capability_type === 'approval_workflow') return 'workflow';
      if (s.is_maclaw_app || s.product_kind === 'maclaw_app_skill') {
        var cat = String(s.maclaw_app_category || '').toLowerCase();
        if (cat.indexOf('approval') !== -1 || cat.indexOf('workflow') !== -1 || cat === 'enterprise_approval_app') return 'workflow';
        return 'app';
      }
      return 'skill';
    }
    function skillCategoryBadge(cat) {
      var map = { skill: ['info', 'SKILL'], mcp: ['info', 'MCP'], workflow: ['warn', '\u5ba1\u6279\u5de5\u4f5c\u6d41'], app: ['ok', 'APP'] };
      var pair = map[cat] || map.skill;
      return '<span class="badge ' + pair[0] + '" style="font-size:9px;padding:3px 7px;font-weight:700">' + escapeHtml(pair[1]) + '</span>';
    }
    function renderSkillHubCard(s, idx) {
      var cat = classifySkillCategory(s);
      // Prefer human-readable display name over internal ID-like name
      var rawName = s.maclaw_app_name || s.display_name || s.name || '-';
      // If name looks like a skill_id format (publisher.skill-name): alphanumeric with dots, 2+ dot-separated parts
      if (/^[a-z0-9_.-]+\.[a-z0-9_.-]+$/i.test(rawName) && rawName === (s.skill_id || s.name)) {
        var parts = rawName.split('.');
        rawName = parts[parts.length - 1];
      }
      // If name still looks like an internal ID (long hex/uuid or enterprise_hub_ prefix), try description first clause
      if (/^[a-f0-9-]{20,}$/.test(rawName) || /^enterprise_hub[_.]/.test(rawName)) {
        var descFallback = (s.description || '').split(/[,.;:，。；：\n]/)[0].trim();
        if (descFallback && descFallback.length <= 60) rawName = descFallback;
        else if (descFallback && descFallback.length > 60) rawName = descFallback.substring(0, 50) + '…';
      }
      var name = escapeHtml(rawName);
      // Show skill_id or internal id below the name (secondary info)
      var skillIdText = s.skill_id || (s.id && s.id !== rawName ? s.id : '');
      var idLine = skillIdText ? '<div class="mp-cap-card-id" title="ID: ' + escapeHtml(skillIdText) + '">' + escapeHtml(skillIdText) + '</div>' : '';
      var verTag = (s.semver || s.version) ? '<span class="mp-card-version">v' + escapeHtml(s.semver || s.version) + '</span>' : '';
      var idArg = jsArg(s.id);
      var catBadge = skillCategoryBadge(cat);
      // Description (prominently displayed, with fallback)
      var description = s.description ? escapeHtml(s.description) : '<span style="opacity:.45">' + escapeHtml(shtr('noDescription')) + '</span>';
      // Source + status meta line (concise)
      var trustLabels = {trusted: currentLang==='zh'?'可信':'Trusted', official: currentLang==='zh'?'官方':'Official', builtin: currentLang==='zh'?'内置':'Built-in', community: currentLang==='zh'?'社区':'Community', 'agent-created': 'Agent'};
      var trustLabel = trustLabels[s.trust_level] || s.trust_level || shtr('unknown');
      var sourceLabel = escapeHtml(s.source || 'SkillHub');
      var statusBadgeClass = s.visible === false ? 'warn' : 'ok';
      var statusLabel = s.visible === false ? shtr('hidden') : shtr('visible');
      var metaLine = sourceLabel + ' | ' + escapeHtml(currentLang==='zh' ? '信任：' + trustLabel : 'Trust: ' + trustLabel) + ' | <span class="badge-inline ' + statusBadgeClass + '">' + escapeHtml(statusLabel) + '</span>';
      // Author + updated time
      var author = s.author || s.uploader_email || '';
      var updatedAt = s.updated_at || s.created_at || '';
      // Stats row
      var statsItems = [];
      if (author) statsItems.push('<span class="mp-card-stat">' + shtr('author') + ': ' + escapeHtml(author) + '</span>');
      if (s.downloads) statsItems.push('<span class="mp-card-stat">' + shtr('downloads') + ': <strong>' + escapeHtml(String(s.downloads)) + '</strong></span>');
      if (s.avg_rating) statsItems.push('<span class="mp-card-stat">' + shtr('rating') + ': <strong>' + Number(s.avg_rating).toFixed(1) + '</strong></span>');
      if (updatedAt) statsItems.push('<span class="mp-card-stat">' + shtr('updated') + ': ' + escapeHtml(fmtDate(updatedAt)) + '</span>');
      var statsHtml = statsItems.length ? '<div class="mp-card-stats">' + statsItems.join('') + '</div>' : '';
      return '<div class="item mp-cap-card" data-category="' + cat + '">' +
        '<div class="mp-cap-card-header">' +
          '<div class="mp-cap-card-title-row"><span class="mp-cap-card-dot"></span><div class="mp-cap-card-name" title="' + name + '">' + name + '</div>' + verTag + '</div>' +
          catBadge +
        '</div>' +
        '<div class="mp-cap-card-desc">' + description + '</div>' +
        idLine +
        '<div class="mp-cap-card-meta">' + metaLine + '</div>' +
        statsHtml +
        '<div class="mp-card-btn-grid">' +
          '<button type="button" class="mp-card-btn mp-card-btn-secondary" onclick="skillhubSetVisibility(' + idArg + ',' + (s.visible === false) + ')">' + (s.visible === false ? shtr('show') : shtr('hide')) + '</button>' +
          '<button type="button" class="mp-card-btn mp-card-btn-ghost" title="' + escapeHtml((currentLang==='zh'?'当前信任等级：':'Current trust level: ') + trustLabel) + '" onclick="skillhubSetTrustLevel(' + idArg + ',' + jsArg(s.trust_level || 'trusted') + ')">' + (currentLang==='zh'?'信任设置':'Trust settings') + '</button>' +
          '<button type="button" class="mp-card-btn mp-card-btn-danger" onclick="skillhubDelete(' + idx + ')">' + shtr('delete') + '</button>' +
        '</div>' +
      '</div>';
    }
    async function loadSkillHubList(page){if(page)skillhubPage=page;try{const data=await api('/api/admin/skillhub/list?page='+skillhubPage+'&page_size='+skillhubPageSize);const skills=data.skills||[];const total=data.total||0;_skillhubSkills=skills;const root=document.getElementById('skillhubList');const totalPages=Math.max(1,Math.ceil(total/skillhubPageSize));if(!skills.length){if(total>0&&skillhubPage>totalPages){skillhubPage=totalPages;return loadSkillHubList(skillhubPage)}root.innerHTML='<div class=\"hint\">'+tr('skillhubEmpty')+'</div>';document.getElementById('skillhubPager').classList.remove('is-visible');return}var activeFilter = typeof catalogActiveSubTab !== 'undefined' ? catalogActiveSubTab : 'all';var filtered = skills;if(activeFilter !== 'all' && activeFilter !== 'mcp'){filtered=skills.filter(function(s){return classifySkillCategory(s)===activeFilter})}if(!filtered.length){root.innerHTML='<div class=\"hint\">'+tr('skillhubEmpty')+'</div>';document.getElementById('skillhubPager').classList.toggle('is-visible',totalPages>1);document.getElementById('skillhubPageInfo').textContent=shtr('page',{current:skillhubPage,total:totalPages});document.getElementById('skillhubPrevBtn').disabled=skillhubPage<=1;document.getElementById('skillhubNextBtn').disabled=skillhubPage>=totalPages;return}root.innerHTML=filtered.map(function(s,idx){var origIdx=skills.indexOf(s);return renderSkillHubCard(s,origIdx)}).join('');document.getElementById('skillhubPager').classList.toggle('is-visible',totalPages>1);document.getElementById('skillhubPageInfo').textContent=shtr('page',{current:skillhubPage,total:totalPages});document.getElementById('skillhubPrevBtn').disabled=skillhubPage<=1;document.getElementById('skillhubNextBtn').disabled=skillhubPage>=totalPages}catch(err){setOutput(shtr('loadFailed',{error:err.message}))}}
    function changeSkillHubPage(delta){loadSkillHubList(skillhubPage+delta)}
    async function skillhubSetVisibility(id,visible){try{await api('/api/admin/skillhub/visibility',{method:'POST',body:JSON.stringify({id,visible})});showToast(shtr('visibilityUpdated'),'success');loadSkillHubList()}catch(err){showToast(err.message,'error')}}
    function skillhubTrustOptions(){return currentLang==='zh'?[['trusted','可信（默认）'],['official','官方'],['community','社区'],['builtin','内置'],['agent-created','Agent 生成']]:[['trusted','Trusted (default)'],['official','Official'],['community','Community'],['builtin','Built-in'],['agent-created','Agent-created']]}
    function skillhubSetTrustLevel(id,currentLevel){
      var options=skillhubTrustOptions();
      var selected=options.some(function(item){return item[0]===currentLevel})?currentLevel:'trusted';
      var previousFocus=document.activeElement;
      var overlay=document.createElement('div');overlay.className='trust-dialog-backdrop';
      var dialog=document.createElement('section');dialog.className='trust-dialog';dialog.setAttribute('role','dialog');dialog.setAttribute('aria-modal','true');dialog.setAttribute('aria-labelledby','trustDialogTitle');dialog.setAttribute('aria-describedby','trustDialogDescription');
      var title=document.createElement('h3');title.id='trustDialogTitle';title.textContent=currentLang==='zh'?'设置信任等级':'Set trust level';
      var description=document.createElement('p');description.id='trustDialogDescription';description.textContent=currentLang==='zh'?'选择该能力在目录和市场中的信任标识。':'Choose the trust indicator shown for this capability.';
      var choices=document.createElement('div');choices.className='trust-dialog-options';
      options.forEach(function(option,index){var label=document.createElement('label');label.className='trust-dialog-option';var input=document.createElement('input');input.type='radio';input.name='trust-level';input.value=option[0];input.checked=option[0]===selected;input.id='trustLevelOption'+index;var text=document.createElement('span');text.textContent=option[1];label.htmlFor=input.id;label.append(input,text);choices.appendChild(label)});
      var actions=document.createElement('div');actions.className='trust-dialog-actions';
      var cancel=document.createElement('button');cancel.type='button';cancel.className='btn-ghost';cancel.textContent=currentLang==='zh'?'取消':'Cancel';
      var confirmButton=document.createElement('button');confirmButton.type='button';confirmButton.className='btn-primary';confirmButton.textContent=currentLang==='zh'?'保存':'Save';
      actions.append(cancel,confirmButton);dialog.append(title,description,choices,actions);overlay.appendChild(dialog);document.body.appendChild(overlay);
      function close(){document.removeEventListener('keydown',onKeyDown);overlay.remove();if(previousFocus&&typeof previousFocus.focus==='function')previousFocus.focus()}
      function onKeyDown(event){if(event.key==='Escape'){event.preventDefault();close()}}
      document.addEventListener('keydown',onKeyDown);overlay.addEventListener('pointerdown',function(event){if(event.target===overlay)close()});cancel.addEventListener('click',close);
      confirmButton.addEventListener('click',async function(){var chosen=dialog.querySelector('input[name="trust-level"]:checked');if(!chosen)return;confirmButton.disabled=true;cancel.disabled=true;var before=confirmButton.textContent;confirmButton.textContent=currentLang==='zh'?'保存中…':'Saving…';try{await api('/api/admin/skillhub/trust-level',{method:'POST',body:JSON.stringify({id:id,trust_level:chosen.value})});showToast(currentLang==='zh'?'信任等级已更新为: '+chosen.parentElement.textContent:'Trust level updated to: '+chosen.parentElement.textContent,'success');close();loadSkillHubList()}catch(err){confirmButton.disabled=false;cancel.disabled=false;confirmButton.textContent=before;showToast(err.message,'error')}});
      var checked=dialog.querySelector('input[name="trust-level"]:checked');if(checked)checked.focus();
    }
    async function skillhubDelete(idx){const s=_skillhubSkills[idx];if(!s)return;const msg=shtr('deleteConfirm',{name:s.name});if(!confirm(msg))return;try{await api('/api/admin/skillhub/'+encodeURIComponent(s.id),{method:'DELETE'});showToast(shtr('deleted'),'success');loadSkillHubList()}catch(err){showToast(err.message,'error')}}
    const MOD_TEXT={en:{title:'LLM Content Moderation',desc:'Configure LLM-based automatic content moderation for the Gossip Wall. When enabled, new gossip posts are checked by the LLM and flagged if inappropriate.',enabled:'Enable Moderation',url:'LLM API URL',model:'Model Name',apiKey:'API Key',urlPlaceholder:'https://api.openai.com/v1',modelPlaceholder:'gpt-4o-mini',apiKeyPlaceholder:'sk-...',testLabel:'Test Content',testPlaceholder:'Enter test content...',emptyTestContent:'Enter test content',save:'Save Moderation Config',test:'Test',saved:'Moderation config saved.',saveFailed:'Save moderation config failed: {error}',loadFailed:'Load moderation config failed: {error}',testResult:'Result: {result}',testFailed:'Test failed: {error}',filterAll:'All Posts',filterFlagged:'Flagged Only',deleteFlagged:'Delete Flagged',deleteFlaggedConfirm:'Delete all flagged gossip posts? This cannot be undone.',flaggedDeleted:'Deleted {count} flagged post(s).',flag:'Flag',unflag:'Unflag',flagged:'Flagged',postFlagged:'Post flagged.',postUnflagged:'Post unflagged.'},zh:{title:'\u5185\u5bb9\u5ba1\u6838',desc:'\u914d\u7f6e\u57fa\u4e8e\u6a21\u578b\u7684\u5410\u69fd\u5899\u81ea\u52a8\u5185\u5bb9\u5ba1\u6838\u3002\u5f00\u542f\u540e\uff0c\u65b0\u53d1\u5e03\u7684\u5410\u69fd\u4f1a\u7ecf\u8fc7\u6a21\u578b\u68c0\u67e5\uff0c\u4e0d\u5408\u89c4\u5185\u5bb9\u4f1a\u81ea\u52a8\u6807\u8bb0\u4e3a\u5ba1\u6838\u72b6\u6001\uff0c\u4e0d\u5728\u524d\u53f0\u5c55\u793a\u3002',enabled:'\u542f\u7528\u5ba1\u6838',url:'\u63a5\u53e3\u5730\u5740',model:'\u6a21\u578b\u540d\u79f0',apiKey:'\u8bbf\u95ee\u5bc6\u94a5',urlPlaceholder:'https://api.openai.com/v1',modelPlaceholder:'gpt-4o-mini',apiKeyPlaceholder:'sk-...',testLabel:'\u6d4b\u8bd5\u5185\u5bb9',testPlaceholder:'\u8bf7\u8f93\u5165\u8981\u68c0\u6d4b\u7684\u5185\u5bb9...',emptyTestContent:'\u8bf7\u8f93\u5165\u6d4b\u8bd5\u5185\u5bb9',save:'\u4fdd\u5b58\u5ba1\u6838\u914d\u7f6e',test:'\u6d4b\u8bd5',saved:'\u5ba1\u6838\u914d\u7f6e\u5df2\u4fdd\u5b58\u3002',saveFailed:'\u4fdd\u5b58\u5ba1\u6838\u914d\u7f6e\u5931\u8d25\uff1a{error}',loadFailed:'\u52a0\u8f7d\u5ba1\u6838\u914d\u7f6e\u5931\u8d25\uff1a{error}',testResult:'\u7ed3\u679c\uff1a{result}',testFailed:'\u6d4b\u8bd5\u5931\u8d25\uff1a{error}',filterAll:'\u5168\u90e8\u5e16\u5b50',filterFlagged:'\u4ec5\u770b\u5ba1\u6838',deleteFlagged:'\u5220\u9664\u5df2\u5ba1\u6838',deleteFlaggedConfirm:'\u786e\u5b9a\u5220\u9664\u6240\u6709\u5df2\u5ba1\u6838\u5410\u69fd\u5e16\u5417\uff1f\u6b64\u64cd\u4f5c\u4e0d\u53ef\u64a4\u9500\u3002',flaggedDeleted:'\u5df2\u5220\u9664 {count} \u6761\u5df2\u5ba1\u6838\u5410\u69fd\u3002',flag:'\u6807\u8bb0\u5ba1\u6838',unflag:'\u53d6\u6d88\u5ba1\u6838',flagged:'\u5df2\u5ba1\u6838',postFlagged:'\u5e16\u5b50\u5df2\u6807\u8bb0\u4e3a\u5ba1\u6838\u3002',postUnflagged:'\u5e16\u5b50\u5df2\u53d6\u6d88\u5ba1\u6838\u6807\u8bb0\u3002'}};
        const mtr=key=>(MOD_TEXT[currentLang]||MOD_TEXT.en)[key]||MOD_TEXT.en[key]||key;
    let gossipFilter='';
    function setGossipFilter(f){gossipFilter=f;document.getElementById('gossipFilterAll').className=f===''?'btn-secondary':'btn-ghost';document.getElementById('gossipFilterFlagged').className=f==='flagged'?'btn-secondary':'btn-ghost';document.getElementById('gossipFilterAll').setAttribute('aria-pressed',f===''?'true':'false');document.getElementById('gossipFilterFlagged').setAttribute('aria-pressed',f==='flagged'?'true':'false');gossipPage=1;loadGossipList()}
    // Override loadGossipList to support filter
    const _origLoadGossipList=loadGossipList;
    loadGossipList=async function(page){if(page)gossipPage=page;try{const filterParam=gossipFilter?'&filter='+gossipFilter:'';const data=await api('/api/admin/gossip?page='+gossipPage+filterParam);const posts=data.posts||[];const total=data.total||0;const root=document.getElementById('gossipList');const totalPages=Math.max(1,Math.ceil(total/gossipPageSize));if(!posts.length){if(total>0&&gossipPage>totalPages){gossipPage=totalPages;return loadGossipList(gossipPage)}root.innerHTML='<div class="hint">'+tr('gossipEmpty')+'</div>';document.getElementById('gossipPager').classList.remove('is-visible');return}root.innerHTML=posts.map(p=>{const flagBadge=p.flagged?` <span class="badge danger">${mtr('flagged')}</span>`:'';const nickname=escapeHtml(p.nickname||gtr('unknownUser'));const category=escapeHtml(p.category||'-');const content=escapeHtml(p.content||'');const machine=escapeHtml(p.machine_id||gtr('na'));const email=escapeHtml(p.user_email||gtr('na'));const createdAt=escapeHtml(p.created_at||'-');const postId=jsArg(p.id);const commentsId=escapeHtml(gossipCommentRootID(p.id));return `<div class="item gossip-card ${p.flagged?'is-flagged':''}"><div class="item-head"><div class="gossip-title"><span class="item-title gossip-name" title="${nickname}">${nickname}</span><span class="badge ${p.locked?'warn':'info'}">${p.locked?gtr('locked'):gtr('open')}</span>${flagBadge}</div><span class="badge info">${category}</span></div><div class="item-meta gossip-text" title="${content}">${content||'-'}</div><div class="item-meta mono gossip-meta"><span>${gtr('id')}: ${escapeHtml(p.id)}</span><span>${gtr('machine')}: ${machine}</span><span>${gtr('email')}: ${email}</span><span>${gtr('score')}: ${Number(p.score||0)}</span><span>${gtr('votes')}: ${Number(p.votes||0)}</span></div><div class="item-meta mono gossip-detail" title="${createdAt}">${createdAt}</div><div class="actions"><button class="btn-ghost" onclick="loadGossipComments(${postId})">${gtr('comments')}</button><button class="btn-secondary" onclick="toggleGossipLock(${postId},${!p.locked})">${p.locked?gtr('unlock'):gtr('lock')}</button><button class="btn-${p.flagged?'secondary':'danger'}" onclick="toggleGossipFlag(${postId},${!p.flagged})">${p.flagged?mtr('unflag'):mtr('flag')}</button><button class="btn-danger" onclick="deleteGossipPost(${postId})">${gtr('delete')}</button></div><div id="${commentsId}" class="list gossip-comment-list"></div></div>`}).join('');document.getElementById('gossipPager').classList.add('is-visible');document.getElementById('gossipPageInfo').textContent=gtr('page',{current:gossipPage,total:totalPages});document.getElementById('gossipPrevBtn').disabled=gossipPage<=1;document.getElementById('gossipNextBtn').disabled=gossipPage>=totalPages}catch(err){setOutput(gtr('loadFailed',{error:err.message}))}};
    async function deleteFlaggedGossipPosts(){if(!confirm(mtr('deleteFlaggedConfirm')))return;const btn=document.getElementById('deleteFlaggedGossipBtn');const prev=btn?btn.textContent:'';if(btn)btn.disabled=true;try{const data=await api('/api/admin/gossip/flagged',{method:'DELETE'});showToast(mtr('flaggedDeleted').replace('{count}',Number(data.deleted||0)),'success');if(gossipFilter!=='flagged')gossipFilter='flagged';setGossipFilter('flagged')}catch(err){showToast(err.message,'error')}finally{if(btn){btn.disabled=false;btn.textContent=prev||mtr('deleteFlagged')}}}
    async function toggleGossipFlag(id,flagged){try{await api('/api/admin/gossip/flag',{method:'POST',body:JSON.stringify({id,flagged})});showToast(flagged?mtr('postFlagged'):mtr('postUnflagged'),'success');loadGossipList()}catch(err){showToast(err.message,'error')}}
    async function loadModerationConfig(){try{const data=await api('/api/admin/moderation/config');document.getElementById('moderationEnabled').checked=!!data.enabled;document.getElementById('moderationUrl').value=data.url||'';document.getElementById('moderationModel').value=data.model||'';document.getElementById('moderationApiKey').value=data.api_key||''}catch(err){setOutput(mtr('loadFailed').replace('{error}',err.message))}}
    async function saveModerationConfig(){try{await api('/api/admin/moderation/config',{method:'POST',body:JSON.stringify({enabled:document.getElementById('moderationEnabled').checked,url:document.getElementById('moderationUrl').value.trim(),model:document.getElementById('moderationModel').value.trim(),api_key:document.getElementById('moderationApiKey').value})});showToast(mtr('saved'),'success')}catch(err){showToast(mtr('saveFailed').replace('{error}',err.message),'error')}}
    async function testModeration(){const content=document.getElementById('moderationTestContent').value.trim();if(!content){showToast(mtr('emptyTestContent'),'error');return}const el=document.getElementById('moderationTestResult');el.textContent='...';el.className='data-row-meta';try{const data=await api('/api/admin/moderation/test',{method:'POST',body:JSON.stringify({content})});el.textContent=mtr('testResult').replace('{result}',data.result);el.className='data-row-meta '+(data.flagged?'error':'ok')}catch(err){el.textContent=mtr('testFailed').replace('{error}',err.message);el.className='data-row-meta error'}}
    function applyModerationI18n(){document.getElementById('moderationCardTitle').textContent=mtr('title');document.getElementById('moderationCardDesc').textContent=mtr('desc');document.getElementById('moderationEnabledLabel').textContent=mtr('enabled');document.getElementById('moderationUrlLabel').textContent=mtr('url');document.getElementById('moderationModelLabel').textContent=mtr('model');document.getElementById('moderationApiKeyLabel').textContent=mtr('apiKey');document.getElementById('moderationTestLabel').textContent=mtr('testLabel');document.getElementById('saveModerationButton').textContent=mtr('save');document.getElementById('testModerationButton').textContent=mtr('test');document.getElementById('gossipFilterAll').textContent=mtr('filterAll');document.getElementById('gossipFilterFlagged').textContent=mtr('filterFlagged');document.getElementById('deleteFlaggedGossipBtn').textContent=mtr('deleteFlagged');document.getElementById('moderationUrl').placeholder=mtr('urlPlaceholder');document.getElementById('moderationModel').placeholder=mtr('modelPlaceholder');document.getElementById('moderationApiKey').placeholder=mtr('apiKeyPlaceholder');document.getElementById('moderationTestContent').placeholder=mtr('testPlaceholder')}
    // Hook into applyI18n and refreshAll
    const _baseApplyI18n2=applyI18n;applyI18n=function(){_baseApplyI18n2();applyModerationI18n();};
    const _baseRefreshAll2=refreshAll;refreshAll=async function(){await Promise.all([_baseRefreshAll2(),loadModerationConfig()])};
    applyModerationI18n();
    if(token())loadModerationConfig();
    // News Management
    const NEWS_UI_TEXT={en:{edit:'Edit',delete:'Delete',saved:'Saved',deleted:'Deleted',deleteConfirm:'Delete this article?',pinBadge:'PIN',page:'Page {current} / {total}',category:{notice:'Notice',update:'Update',tip:'Tip',alert:'Alert'}},zh:{edit:'\u7f16\u8f91',delete:'\u5220\u9664',saved:'\u5df2\u4fdd\u5b58',deleted:'\u5df2\u5220\u9664',deleteConfirm:'\u786e\u5b9a\u5220\u9664\u8fd9\u7bc7\u6d88\u606f\u5417\uff1f',pinBadge:'\u7f6e\u9876',page:'\u7b2c {current} / {total} \u9875',category:{notice:'\u901a\u77e5',update:'\u66f4\u65b0',tip:'\u6280\u5de7',alert:'\u63d0\u9192'}}};
    const ntr=(key,vars={})=>{const lang=NEWS_UI_TEXT[currentLang]||NEWS_UI_TEXT.en;const value=key.split('.').reduce((v,k)=>v&&v[k],lang) ?? key;return typeof value==='string'?value.replace(/\{(\w+)\}/g,(_,n)=>vars[n]??''):value;};
    const catIcons={notice:'N',update:'U',tip:'T',alert:'!'};
    function applyNewsEditorI18n(){
      const category=document.getElementById('newsEditCategory');
      const titleInput=document.getElementById('newsEditTitle');
      const contentInput=document.getElementById('newsEditContent');
      if(titleInput) titleInput.placeholder=tr('newsFieldTitle');
      if(contentInput) contentInput.placeholder=tr('newsEditorPlaceholder');
      if(!category) return;
      const notice=category.querySelector('option[value="notice"]');
      const update=category.querySelector('option[value="update"]');
      const tip=category.querySelector('option[value="tip"]');
      const alert=category.querySelector('option[value="alert"]');
      if(notice) notice.textContent='[N] '+ntr('category.notice');
      if(update) update.textContent='[U] '+ntr('category.update');
      if(tip) tip.textContent='[T] '+ntr('category.tip');
      if(alert) alert.textContent='[!] '+ntr('category.alert');
    }
    const _baseApplyI18nNews=applyI18n;
    applyI18n=function(){_baseApplyI18nNews();applyNewsEditorI18n();applySkillHubI18n();};
    applyNewsEditorI18n();
    applySkillHubI18n();
    let newsPage=1;const newsPageSize=20;let _newsArticles=[];
    async function loadNewsList(page){
      if(page)newsPage=page;
      try{const data=await api('/api/admin/news?offset='+((newsPage-1)*newsPageSize)+'&limit='+newsPageSize);const root=document.getElementById('newsList');const articles=data.articles||[];_newsArticles=articles;const total=data.total||0;const pager=document.getElementById('newsPager');const totalPages=Math.max(1,Math.ceil(total/newsPageSize));
      if(!articles.length){if(total>0&&newsPage>totalPages){newsPage=totalPages;return loadNewsList(newsPage)}root.innerHTML='<div class="hint">'+tr('newsEmpty')+'</div>';if(pager)pager.classList.remove('is-visible');return}
      root.innerHTML=articles.map((a,idx)=>{const icon=catIcons[a.category]||'N';const title=escapeHtml(a.title||'-');const categoryLabel=ntr('category.' + (a.category||'notice'));const pinIcon=a.pinned?'<span class="news-pin-icon" title="'+escapeHtml(ntr('pinBadge'))+'">&#x1F4CC;</span> ':'';const pinTag=a.pinned?'<span class="badge ok pin-badge">'+escapeHtml(ntr('pinBadge'))+'</span>':'';const rawContent=String(a.content||'').replaceAll('\r',' ').replaceAll('\n',' ');const previewText=rawContent.length>120?rawContent.slice(0,120)+'...':rawContent;const preview=escapeHtml(previewText||'-');const fullPreview=escapeHtml(rawContent.length>500?rawContent.slice(0,500)+'...':rawContent||'');const time=a.created_at?new Date(a.created_at).toLocaleString():'';const metaLine=(categoryLabel||'-')+(time?' | '+time:'');
      return `<div class="item news-card${a.pinned?' is-pinned':''}"><div class="item-head"><div class="min-0"><div class="item-title news-title" title="[${icon}] ${title}">${pinIcon}[${icon}] ${title}</div><div class="item-meta news-meta" title="${escapeHtml(metaLine)}">${escapeHtml(metaLine)}</div></div>${pinTag}</div><div class="item-meta news-preview section-gap-sm" title="${fullPreview}">${preview}</div><div class="actions stack-gap-sm"><button class="btn-secondary" onclick="editNewsIndex(${idx})">${ntr('edit')}</button><button class="btn-danger" onclick="deleteNews(${jsArg(a.id)})">${ntr('delete')}</button></div></div>`;}).join('');
      if(pager)pager.classList.toggle('is-visible',totalPages>1);
      const info=document.getElementById('newsPageInfo');if(info)info.textContent=ntr('page',{current:newsPage,total:totalPages});
      const prev=document.getElementById('newsPrevBtn');if(prev)prev.disabled=newsPage<=1;
      const next=document.getElementById('newsNextBtn');if(next)next.disabled=newsPage>=totalPages}catch(err){showToast(err.message,'error')}
    }
    function changeNewsPage(delta){loadNewsList(newsPage+delta)}
function showNewsEditor(){document.getElementById('newsEditor').classList.add('is-visible');document.getElementById('newsEditId').value='';document.getElementById('newsEditTitle').value='';document.getElementById('newsEditContent').value='';document.getElementById('newsEditCategory').value='notice';document.getElementById('newsEditPinned').checked=false}
    function hideNewsEditor(){document.getElementById('newsEditor').classList.remove('is-visible')}
    function editNewsIndex(idx){const a=_newsArticles[idx];if(!a)return;document.getElementById('newsEditor').classList.add('is-visible');document.getElementById('newsEditId').value=a.id;document.getElementById('newsEditTitle').value=a.title||'';document.getElementById('newsEditContent').value=a.content||'';document.getElementById('newsEditCategory').value=a.category||'notice';document.getElementById('newsEditPinned').checked=!!a.pinned}
    async function saveNews(){const id=document.getElementById('newsEditId').value;const body={title:document.getElementById('newsEditTitle').value,content:document.getElementById('newsEditContent').value,category:document.getElementById('newsEditCategory').value,pinned:document.getElementById('newsEditPinned').checked};
      try{if(id){await api('/api/admin/news?id='+encodeURIComponent(id),{method:'PUT',body:JSON.stringify(body)})}else{await api('/api/admin/news',{method:'POST',body:JSON.stringify(body)})}hideNewsEditor();showToast(ntr('saved'),'success');loadNewsList(newsPage)}catch(err){showToast(err.message,'error')}}
    async function deleteNews(id){if(!confirm(ntr('deleteConfirm')))return;try{await api('/api/admin/news?id='+encodeURIComponent(id),{method:'DELETE'});showToast(ntr('deleted'),'success');loadNewsList(newsPage)}catch(err){showToast(err.message,'error')}}

    let gossipListInFlight=null,gossipListInFlightKey='';
    const _finalLoadGossipList=loadGossipList;
    loadGossipList=function(page){const targetPage=page||gossipPage;const key=targetPage+'|'+(gossipFilter||'');if(gossipListInFlight&&gossipListInFlightKey===key)return gossipListInFlight;gossipListInFlightKey=key;gossipListInFlight=Promise.resolve(_finalLoadGossipList(page));return gossipListInFlight.finally(()=>{gossipListInFlight=null;gossipListInFlightKey=''})};
    let skillhubListInFlight=null,skillhubListInFlightKey='';
    const _finalLoadSkillHubList=loadSkillHubList;
    loadSkillHubList=function(page){const key=String(page||skillhubPage);if(skillhubListInFlight&&skillhubListInFlightKey===key)return skillhubListInFlight;skillhubListInFlightKey=key;skillhubListInFlight=Promise.resolve(_finalLoadSkillHubList(page));return skillhubListInFlight.finally(()=>{skillhubListInFlight=null;skillhubListInFlightKey=''})};
    let moderationConfigInFlight=null;
    const _finalLoadModerationConfig=loadModerationConfig;
    loadModerationConfig=function(){if(moderationConfigInFlight)return moderationConfigInFlight;moderationConfigInFlight=Promise.resolve(_finalLoadModerationConfig());return moderationConfigInFlight.finally(()=>{moderationConfigInFlight=null})};
    let newsListInFlight=null,newsListInFlightKey='';
    const _finalLoadNewsList=loadNewsList;
    loadNewsList=function(page){const key=String(page||newsPage);if(newsListInFlight&&newsListInFlightKey===key)return newsListInFlight;newsListInFlightKey=key;newsListInFlight=Promise.resolve(_finalLoadNewsList(page));return newsListInFlight.finally(()=>{newsListInFlight=null;newsListInFlightKey=''})};

    if(token()){
      if(document.getElementById('tab-gossip')?.classList.contains('active')) setTimeout(function(){loadGossipList(gossipPage);loadModerationConfig();},0);
      if(document.getElementById('tab-skillhub')?.classList.contains('active')){window._skillhubAdminLoaded=true;setTimeout(function(){if(typeof reloadCurrentCatalogSubTab==='function')reloadCurrentCatalogSubTab();else loadSkillHubList(skillhubPage);},0);}
      if(document.getElementById('tab-news')?.classList.contains('active')) setTimeout(function(){loadNewsList(newsPage);},0);
    }
