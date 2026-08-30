// HubCenter user rankings tab. Token usage is client-reported actual LLM usage,
// including user-owned providers configured in the MaClaw GUI.
(function(){
  const state={period:'daily',date:'',month:'',year:'',dimension:'all',hubID:'',tenantID:''};
  let hubsLoaded=false;
  let hubItems=[];

  Object.assign(I18N_EN,{
    navUserRankings:'User Rankings',navUserRankingsDesc:'Client token and online time leaders',
    userRankingsTabTitle:'User Rankings',userRankingsTabSubtitle:'Rank users by MaClaw GUI reported token usage and session online time across Hubs and tenants.',
    userRankingsTitle:'User Rankings',userRankingsDesc:'Token totals come from actual client usage reports, including user-configured LLM providers.',
    userRankingsReload:'Reload',userRankingsGlobalTitle:'Global Top 10',userRankingsGlobalDesc:'Top users across every Hub and tenant.',
    userRankingsFiltersTitle:'Filtered Scope',userRankingsFiltersDesc:'Limit the ranking to one Hub or tenant.',
    userRankingsHub:'Hub',userRankingsTenant:'Tenant',userRankingsAllHubs:'All Hubs',userRankingsAllTenants:'All Tenants',
    userRankingsPeriod:'Period',userRankingsDaily:'Daily',userRankingsMonthly:'Monthly',userRankingsYearly:'Yearly',
    userRankingsDate:'Date',userRankingsMonth:'Month',userRankingsYear:'Year',userRankingsDimension:'Dimension',
    userRankingsAll:'All',userRankingsTokens:'Tokens',userRankingsDuration:'Online Time',userRankingsQuery:'Query',
    userRankingsTokenTop:'Token Top 10',userRankingsDurationTop:'Online Time Top 10',
    userRankingsEmail:'Email',userRankingsHubCol:'Hub',userRankingsTenantCol:'Tenant',userRankingsTokenRank:'Token Rank',
    userRankingsDurationRank:'Time Rank',userRankingsTokenTotal:'Tokens',userRankingsDurationTotal:'Online Time',
    userRankingsGenerated:'Generated at {time}',userRankingsEmpty:'No ranking data for this scope.',
    userRankingsLoading:'Loading rankings...',userRankingsLoadFailed:'Load user rankings failed: {error}'
  });
  Object.assign(I18N_ZH,{
    navUserRankings:'\u7528\u6237\u6392\u884c',navUserRankingsDesc:'\u5ba2\u6237\u7aef Token \u4e0e\u5728\u7ebf\u65f6\u957f',
    userRankingsTabTitle:'\u7528\u6237\u6392\u884c',userRankingsTabSubtitle:'\u6309 MaClaw GUI \u4e0a\u62a5\u7684\u5b9e\u9645 Token \u7528\u91cf\u4e0e\u4f1a\u8bdd\u5728\u7ebf\u65f6\u957f\u5bf9\u7528\u6237\u6392\u540d\u3002',
    userRankingsTitle:'\u7528\u6237\u6392\u884c',userRankingsDesc:'Token \u6765\u81ea\u5ba2\u6237\u7aef\u5b9e\u9645\u4f7f\u7528\u4e0a\u62a5\uff0c\u5305\u542b\u7528\u6237\u81ea\u884c\u914d\u7f6e\u7684 LLM \u670d\u52a1\u5546\u3002',
    userRankingsReload:'\u91cd\u65b0\u52a0\u8f7d',userRankingsGlobalTitle:'\u5168\u5c40 Top 10',userRankingsGlobalDesc:'\u6240\u6709 Hub \u548c\u79df\u6237\u4e0b\u7684\u7528\u6237\u6392\u884c\u3002',
    userRankingsFiltersTitle:'\u7b5b\u9009\u8303\u56f4',userRankingsFiltersDesc:'\u6309 Hub \u6216\u79df\u6237\u7f29\u5c0f\u6392\u884c\u8303\u56f4\u3002',
    userRankingsHub:'Hub',userRankingsTenant:'\u79df\u6237',userRankingsAllHubs:'\u5168\u90e8 Hub',userRankingsAllTenants:'\u5168\u90e8\u79df\u6237',
    userRankingsPeriod:'\u5468\u671f',userRankingsDaily:'\u6309\u65e5',userRankingsMonthly:'\u6309\u6708',userRankingsYearly:'\u6309\u5e74',
    userRankingsDate:'\u65e5\u671f',userRankingsMonth:'\u6708\u4efd',userRankingsYear:'\u5e74\u4efd',userRankingsDimension:'\u7ef4\u5ea6',
    userRankingsAll:'\u5168\u90e8',userRankingsTokens:'Token \u91cf',userRankingsDuration:'\u5728\u7ebf\u65f6\u957f',userRankingsQuery:'\u67e5\u8be2',
    userRankingsTokenTop:'Token Top 10',userRankingsDurationTop:'\u5728\u7ebf\u65f6\u957f Top 10',
    userRankingsEmail:'Email',userRankingsHubCol:'Hub',userRankingsTenantCol:'\u79df\u6237',userRankingsTokenRank:'Token \u6392\u540d',
    userRankingsDurationRank:'\u65f6\u957f\u6392\u540d',userRankingsTokenTotal:'Token',userRankingsDurationTotal:'\u5728\u7ebf\u65f6\u957f',
    userRankingsGenerated:'\u751f\u6210\u65f6\u95f4 {time}',userRankingsEmpty:'\u5f53\u524d\u8303\u56f4\u6682\u65e0\u6392\u884c\u6570\u636e\u3002',
    userRankingsLoading:'\u6b63\u5728\u52a0\u8f7d\u6392\u884c...',userRankingsLoadFailed:'\u52a0\u8f7d\u7528\u6237\u6392\u884c\u5931\u8d25: {error}'
  });

  function t(key,vars){return typeof tr==='function'?tr(key,vars||{}):key}
  function esc(value){return typeof escapeHtml==='function'?escapeHtml(value):String(value==null?'':value).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
  function today(){return new Date().toISOString().slice(0,10)}
  function thisMonth(){return new Date().toISOString().slice(0,7)}
  function thisYear(){return String(new Date().getUTCFullYear())}
  function fmtInt(value){return Number(value||0).toLocaleString(currentLang==='zh'?'zh-CN':'en-US')}
  function fmtDuration(seconds){const total=Math.max(0,Number(seconds||0));const h=Math.floor(total/3600);const m=Math.floor((total%3600)/60);return h>0?h+'h '+m+' Min':m+' Min'}
  function labelHub(row){return row.hub_name||row.hub_id||'-'}
  function labelTenant(id){return id==='tenant_default'?t('defaultTenant'):(id||'-')}

  function ensureShell(){
    ensureNav();
    ensurePanel();
    if(!state.date)state.date=today();
    if(!state.month)state.month=thisMonth();
    if(!state.year)state.year=thisYear();
    syncControls();
    applyUserRankingsI18n();
  }
  function ensureNav(){
    if(document.querySelector('.nav button[data-tab="userrankings"]'))return;
    const nav=document.querySelector('.nav');
    if(!nav)return;
    const btn=document.createElement('button');
    btn.type='button';btn.dataset.tab='userrankings';btn.setAttribute('onclick',"openTab('userrankings')");
    btn.innerHTML='<span class="nav-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M4 19h16"></path><path d="M7 16V9"></path><path d="M12 16V5"></path><path d="M17 16v-4"></path><path d="M6 20h12"></path></svg></span><span data-i18n="navUserRankings"></span><small data-i18n="navUserRankingsDesc"></small>';
    const before=nav.querySelector('button[data-tab="news"],button[data-tab="failurelogs"],button[data-tab="system"]');
    const group=(before&&before.closest('.nav-group'))||nav.querySelector('.nav-group[data-nav-group="content"]')||nav;
    group.insertBefore(btn, before || null);
    if (typeof syncNavGroups === 'function') syncNavGroups();
  }
  function ensurePanel(){
    if(document.getElementById('tab-userrankings'))return;
    const main=document.querySelector('main.main');
    if(!main)return;
    const section=document.createElement('section');
    section.id='tab-userrankings';section.className='panel card';
    section.innerHTML='<div class="head"><div><h3 data-i18n="userRankingsTitle"></h3><div class="desc" data-i18n="userRankingsDesc"></div></div><button class="btn-ghost" type="button" onclick="loadUserRankings()" data-i18n="userRankingsReload"></button></div>'
      +'<div class="item user-rankings-global"><div class="head meta-spaced"><div><div class="item-title" data-icon="chart" data-i18n="userRankingsGlobalTitle"></div><div class="item-meta" data-i18n="userRankingsGlobalDesc"></div></div></div><div id="urGlobal" class="user-rankings-columns" role="status" aria-live="polite"></div></div>'
      +'<div class="item section-gap-lg"><div class="head meta-spaced"><div><div class="item-title" data-icon="sliders" data-i18n="userRankingsFiltersTitle"></div><div class="item-meta" data-i18n="userRankingsFiltersDesc"></div></div><button class="btn-primary" type="button" onclick="loadUserRankings()" data-i18n="userRankingsQuery"></button></div><div class="user-rankings-filter-grid">'
      +'<div><label for="urHub" data-i18n="userRankingsHub"></label><select id="urHub" onchange="onUserRankingHubChange()"></select></div>'
      +'<div><label for="urTenant" data-i18n="userRankingsTenant"></label><select id="urTenant" onchange="onUserRankingFilterChange()"></select></div>'
      +'<div><label for="urPeriod" data-i18n="userRankingsPeriod"></label><select id="urPeriod" onchange="onUserRankingPeriodChange()"><option value="daily" data-i18n="userRankingsDaily"></option><option value="monthly" data-i18n="userRankingsMonthly"></option><option value="yearly" data-i18n="userRankingsYearly"></option></select></div>'
      +'<div id="urDateWrap"><label for="urDate" data-i18n="userRankingsDate"></label><input id="urDate" type="date" onchange="onUserRankingFilterChange()"></div>'
      +'<div id="urMonthWrap"><label for="urMonth" data-i18n="userRankingsMonth"></label><input id="urMonth" type="month" onchange="onUserRankingFilterChange()"></div>'
      +'<div id="urYearWrap"><label for="urYear" data-i18n="userRankingsYear"></label><input id="urYear" type="number" min="1970" max="9999" onchange="onUserRankingFilterChange()"></div>'
      +'<div><label for="urDimension" data-i18n="userRankingsDimension"></label><select id="urDimension" onchange="onUserRankingFilterChange()"><option value="all" data-i18n="userRankingsAll"></option><option value="tokens" data-i18n="userRankingsTokens"></option><option value="duration" data-i18n="userRankingsDuration"></option></select></div>'
      +'</div><div id="urGeneratedAt" class="item-meta section-gap"></div></div><div id="urFiltered" class="user-rankings-columns section-gap" role="status" aria-live="polite"></div>';
    const before=document.getElementById('tab-news')||document.getElementById('tab-system');
    main.insertBefore(section,before||null);
  }

  function applyUserRankingsI18n(){
    if(typeof applyI18n==='function')applyI18n();
    syncControls();
  }
  function syncControls(){
    const period=document.getElementById('urPeriod');if(period)period.value=state.period;
    const dim=document.getElementById('urDimension');if(dim)dim.value=state.dimension;
    const date=document.getElementById('urDate');if(date)date.value=state.date;
    const month=document.getElementById('urMonth');if(month)month.value=state.month;
    const year=document.getElementById('urYear');if(year)year.value=state.year;
    const dateWrap=document.getElementById('urDateWrap'),monthWrap=document.getElementById('urMonthWrap'),yearWrap=document.getElementById('urYearWrap');
    if(dateWrap)dateWrap.classList.toggle('hidden-view',state.period!=='daily');
    if(monthWrap)monthWrap.classList.toggle('hidden-view',state.period!=='monthly');
    if(yearWrap)yearWrap.classList.toggle('hidden-view',state.period!=='yearly');
  }
  function readControls(){
    state.hubID=(document.getElementById('urHub')||{}).value||'';
    state.tenantID=(document.getElementById('urTenant')||{}).value||'';
    state.period=(document.getElementById('urPeriod')||{}).value||'daily';
    state.dimension=(document.getElementById('urDimension')||{}).value||'all';
    state.date=(document.getElementById('urDate')||{}).value||state.date||today();
    state.month=(document.getElementById('urMonth')||{}).value||state.month||thisMonth();
    state.year=(document.getElementById('urYear')||{}).value||state.year||thisYear();
  }
  async function loadHubsForRankings(){
    if(hubsLoaded)return;
    try{
      const data=await api('/api/admin/hubs');
      hubItems=Array.isArray(data&&data.hubs)?data.hubs:[];
      window._lastHubItems=hubItems;
      hubsLoaded=true;
    }catch(_){
      if(Array.isArray(window._lastHubItems)){
        hubItems=window._lastHubItems;
        hubsLoaded=true;
      }else{
        hubItems=[];
      }
    }
    renderHubOptions();
  }
  function renderHubOptions(){
    const hubSelect=document.getElementById('urHub');
    if(!hubSelect)return;
    const selected=state.hubID||hubSelect.value||'';
    hubSelect.innerHTML='<option value="">'+esc(t('userRankingsAllHubs'))+'</option>'+hubItems.map(h=>'<option value="'+esc(h.id||h.hub_id||'')+'">'+esc(h.name||h.id||h.hub_id||'-')+'</option>').join('');
    hubSelect.value=selected;
    renderTenantOptions();
  }
  function renderTenantOptions(){
    const tenantSelect=document.getElementById('urTenant');
    if(!tenantSelect)return;
    const hubID=(document.getElementById('urHub')||{}).value||'';
    const seen=new Map();
    function add(id,name){id=String(id||'');if(!id||seen.has(id))return;seen.set(id,name||labelTenant(id))}
    const source=hubID?hubItems.filter(h=>String(h.id||h.hub_id||'')===hubID):hubItems;
    source.forEach(h=>{add('tenant_default',t('defaultTenant'));(Array.isArray(h.tenants)?h.tenants:[]).forEach(tenant=>add(tenant.tenant_id,tenant.tenant_name||tenant.tenant_id))});
    tenantSelect.innerHTML='<option value="">'+esc(t('userRankingsAllTenants'))+'</option>'+Array.from(seen.entries()).map(([id,name])=>'<option value="'+esc(id)+'">'+esc(name)+' <'+esc(id)+'></option>').join('');
    tenantSelect.value=seen.has(state.tenantID)?state.tenantID:'';
    state.tenantID=tenantSelect.value;
  }

  function queryParams(dimension){
    const params=new URLSearchParams({period:state.period,dimension:dimension,limit:'10'});
    if(state.hubID)params.set('hub_id',state.hubID);
    if(state.tenantID)params.set('tenant_id',state.tenantID);
    if(state.period==='monthly')params.set('month',state.month||thisMonth());
    else if(state.period==='yearly')params.set('year',state.year||thisYear());
    else params.set('date',state.date||today());
    return params.toString();
  }
  async function loadUserRankings(){
    ensureShell();readControls();syncControls();
    const global=document.getElementById('urGlobal'),filtered=document.getElementById('urFiltered');
    if(global)global.innerHTML='<div class="hint">'+esc(t('userRankingsLoading'))+'</div>';
    if(filtered)filtered.innerHTML='<div class="hint">'+esc(t('userRankingsLoading'))+'</div>';
    try{
      await loadHubsForRankings();
      const dims=state.dimension==='all'?['tokens','duration']:[state.dimension];
      const responses=await Promise.all(dims.map(d=>api('/api/admin/user-rankings?'+queryParams(d)).then(data=>({dimension:d,data:data||{}}))));
      renderRankings(responses);
    }catch(err){
      const msg=t('userRankingsLoadFailed',{error:err.message});
      if(global)global.innerHTML='<div class="hint">'+esc(msg)+'</div>';
      if(filtered)filtered.innerHTML='';
      if(typeof showToast==='function')showToast(msg,'error');
      if(typeof setOutput==='function')setOutput(msg);
    }
  }
  function renderRankings(responses){
    const generated=(responses[0]&&responses[0].data&&responses[0].data.generated_at)||'';
    const gen=document.getElementById('urGeneratedAt');
    if(gen)gen.textContent=generated?t('userRankingsGenerated',{time:new Date(generated).toLocaleString()}):'';
    const global=document.getElementById('urGlobal'),filtered=document.getElementById('urFiltered');
    if(global)global.innerHTML=renderResponseColumns(responses,'global_top',true);
    if(filtered)filtered.innerHTML=renderResponseColumns(responses,'filtered_top',false);
  }
  function renderResponseColumns(responses,key,isGlobal){
    if(state.dimension!=='all'){
      const res=responses[0]||{dimension:state.dimension,data:{}};
      return renderRankingList(res.dimension,(res.data&&res.data[key])||[],isGlobal);
    }
    return responses.map(res=>renderRankingList(res.dimension,(res.data&&res.data[key])||[],isGlobal)).join('');
  }
  function renderRankingList(dimension,rows,isGlobal){
    const title=dimension==='duration'?t('userRankingsDurationTop'):t('userRankingsTokenTop');
    const cls=rows&&rows.length?'':' is-empty';
    return '<div class="user-ranking-list'+cls+'"><div class="user-ranking-list-title"><strong>'+esc(title)+'</strong><span class="badge info">'+esc(rows.length||0)+'</span></div>'
      +(rows&&rows.length?'<div class="user-ranking-table">'+rows.map((row,idx)=>renderRankingRow(row,idx,dimension,isGlobal)).join('')+'</div>':'<div class="hint">'+esc(t('userRankingsEmpty'))+'</div>')+'</div>';
  }
  function renderRankingRow(row,idx,dimension,isGlobal){
    const primaryRank=dimension==='duration'?row.duration_rank:row.token_rank;
    const metric=dimension==='duration'?fmtDuration(row.duration_seconds):fmtInt(row.total_tokens);
    const metricLabel=dimension==='duration'?t('userRankingsDurationTotal'):t('userRankingsTokenTotal');
    return '<div class="user-ranking-row"><div class="user-ranking-rank">#'+esc(primaryRank||idx+1)+'</div><div class="user-ranking-main"><strong title="'+esc(row.user_email||'')+'">'+esc(row.user_email||'-')+'</strong>'
      +(isGlobal?'<span>'+esc(labelHub(row))+' / '+esc(labelTenant(row.tenant_id))+'</span>':'<span>'+esc(metricLabel)+': '+esc(metric)+'</span>')+'</div>'
      +'<div class="user-ranking-num"><strong>'+esc(metric)+'</strong><span>'+esc(t('userRankingsTokens'))+' #'+esc(row.token_rank||'-')+' / '+esc(t('userRankingsDuration'))+' #'+esc(row.duration_rank||'-')+'</span></div></div>';
  }
  function onUserRankingPeriodChange(){readControls();syncControls();loadUserRankings()}
  function onUserRankingFilterChange(){readControls();syncControls();loadUserRankings()}
  function onUserRankingHubChange(){readControls();renderTenantOptions();loadUserRankings()}
  async function initUserRankingsTab(){
    ensureShell();
    await loadHubsForRankings();
    await loadUserRankings();
  }

  window.initUserRankingsTab=initUserRankingsTab;
  window.loadUserRankings=loadUserRankings;
  window.onUserRankingPeriodChange=onUserRankingPeriodChange;
  window.onUserRankingFilterChange=onUserRankingFilterChange;
  window.onUserRankingHubChange=onUserRankingHubChange;
  document.addEventListener('DOMContentLoaded',()=>{ensureShell();if((localStorage.getItem('maclawHubCenterActiveTab')||'')==='userrankings'&&typeof openTab==='function')openTab('userrankings')});
})();
