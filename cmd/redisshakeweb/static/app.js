import { showErrorDialog, showInfoDialog } from './dialog.js?v=feedback-dialog';

const root = document.querySelector('#app');
const state = { page: 'tasks', session: null, adminUsername: 'admin', connections: [], tasks: [], editing: null, task: null, step: 0, checks: [], selectedRun: null, logRun: null, refresh: null };

const iconPaths = {
  database: '<ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v7c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 12v7c0 1.7 3.6 3 8 3s8-1.3 8-3v-7"/>',
  tasks: '<rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 9h8M8 13h8M8 17h5"/>',
  connections: '<circle cx="6" cy="12" r="2"/><circle cx="18" cy="6" r="2"/><circle cx="18" cy="18" r="2"/><path d="m8 11 8-4m-8 6 8 4"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  eye: '<path d="M2 12s3.6-6 10-6 10 6 10 6-3.6 6-10 6S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/>',
  eyeOff: '<path d="m3 3 18 18M10.6 6.1A11 11 0 0 1 12 6c6.4 0 10 6 10 6a16 16 0 0 1-4 4.5M6 6.9C3.4 8.7 2 12 2 12s3.6 6 10 6c1.2 0 2.3-.2 3.3-.5"/><path d="M10 10a3 3 0 0 0 4 4"/>'
};
const icon = name => `<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${iconPaths[name]}</svg>`;
const authBrand = () => `<div class="auth-brand"><span class="brand-mark">${icon('database')}</span><span>RedisShake Web</span></div>`;

const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const lines = value => String(value ?? '').split(/\r?\n/).map(x => x.trim()).filter(Boolean);
const badge = (value) => `<span class="badge ${esc(value)}">${esc({RUNNING:'运行中',STARTING:'启动中',STOPPING:'停止中',STOPPED:'已停止',FAILED:'失败',FULL_SYNC:'全量同步',INCREMENTAL:'增量同步',UNKNOWN:'阶段待识别'}[value] || value || '草稿')}</span>`;
const field = (label, name, value='', type='text', hint='') => {
  const input = `<input id="${esc(name)}" name="${esc(name)}" type="${esc(type)}" value="${esc(value)}" autocomplete="off">`;
  const control = type === 'password' ? `<div class="password-control">${input}<button class="password-toggle" type="button" data-password-toggle="${esc(name)}" aria-label="显示密码" aria-pressed="false" title="显示密码">${icon('eye')}</button></div>` : input;
  return `<div class="field"><label for="${esc(name)}">${esc(label)}</label>${control}${hint ? `<small>${esc(hint)}</small>` : ''}</div>`;
};
const area = (label, name, value='', hint='') => `<div class="field"><label for="${esc(name)}">${esc(label)}</label><textarea id="${esc(name)}" name="${esc(name)}">${esc(value)}</textarea>${hint ? `<small>${esc(hint)}</small>` : ''}</div>`;
const notice = (text, error=false) => {
  if (error) {
    showErrorDialog(text);
    return;
  }
  render();
  showInfoDialog(text);
};
const byId = id => document.getElementById(id);
const on = (id, event, callback) => { const element=byId(id); if(element) element.addEventListener(event, callback); };
const formData = id => Object.fromEntries(new FormData(byId(id)).entries());
document.addEventListener('click', event => {
  const button = event.target.closest('[data-password-toggle]');
  if (!button) return;
  const input = byId(button.dataset.passwordToggle);
  if (!input) return;
  const visible = input.type === 'password';
  input.type = visible ? 'text' : 'password';
  button.setAttribute('aria-label', visible ? '隐藏密码' : '显示密码');
  button.setAttribute('aria-pressed', String(visible));
  button.title = visible ? '隐藏密码' : '显示密码';
  button.innerHTML = icon(visible ? 'eyeOff' : 'eye');
});

async function api(path, options={}) {
  const opts = {...options, credentials:'same-origin', headers:{...(options.body ? {'Content-Type':'application/json'} : {}), ...(state.session && options.method && options.method !== 'GET' ? {'X-CSRF-Token':state.session.csrf} : {}), ...options.headers}};
  const response = await fetch('/api'+path, opts);
  let body;
  try { body = await response.json(); } catch { body = {error:'服务响应无法解析'}; }
  if (!response.ok) { const error=new Error(body.error || `请求失败 (${response.status})`); error.details=body; error.status=response.status; throw error; }
  return body;
}
const send = (path, body, method='POST') => api(path,{method,body:JSON.stringify(body)});

async function boot() {
  try {
    const status = await api('/bootstrap/status');
    if (!status.initialized) { state.page='bootstrap'; render(); return; }
    try { state.session=await api('/me'); state.adminUsername=state.session.username; } catch { state.page='login'; render(); return; }
    await loadLists(); render();
  } catch (error) { root.innerHTML=`<div class="auth-shell"><div class="card auth-card"><h1>暂时无法连接执行器</h1><p>${esc(error.message)}</p><button class="primary" id="retry">重试</button></div></div>`; on('retry','click',boot); }
}

async function loadLists() {
  [state.connections,state.tasks] = await Promise.all([api('/connections'),api('/tasks')]);
}

function shell(title, subtitle, action, content) {
  root.innerHTML=`<div class="layout"><aside class="sidebar"><div class="brand"><span class="brand-mark">${icon('database')}</span><span class="brand-copy">RedisShake Web<small>数据迁移工作台</small></span></div><nav class="nav" aria-label="主导航"><button id="nav-tasks" class="${state.page==='tasks'||state.page==='task'||state.page==='wizard'?'active':''}">${icon('tasks')}<span>同步任务</span></button><button id="nav-connections" class="${state.page==='connections'||state.page==='connection'?'active':''}">${icon('connections')}<span>连接管理</span></button></nav></aside><main class="main"><div class="topline"><div><span class="eyebrow">工作台</span><h1>${esc(title)}</h1><span class="muted">${esc(subtitle)}</span></div>${action||''}</div>${content}</main></div>`;
  on('nav-tasks','click',async()=>{state.page='tasks';await loadLists();render();});
  on('nav-connections','click',async()=>{state.page='connections';await loadLists();render();});
}

function render() {
  if (state.refresh) { clearTimeout(state.refresh); state.refresh=null; }
  if (state.page==='bootstrap') return renderBootstrap();
  if (state.page==='login') return renderLogin();
  if (state.page==='tasks') return renderTasks();
  if (state.page==='connections') return renderConnections();
  if (state.page==='connection') return renderConnectionForm();
  if (state.page==='wizard') return renderWizard();
  if (state.page==='task') return renderTask();
}

function renderBootstrap() {
  root.innerHTML=`<div class="auth-shell"><div class="card auth-card">${authBrand()}<div class="auth-heading"><h1>初始化管理员账号</h1></div><form id="bootstrap-form" class="stack">${field('管理员账号','username',state.adminUsername)}${field('管理员密码','password','RedisShake@123456','password')}${field('再次输入密码','confirm','RedisShake@123456','password')}<button class="primary">创建管理员账号</button></form></div></div>`;
  on('bootstrap-form','submit',async event=>{event.preventDefault();const d=formData('bootstrap-form');state.adminUsername=d.username;if(d.password!==d.confirm)return notice('两次输入的密码不一致',true);try{await send('/bootstrap/init',{username:d.username,password:d.password});state.page='login';notice('管理员账号已创建，请登录');}catch(error){notice(error.message,true);}});
}

function renderLogin() {
  root.innerHTML=`<div class="auth-shell"><div class="card auth-card">${authBrand()}<form id="login-form" class="stack">${field('账号','username',state.adminUsername)}${field('密码','password','','password')}<button class="primary">登录</button></form></div></div>`;
  on('login-form','submit',async event=>{event.preventDefault();const credentials=formData('login-form');state.adminUsername=credentials.username;try{state.session=await send('/login',credentials);await loadLists();state.page='tasks';render();}catch(error){byId('password').value='';notice(error.message,true);}});
}

function renderTasks() {
  shell('同步任务','创建、运行和查看 Redis 数据同步',state.tasks.length ? `<button class="primary" id="new-task">${icon('plus')} 创建任务</button>` : '',
    `<div class="card"><h2>任务列表</h2>${state.tasks.length ? state.tasks.map(t=>`<div class="list-item"><div class="details"><strong>${esc(t.name)} <span data-run-status="${esc(t.id)}" class="badge">加载中</span></strong><span class="muted">${esc(connectionName(t.sourceId))} → ${esc(connectionName(t.targetId))}　·　${esc(t.updatedAt||'未运行')}</span></div><button class="secondary" data-task="${esc(t.id)}">查看详情</button></div>`).join('') : `<div class="empty"><strong>还没有同步任务</strong><p>先创建源端和目标端连接，再用向导配置同步。</p><button class="primary" id="empty-new">创建同步任务</button></div>`}</div>`);
  on('new-task','click',newTask);on('empty-new','click',newTask);
  document.querySelectorAll('[data-task]').forEach(button=>button.addEventListener('click',()=>openTask(button.dataset.task)));
  state.tasks.forEach(async t=>{try{const runs=await api('/tasks/'+t.id+'/runs');const label=document.querySelector(`[data-run-status="${CSS.escape(t.id)}"]`);if(label){const status=runs[0]?.status||'草稿';label.textContent={RUNNING:'运行中',FAILED:'失败',STOPPED:'已停止',STARTING:'启动中',STOPPING:'停止中'}[status]||status;label.className='badge '+status;}}catch{}});
}
function connectionName(id) { return state.connections.find(x=>x.id===id)?.name || '未选择连接'; }
function newTask(){state.editing={name:'',sourceId:'',targetId:'',dbMap:{'0':0},rules:{},targetPolicy:'require_empty'};state.step=0;state.checks=[];state.page='wizard';render();}

function renderConnections() {
  shell('连接管理','保存并复用源端与目标端连接',`<button class="primary" id="new-connection">${icon('plus')} 新建连接</button>`,
    `<div class="card"><h2>已保存的连接</h2>${state.connections.length ? state.connections.map(c=>`<div class="list-item"><div class="details"><strong>${esc(c.name)}</strong><span class="muted">${esc({standalone:'单机',sentinel:'哨兵',cluster:'Cluster'}[c.kind])} · ${esc(c.kind==='sentinel'?c.sentinelAddress:c.address)}</span></div><div class="actions"><button class="secondary" data-edit-connection="${esc(c.id)}">编辑</button><button class="danger" data-delete-connection="${esc(c.id)}">删除</button></div></div>`).join('') : `<div class="empty"><strong>还没有 Redis 连接</strong><p>添加连接后即可在任务向导中选择。</p></div>`}</div>`);
  on('new-connection','click',()=>{state.editing={kind:'standalone'};state.page='connection';render();});
  document.querySelectorAll('[data-edit-connection]').forEach(b=>b.addEventListener('click',()=>{state.editing={...state.connections.find(x=>x.id===b.dataset.editConnection)};state.page='connection';render();}));
  document.querySelectorAll('[data-delete-connection]').forEach(b=>b.addEventListener('click',async()=>{if(!confirm('删除这个连接？已被任务使用的连接无法删除。'))return;try{await api('/connections/'+b.dataset.deleteConnection,{method:'DELETE'});await loadLists();notice('连接已删除');}catch(error){notice(error.message,true);}}));
}

function renderConnectionForm() {
  const c=state.editing||{};
  shell(c.id?'编辑连接':'新建连接',c.id?'密码留空表示保留现有密码':'密码保存后不会回显',`<button class="ghost" id="back-connections">返回连接管理</button>`,
    `<div class="card"><form id="connection-form" class="stack"><div class="grid">${field('连接名称','name',c.name)}<div class="field"><label for="kind">部署类型</label><select name="kind" id="kind"><option value="standalone" ${c.kind==='standalone'?'selected':''}>单机</option><option value="sentinel" ${c.kind==='sentinel'?'selected':''}>哨兵</option><option value="cluster" ${c.kind==='cluster'?'selected':''}>Redis Cluster</option></select></div></div><div id="connection-extra"></div><div class="grid">${field('Redis ACL 用户名','username',c.username)}${field('Redis 密码','password','','password')}</div><div class="actions"><button type="button" class="secondary" id="test-connection">测试连接</button><button class="primary">保存连接</button></div></form><div id="connection-checks"></div></div>`);
  const renderExtra=()=>{const kind=byId('kind').value;byId('connection-extra').innerHTML=kind==='sentinel'?`<div class="grid">${field('Sentinel 地址 host:port','sentinelAddress',c.sentinelAddress)}${field('主节点名称','sentinelMaster',c.sentinelMaster)}${field('Sentinel ACL 用户名','sentinelUsername',c.sentinelUsername)}${field('Sentinel 密码','sentinelPassword','','password')}</div>`:`<div class="grid">${field(kind==='cluster'?'Cluster 入口节点 host:port':'Redis 地址 host:port','address',c.address)}</div>`;};
  renderExtra();on('kind','change',renderExtra);
  on('back-connections','click',()=>{state.page='connections';render();});
  const input=()=>({...formData('connection-form'),id:c.id});
  on('test-connection','click',async()=>{try{const checks=await send('/connections/test',input());byId('connection-checks').innerHTML=checksHTML(checks);}catch(error){notice(error.message,true);}});
  on('connection-form','submit',async event=>{event.preventDefault();const draft=input();state.editing={...c,...draft,password:'',sentinelPassword:''};try{await send(c.id?'/connections/'+c.id:'/connections',draft,c.id?'PUT':'POST');await loadLists();state.page='connections';notice('连接已保存');}catch(error){notice(error.message,true);}});
}

function checksHTML(checks) { return `<div style="margin-top:20px">${checks.map(c=>`<div class="check ${c.ok?'ok':'bad'}"><strong>${c.ok?'✓':'!'} ${esc(c.name)}</strong><div class="muted">${esc(c.message)}</div></div>`).join('')}</div>`; }

function taskConnectionOptions(selected) { return `<option value="">请选择连接</option>`+state.connections.map(c=>`<option value="${esc(c.id)}" ${selected===c.id?'selected':''}>${esc(c.name)} · ${esc(c.kind)}</option>`).join(''); }
function mapText(map) { return Object.entries(map||{}).map(([a,b])=>`${a}:${b}`).join('\n'); }
function parseMap(text){const map={};for(const row of lines(text)){const pair=row.split(':');if(pair.length!==2 || !/^\d+$/.test(pair[0].trim()) || !/^\d+$/.test(pair[1].trim()))throw new Error('DB 映射格式应为 0:0，每行一组');map[pair[0].trim()]=Number(pair[1].trim());}return map;}

function renderWizard() {
  const t=state.editing,r=t.rules||{};
  const steps=['连接','规则','预检','确认'];
  let content='';
  if(state.step===0)content=`<form id="wizard-form" class="stack"><div class="grid">${field('任务名称','name',t.name)}<div></div><div class="field"><label for="sourceId">源连接</label><select name="sourceId" id="sourceId">${taskConnectionOptions(t.sourceId)}</select></div><div class="field"><label for="targetId">目标连接</label><select name="targetId" id="targetId">${taskConnectionOptions(t.targetId)}</select></div></div></form><p class="muted">没有连接？先到“连接管理”保存，返回后继续创建任务。</p>`;
  if(state.step===1)content=`<form id="wizard-form" class="stack">${area('DB 映射','dbMap',mapText(t.dbMap),'每行一组，例如 0:0；不能多个源 DB 指向同一目标 DB。')}<div class="grid">${area('包含的 Key 前缀','allowPrefixes',(r.allowPrefixes||[]).join('\n'),'每行一个；留空表示全部通过。')}${area('排除的 Key 前缀','blockPrefixes',(r.blockPrefixes||[]).join('\n'))}${area('包含的 Key 正则','allowRegex',(r.allowRegex||[]).join('\n'),'每行一个 Go 正则表达式。')}${area('排除的 Key 正则','blockRegex',(r.blockRegex||[]).join('\n'))}${area('仅增量：包含命令','allowCommands',(r.allowCommands||[]).join('\n'),'每行一个命令；不影响全量导入。')}${area('仅增量：排除命令','blockCommands',(r.blockCommands||[]).join('\n'),'多 Key 部分匹配时整条命令跳过。')}</div><div class="field"><label for="targetPolicy">目标端已有数据</label><select name="targetPolicy" id="targetPolicy"><option value="require_empty" ${t.targetPolicy==='require_empty'?'selected':''}>要求目标 DB 为空（默认）</option><option value="overwrite" ${t.targetPolicy==='overwrite'?'selected':''}>覆盖同名 Key</option></select></div></form>`;
  if(state.step===2)content=`<p>预检会测试连接、版本、拓扑、规则和目标 DB 条件，不会删除或修改 Redis 数据。</p>${checksHTML(state.checks)}<button class="secondary" id="rerun-checks">重新预检</button>`;
  if(state.step===3)content=`<div class="grid3"><div><h3>任务</h3>${esc(t.name)}</div><div><h3>源 → 目标</h3>${esc(connectionName(t.sourceId))} → ${esc(connectionName(t.targetId))}</div><div><h3>目标策略</h3>${t.targetPolicy==='overwrite'?'覆盖同名 Key':'要求目标为空'}</div></div><div class="divider"></div><p class="muted">启动后执行一次全量迁移，并持续同步增量；浏览器关闭不停止任务。</p>`;
  shell(t.id?'编辑同步任务':'创建同步任务','四步完成连接、规则、预检和确认',`<button class="ghost" id="back-tasks">返回任务列表</button>`,
    `<div class="steps">${steps.map((s,i)=>`<span class="step ${i===state.step?'active':''}">${i+1} · ${s}</span>`).join('')}</div><div class="card">${content}<div class="actions">${state.step>0?'<button class="ghost" id="previous-step">上一步</button>':''}<button class="secondary" id="save-draft">保存草稿</button>${state.step<3?`<button class="primary" id="next-step">${state.step===1?'保存并预检':'下一步'}</button>`:'<button class="primary" id="start-sync">启动同步</button>'}</div></div>`);
  on('back-tasks','click',async()=>{await loadLists();state.page='tasks';render();});
  on('previous-step','click',()=>{captureWizard();state.step--;render();});
  on('next-step','click',async()=>{try{captureWizard();if(state.step===0&&(!t.name||!t.sourceId||!t.targetId))throw new Error('请填写任务名称并选择源端和目标端');if(state.step===1){await saveWizard();await runChecks();}state.step++;render();}catch(error){notice(error.message,true);}});
  on('save-draft','click',async()=>{try{captureWizard();await saveWizard();notice('草稿已保存');}catch(error){notice(error.message,true);}});
  on('rerun-checks','click',async()=>{try{await runChecks();notice('预检已更新');}catch(error){notice(error.message,true);}});
  on('start-sync','click',async()=>{try{const run=await send('/tasks/'+t.id+'/start',{});state.task=t.id;state.selectedRun=run.id;state.page='task';await loadLists();notice('同步任务已启动');}catch(error){if(error.details?.checks)state.checks=error.details.checks;notice(error.message,true);}});
}
function captureWizard(){if(!byId('wizard-form'))return;const d=formData('wizard-form');if(state.step===0)Object.assign(state.editing,{name:d.name,sourceId:d.sourceId,targetId:d.targetId});if(state.step===1){state.editing.dbMap=parseMap(d.dbMap);state.editing.rules={allowPrefixes:lines(d.allowPrefixes),blockPrefixes:lines(d.blockPrefixes),allowRegex:lines(d.allowRegex),blockRegex:lines(d.blockRegex),allowCommands:lines(d.allowCommands),blockCommands:lines(d.blockCommands)};state.editing.targetPolicy=d.targetPolicy;}}
async function saveWizard(){const t=state.editing;if(!t.name)throw new Error('任务名称不能为空');const result=await send(t.id?'/tasks/'+t.id:'/tasks',t,t.id?'PUT':'POST');state.editing={...t,id:result.id};await loadLists();}
async function runChecks(){state.checks=await send('/tasks/'+state.editing.id+'/preflight',{});}

async function openTask(id){state.task=id;state.selectedRun=null;state.logRun=null;state.page='task';await loadLists();render();}
function renderTask(){
  const t=state.tasks.find(x=>x.id===state.task);if(!t){state.page='tasks';render();return;}
  shell(t.name,`${connectionName(t.sourceId)} → ${connectionName(t.targetId)}`,`<button class="ghost" id="back-tasks">返回任务列表</button>`,
    `<div class="card"><h2>运行状态</h2><div id="run-summary" class="muted">正在读取运行记录…</div><div class="actions"><button class="primary" id="run-start">${state.selectedRun?'重新全量运行':'启动同步'}</button><button class="secondary" id="run-stop">停止</button><button class="ghost" id="run-edit">编辑</button><button class="ghost" id="run-copy">复制任务</button><button class="danger" id="run-delete">删除任务</button></div></div><div class="card"><h2>目标 DB 清理</h2><p class="muted">此操作会清空所选目标 DB 内的所有 Key，不受任务 Key 规则限制。执行前需二次确认。</p><button class="danger" id="run-clear">清空所选目标 DB</button></div><div class="card"><div class="row"><h2>RedisShake 日志</h2><div class="actions" style="margin:0"><select id="log-run-select" aria-label="选择运行记录" style="min-width:190px"></select><button class="ghost" id="refresh-logs">刷新</button></div></div><pre class="log" id="run-log">暂无运行记录</pre></div>`);
  on('back-tasks','click',async()=>{await loadLists();state.page='tasks';render();});
  on('run-edit','click',()=>{state.editing=structuredClone(t);state.step=0;state.page='wizard';render();});
  on('run-copy','click',async()=>{try{await send('/tasks/'+t.id+'/copy',{});await loadLists();notice('任务已复制为草稿');}catch(error){notice(error.message,true);}});
  on('run-delete','click',async()=>{if(!confirm('删除任务记录？不会删除 Redis 数据。'))return;try{await api('/tasks/'+t.id,{method:'DELETE'});await loadLists();state.page='tasks';notice('任务已删除');}catch(error){notice(error.message,true);}});
  on('run-start','click',async()=>{if(state.selectedRun&&!confirm('重新全量运行会从源端重新读取数据，不会断点续传。继续？'))return;try{const run=await send('/tasks/'+t.id+'/start',{});state.selectedRun=run.id;notice('同步已启动');}catch(error){notice(error.message,true);}});
  on('run-stop','click',async()=>{if(!state.selectedRun)return;try{await send('/runs/'+state.selectedRun+'/stop',{});notice('正在停止');}catch(error){notice(error.message,true);}});
  on('refresh-logs','click',loadRunDetails);
  on('log-run-select','change',event=>{state.logRun=event.target.value;loadRunDetails();});
  on('run-clear','click',async()=>{
    try {
      const preview=await send('/tasks/'+t.id+'/clear-preview',{});
      const scope=preview.kind==='cluster'?'整个目标 Cluster 的 DB 0':`目标 DB ${preview.dbs.join(', ')}`;
      const typed=prompt(`即将清空「${preview.targetName}」的${scope}中的全部 Key。\n此操作不受任务 Key 过滤限制。\n请输入目标连接名称确认：`);
      if(typed===null)return;
      if(typed!==preview.targetName)throw new Error('连接名称不匹配，未执行清理');
      const result=await send('/tasks/'+t.id+'/clear-confirm',{token:preview.token,targetName:typed});
      notice(result.message+(result.results?.length?'；'+result.results.map(x=>`DB ${x.db} ${x.node}: ${x.message}`).join('；'):''),!result.ok);
    } catch(error) { notice(error.message,true); }
  });
  loadRunDetails();
}
async function loadRunDetails(){
  if(state.page!=='task')return;
  if(state.refresh){clearTimeout(state.refresh);state.refresh=null;}
  try{
    const runs=await api('/tasks/'+state.task+'/runs');const run=runs[0];
    const summary=byId('run-summary');const log=byId('run-log');if(!summary||!log)return;
    const selected=byId('log-run-select');if(selected){if(!runs.some(x=>x.id===state.logRun))state.logRun=run?.id||null;selected.innerHTML=runs.map((x,i)=>`<option value="${esc(x.id)}" ${state.logRun===x.id?'selected':''}>第 ${runs.length-i} 次 · ${esc(x.status)} · ${esc(x.startedAt)}</option>`).join('');}
    byId('run-start').textContent=run?'重新全量运行':'启动同步';
    if(!run){summary.textContent='草稿，尚未运行';log.textContent='暂无运行记录';byId('run-stop').disabled=true;}
    else{state.selectedRun=run.id;summary.innerHTML=`${badge(run.status)}　${badge(run.phase)}<p>开始：${esc(run.startedAt)}${run.endedAt?'　结束：'+esc(run.endedAt):''}</p>${run.error?`<div class="banner error">${esc(run.error)}</div>`:''}`;const busy=['RUNNING','STARTING','STOPPING'].includes(run.status);byId('run-stop').disabled=!busy;byId('run-start').disabled=busy;byId('run-edit').disabled=busy;byId('run-delete').disabled=busy;byId('run-clear').disabled=busy;try{const result=await api('/runs/'+state.logRun+'/logs');log.textContent=(result.truncated?'…仅显示末尾日志\n':'')+result.text;}catch(error){log.textContent=error.message;}}
  }catch(error){const summary=byId('run-summary');if(summary)summary.textContent='执行器暂不可用：'+error.message;}
  if(state.page==='task')state.refresh=setTimeout(loadRunDetails,4000);
}

boot();
