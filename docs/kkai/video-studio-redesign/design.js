/* Local, dependency-free review prototype. It makes no API or generation requests. */
const ASSETS = '../video-works/assets/';
const coverSrc = (name) => name === 'city' ? 'assets/city-ride-cinematic.webp' : name === 'stilllife' ? 'assets/cinema-tools-still-life.webp' : `${ASSETS}demo/${name === 'coupe' ? 'coupe' : 'ocean'}.webp`;
const $ = (selector) => document.querySelector(selector);
const escapeHtml = (value) => String(value).replace(/[&<>"']/g, (char) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
const icons = {
  chevron:'<path d="m9 5 7 7-7 7"/>', arrow:'<path d="M5 12h14m-5-5 5 5-5 5"/>', grid:'<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>', image:'<rect x="3" y="3" width="18" height="18" rx="3"/><circle cx="8" cy="8" r="1.5"/><path d="m3 16 5-5 4 4 3-3 6 6"/>', video:'<rect x="3" y="5" width="13" height="14" rx="3"/><path d="m16 10 5-3v10l-5-3"/>', help:'<circle cx="12" cy="12" r="9"/><path d="M9.5 9a2.5 2.5 0 0 1 5 0c0 2-2.5 2-2.5 4m0 3h.01"/>', sparkles:'<path d="m12 3 2.4 6.6L21 12l-6.6 2.4L12 21l-2.4-6.6L3 12l6.6-2.4L12 3ZM20 2v4m-2-2h4"/>', folder:'<path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>', list:'<path d="M8 5h13M8 12h13M8 19h13M3 5h.01M3 12h.01M3 19h.01"/>', sun:'<circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/>', moon:'<path d="M20.8 13A9 9 0 0 1 11 3.2 9 9 0 1 0 20.8 13Z"/>', sliders:'<path d="M4 7h4m6 0h6M4 17h9m5 0h2"/><circle cx="11" cy="7" r="3"/><circle cx="15" cy="17" r="3"/>', check:'<path d="m5 12 4 4L19 6"/>', refresh:'<path d="M20 7v5h-5M4 17v-5h5"/><path d="M5.5 8a7 7 0 0 1 11.7-3L20 8M4 16l2.8 3A7 7 0 0 0 18.5 16"/>', plus:'<path d="M12 5v14M5 12h14"/>', upload:'<path d="M12 16V3m-5 5 5-5 5 5M4 15v5h16v-5"/>', play:'<path d="m7 4 14 8-14 8Z"/>', download:'<path d="M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5"/>', more:'<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>', expand:'<path d="M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5"/>', volume:'<path d="m11 4-6 5H2v6h3l6 5ZM15 8a6 6 0 0 1 0 8m3-11a10 10 0 0 1 0 14"/>', clock:'<circle cx="12" cy="12" r="9"/><path d="M12 7v6l4 2"/>', alert:'<path d="m12 3 10 18H2Z"/><path d="M12 9v5m0 3h.01"/>', close:'<path d="m6 6 12 12M6 18 18 6"/>', info:'<circle cx="12" cy="12" r="9"/><path d="M12 11v6m0-10h.01"/>', copy:'<rect x="8" y="8" width="12" height="13" rx="2"/><path d="M16 8V3H3v13h5"/>', trash:'<path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7m4-7v7"/>', lock:'<rect x="4" y="10" width="16" height="11" rx="2"/><path d="M8 10V6a4 4 0 0 1 8 0v4"/>', cloud:'<path d="M7 18H6a5 5 0 0 1-1-9 7 7 0 0 1 13-2 5.5 5.5 0 0 1 0 11h-1M12 12v9m-3-3 3 3 3-3"/>', wifi:'<path d="M3 3l18 18M2 8a16 16 0 0 1 5-3m4-1a16 16 0 0 1 11 4M5 12a11 11 0 0 1 5-3m5 0a11 11 0 0 1 4 3M8 16a6 6 0 0 1 7-1m-3 5h.01"/>'
};
const icon = (name) => `<svg viewBox="0 0 24 24" aria-hidden="true">${icons[name] || icons.video}</svg>`;
const statusText = {ready:'可播放',queued:'排队中',processing:'生成中',archiving:'归档中',failed:'未能完成'};
const badge = (status) => `<span class="status ${status}">${icon(status === 'ready' ? 'check' : status === 'failed' ? 'alert' : 'clock')}${statusText[status] || '待核实'}</span>`;
const scenes = [
  ['ready','D01 · 创作 / 已完成'],['empty','D02 · 首次创作'],['processing','D03 · 正在生成'],['archiving','D03 · 正在归档'],['failed','D04 · 生成失败'],['unknown','D04 · 提交结果待核实'],['locating','D04 · 任务定位超时'],['media-error','D04 · 播放读取失败'],['quote-error','D04 · 报价失败'],['quote-expired','D04 · 报价过期'],['image','D05 · 参考图片'],['video-reference','D05 · 视频参考独立规格'],['frames','D05 · 首尾帧配置示例'],['upload-failed','D05 · 上传失败 / 续传'],['library','D06 · 作品库'],['tasks','D07 · 任务列表'],['dark','D08 · 深色创作台'],['portrait','D08 · 9:16 比例示意'],['square','D08 · 1:1 比例示意'],['preparing','D09 · 令牌准备'],['unavailable','D09 · 访问不可用'],['no-models','D09 · 无可用模型'],['load-error','D09 · 首次读取失败'],['library-empty','D09 · 作品库真空'],['filter-empty','D09 · 筛选为空'],['refresh-error','D09 · 刷新失败 / 保留内容'],['offline','D09 · 断网 / 旧快照']
];
const oceanPrompt = '清晨的海岸，柔和的晨光洒在蓝绿色的海面上，海浪缓缓推向岸边。镜头贴近水面平稳向前，捕捉浪花细腻的光影，宁静、通透，富有电影质感。';
const carPrompt = '银色跑车沿着蜿蜒海岸公路缓缓驶过，夕阳照亮车身轮廓。低机位跟拍，镜头平稳移动，保留自然的速度感与精致的金属反光。';
const cityPrompt = '雨后的城市街道，一位穿红色骑行夹克的骑手驶过潮湿路面。镜头从街道低位平稳跟拍，霓虹与车灯在积水中反射，细腻的电影摄影质感。';
const initialRecords = () => [
  {id:'city',title:'穿过雨夜',prompt:cityPrompt,image:'city',status:'ready',model:'Seedance 2.0 1080p',duration:5,ratio:'16:9',time:'今天 11:08',task:'task_demo_city_1028',date:'今天 · 10月28日'},
  {id:'ocean',title:'潮汐之间',prompt:oceanPrompt,image:'ocean',status:'ready',model:'Seedance 2.0 1080p',duration:5,ratio:'16:9',time:'今天 10:42',task:'task_demo_ocean_1028',date:'今天 · 10月28日'},
  {id:'coupe',title:'驶向日落',prompt:carPrompt,image:'coupe',status:'ready',model:'Seedance 2.0 1080p',duration:5,ratio:'16:9',time:'今天 10:28',task:'task_demo_coupe_1028',date:'今天 · 10月28日'},
  {id:'mountain',title:'山间的第一缕晨光',prompt:'山间晨雾缓慢散开，光线穿过树林。平稳推进，安静的自然气息。',status:'processing',model:'Seedance 2.0 720p',duration:5,ratio:'16:9',time:'今天 10:46',task:'task_demo_mountain_1028',date:'今天 · 10月28日'},
  {id:'rain',title:'雨后的城市街角',prompt:'城市街角，雨后路面的倒影映出暖黄色灯光。',status:'failed',model:'Seedance 2.0 720p',duration:5,ratio:'16:9',time:'昨天 18:26',task:'task_demo_rain_1027',date:'昨天 · 10月27日'}
];
let state = {page:'create',scene:'ready',theme:'dark',records:initialRecords(),selected:'city',prompt:cityPrompt,model:'standard',mode:'text',duration:5,ratio:'16:9',audio:true,dirty:false,uploads:[],inspiration:false,filter:'all',sampleCategory:'all',sampleModel:'all',samplePage:1,pending:false,pendingFingerprint:'',quote:'ready',busy:false};
let timerIds = [];
let toastTimer;
let dialogCallback;
let dialogTrigger;

function notify(message) {
  clearTimeout(toastTimer); $('#toast').textContent = message; $('#toast').classList.add('visible');
  toastTimer = setTimeout(() => $('#toast').classList.remove('visible'), 3800);
}
function showDialog(title,body,confirmLabel,callback,destructive=false) {
  dialogTrigger = document.activeElement; dialogCallback = callback;
  $('#dialog-content').innerHTML = `<div class="dialog-head"><h2 id="dialog-title">${title}</h2><button class="button icon subtle" data-action="dialog-close" aria-label="关闭">${icon('close')}</button></div><div class="dialog-body">${body}</div><div class="dialog-actions">${confirmLabel ? `<button class="button" data-action="dialog-close">取消</button><button class="button ${destructive?'danger':'primary'}" data-action="dialog-confirm">${confirmLabel}</button>` : '<button class="button primary" data-action="dialog-close">知道了</button>'}</div>`;
  $('#dialog').showModal();
}
$('#dialog').addEventListener('keydown', event => {
  if (event.key !== 'Tab') return;
  const controls = [...$('#dialog').querySelectorAll('button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled)')];
  const first = controls[0], last = controls.at(-1);
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
  if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
});
function closeDialog() { $('#dialog').close(); }
$('#dialog').addEventListener('close', () => { dialogCallback = null; if(dialogTrigger?.isConnected) dialogTrigger.focus(); else ($('.library-preview')||$('#main-content'))?.focus(); });
function replaceWithConfirmation(title,description,callback) {
  if(state.dirty || state.uploads.some(Boolean)) showDialog(title,`<p>${description}</p><p>已生成的作品不会受影响。取消后保留当前草稿。</p>`,'确认更换',callback);
  else callback();
}
function syncUrl() {
  const url = new URL(location.href); url.searchParams.set('scene',state.scene); url.searchParams.set('page',state.page); url.searchParams.set('theme',state.theme);
  history.replaceState({},'',url);
}
function setTheme(theme) {
  state.theme = theme; document.documentElement.dataset.theme = theme;
  $('#theme-button').innerHTML = icon(theme === 'dark' ? 'sun' : 'moon');
  $('#theme-button').setAttribute('aria-label',theme === 'dark' ? '切换浅色主题' : '切换深色主题');
}
function applyScene(name,page) {
  timerIds.forEach(clearTimeout); timerIds=[];
  name = name === 'completed' ? 'ready' : name === 'first' ? 'empty' : name === 'generating' ? 'processing' : name;
  if(!scenes.some(([id])=>id===name)) name='ready';
  state = {...state,scene:name,page:page || (name.includes('library') || name === 'filter-empty' ? 'library' : name === 'tasks' ? 'tasks':'create'),records:initialRecords(),selected:'city',prompt:cityPrompt,model:'standard',mode:'text',duration:5,ratio:'16:9',audio:true,dirty:false,uploads:[],inspiration:false,filter:'all',sampleCategory:'all',sampleModel:'all',samplePage:1,pending:false,quote:'ready',busy:false};
  if(name==='empty'){state.records=[];state.prompt='';state.selected=null;state.inspiration=true;}
  if(name==='processing'||name==='archiving'){state.selected='mountain';state.records.find(r=>r.id==='mountain').status=name;}
  if(name==='failed')state.selected='rain';
  if(name==='unknown'){state.pending=true;state.pendingFingerprint=cityPrompt;state.selected='pending';}
  if(name==='locating')state.selected='locating';
  if(name==='image'||name==='upload-failed'){state.mode='image';state.uploads=name==='image'?['ready']:['failed'];}
  if(name==='video-reference'){state.model='video';state.mode='reference';state.uploads=['ready'];}
  if(name==='frames'){state.model='frames';state.mode='frames';state.uploads=['ready','checking'];}
  if(name==='library-empty')state.records=[];
  if(name==='filter-empty'){state.records=state.records.filter(r=>r.status==='processing');state.filter='ready';}
  if(name==='quote-error')state.quote='failed';
  if(name==='quote-expired')state.quote='expired';
  if(name==='dark')setTheme('dark');
  $('#scene').value=name;
  syncUrl(); render();
}
function modelLabel(){return state.model==='video'?'Seedance 2.0 · 视频参考':state.model==='frames'?'首尾帧 · 能力配置示例':'Seedance 2.0 1080p';}
function quoteBlocked(){return state.busy||!state.prompt.trim()||new TextEncoder().encode(state.prompt).length>8000||state.quote!=='ready'||(state.mode!=='text'&&(state.uploads.length<(state.mode==='frames'?2:1)||state.uploads.some(s=>s!=='ready')))||(state.pending&&state.prompt===state.pendingFingerprint);}
function quoteMarkup(){
  const pendingSame=state.pending&&state.prompt===state.pendingFingerprint;
  const missingAssets=state.mode!=='text'&&(state.uploads.length<(state.mode==='frames'?2:1)||state.uploads.some(s=>!s));
  const unavailableAssets=state.mode!=='text'&&(missingAssets||state.uploads.some(s=>s!=='ready'));
  const amount=(Number(state.duration)*0.48).toFixed(2);
  const label=state.busy?'正在提交…':pendingSame?'原提交待核实':'生成视频';
  return `<div class="price-line"><span>预估费用 <small> / 次</small></span>${unavailableAssets?'<span>素材就绪后获取报价</span>':state.quote==='ready'?`<strong class="price-value">¥ ${amount} <small>CNY</small></strong>`:`<button type="button" class="button small" data-action="refresh-quote">${icon('refresh')}${state.quote==='failed'?'重新获取报价':'刷新已过期报价'}</button>`}</div><button type="button" class="button primary wide" data-action="generate" ${quoteBlocked()?'disabled':''}>${icon('sparkles')}${label}</button><p class="generate-note">${pendingSame?'先核实原任务，避免重复生成':state.quote!=='ready'?'报价恢复后才能提交；输入内容已保留':!state.prompt.trim()?'写下你的构想，开始创作':new TextEncoder().encode(state.prompt).length>8000?'提示词超出 8000 UTF-8 字节，请缩短':missingAssets?'请补充所需参考素材':state.mode!=='text'&&state.uploads.some(s=>s==='failed')?'素材上传中断，请恢复或重新选择':state.mode!=='text'&&state.uploads.some(s=>s!=='ready')?'请等待素材检查完成后再生成':'预计费用随当前参数更新'}</p>`;
}
function uploadMarkup(){
  if(state.mode==='text')return '';
  const roles=state.mode==='frames'?['首帧图片','尾帧图片']:state.mode==='reference'?['参考视频']:['参考图片'];
  return `<div class="field"><div class="field-label"><span>${state.mode==='frames'?'首尾帧素材':'参考素材'}</span><span class="muted">${roles.length} 个素材槽位</span></div><div class="upload-grid ${roles.length===1?'single':''}">${roles.map((role,index)=>{
    const status=state.uploads[index];
    return `<button type="button" class="upload-slot ${status==='failed'?'upload-failed':''}" data-upload="${index}" aria-label="${role}，${status==='ready'?'已就绪，可更换':status==='checking'?'检查中':status==='failed'?'上传失败，可恢复':'添加素材'}">${status==='ready'?`<img src="${ASSETS}demo/${index?'coupe':'ocean'}.webp" alt="示例素材缩略图">`:icon(status==='failed'?'refresh':status==='checking'?'clock':'plus')}<span><strong>${role}</strong><small>${status==='ready'?'示例素材 · 已就绪':status==='checking'?'正在检查素材':status==='failed'?'上传中断 · 点击恢复':state.mode==='reference'?'添加视频素材':'添加图片素材'}</small></span></button>`;
  }).join('')}</div><p class="field-note">${state.mode==='frames'?'配置示例：按首帧 → 尾帧的顺序构建画面变化。':state.mode==='reference'?'该独立规格需要视频参考素材。':'添加一张图片，描述希望画面如何运动。'}</p></div>`;
}
function composerMarkup(){
  const modes=state.model==='frames'?[['frames','首尾帧']]:state.model==='video'?[['reference','视频参考']]:[['text','文生视频'],['image','图生视频']];
  return `<section class="card composer" id="editor" aria-label="创作设置">
    <div class="card-heading"><h2>${icon('sliders')}创作设置</h2><span class="draft-mark">${icon('check')}当前草稿</span></div>
    <div class="field" style="margin-top:0"><label class="field-label" for="model-select"><span>生成模型</span><span class="muted">${state.model==='frames'?'能力示例':'1080p'}</span></label>
      <div class="model-select"><span class="model-logo" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M12 3 3 8l9 5 9-5-9-5Z"/><path d="m3 12 9 5 9-5M3 16l9 5 9-5"/></svg></span>
        <select id="model-select" aria-describedby="model-description"><option value="standard" ${state.model==='standard'?'selected':''}>Seedance 2.0 1080p</option><option value="video" ${state.model==='video'?'selected':''}>Seedance 2.0 · 视频参考</option><option value="frames" ${state.model==='frames'?'selected':''}>首尾帧 · 能力配置示例</option></select>
        <p id="model-description">${state.model==='frames'?'独立规格示例 · 不代表线上已启用':state.model==='video'?'Seedance Global · 需提供视频参考':'Seedance Global · 文生 / 图生视频'}</p>
      </div>
    </div>
    <div class="field"><span class="visually-hidden">创作方式</span><div class="segmented mode-tabs" aria-label="创作方式">${modes.map(([id,label])=>`<button type="button" data-mode="${id}" aria-pressed="${state.mode===id}">${icon(id==='text'?'video':'image')}<span>${label}</span></button>`).join('')}</div></div>
    <div class="field"><label class="field-label" for="prompt"><span>描述你的画面</span><span class="muted">提示词</span></label><div class="prompt-box"><textarea id="prompt" placeholder="描述主体、动作与镜头，让你的创意成为画面…" aria-describedby="prompt-count" spellcheck="false">${escapeHtml(state.prompt)}</textarea><div class="prompt-footer"><button type="button" data-action="prompt-tips">${icon('info')}写作建议</button><span id="prompt-count">${new TextEncoder().encode(state.prompt).length} / 8000 字节</span></div></div></div>
    <div id="upload-fields">${uploadMarkup()}</div>
    <div class="form-row output-settings"><div class="field"><label class="field-label" for="duration">视频时长</label><select id="duration" class="control">${[4,5,6,8,10,12,15].map(n=>`<option value="${n}" ${Number(state.duration)===n?'selected':''}>${n} 秒</option>`).join('')}</select></div>
      <div class="field"><span id="ratio-label" class="field-label">画幅比例</span><div class="ratio-options" role="group" aria-labelledby="ratio-label">${['16:9','9:16','1:1'].map(r=>`<button type="button" data-ratio="${r}" aria-pressed="${state.ratio===r}" aria-label="${r} 画幅"><span class="ratio-outline ratio-${r.replace(':','-')}" aria-hidden="true"></span><span>${r}</span></button>`).join('')}</div></div>
    </div>
    <details class="advanced"><summary><span>高级设置</span>${icon('chevron')}</summary><label class="switch-row" for="audio"><span>生成音频 <small class="muted"> 与画面同步</small></span><input type="checkbox" id="audio" ${state.audio?'checked':''}></label></details>
    <div class="price-section" id="quote-section">${quoteMarkup()}</div>
  </section>`;
}
function pendingMarkup(){
  if(!state.pending)return '';
  return `<div class="notice warning" role="status"><i>${icon('alert')}</i><div class="notice-text"><strong>有一次提交仍待核实</strong><p>服务尚未返回明确结果。查看其他作品或修改草稿，不会重复提交原请求。</p><code>task_demo_pending_1028</code><div class="notice-actions"><button class="button" data-action="check-pending">${icon('refresh')}核实原任务</button><button class="button" data-copy="task_demo_pending_1028">${icon('copy')}复制任务号</button><button class="button subtle" data-tab="tasks">查看任务</button></div></div></div>`;
}
function staleMarkup(){
  if(!['offline','refresh-error'].includes(state.scene))return '';
  return `<div class="notice ${state.scene==='offline'?'warning':'error'}" role="status"><i>${icon(state.scene==='offline'?'wifi':'refresh')}</i><div class="notice-text"><strong>${state.scene==='offline'?'连接暂时中断':'状态暂未更新'}</strong><p>保留上次读取的内容与当前草稿。恢复连接后，请重新获取最新状态。</p></div><button class="button" data-action="refresh-data">${icon('refresh')}重试</button></div>`;
}
function sampleMarkup(){
  const samples=[{id:'ocean',title:'让海风慢下来',category:'自然风景',model:'Seedance 2.0 1080p'},{id:'coupe',title:'驶入电影里的黄昏',category:'产品展示',model:'Seedance 2.0 1080p'}].filter(s=>state.sampleCategory==='all'||s.category===state.sampleCategory);
  return `<section class="inspiration"><button class="inspiration-title" data-action="toggle-inspiration" aria-expanded="${state.inspiration}"><span><strong>${icon('sparkles')}从灵感开始</strong><small>挑选一种氛围，让下一次创作更轻松</small></span><i>${icon('chevron')}</i></button>${state.inspiration?`<div class="inspiration-content"><div class="inspiration-filters"><select id="sample-model" aria-label="筛选样片模型"><option value="all">全部模型</option><option value="seedance" ${state.sampleModel==='seedance'?'selected':''}>Seedance 2.0 1080p</option></select><select id="sample-category" aria-label="筛选样片分类"><option value="all">全部分类</option><option ${state.sampleCategory==='自然风景'?'selected':''}>自然风景</option><option ${state.sampleCategory==='产品展示'?'selected':''}>产品展示</option></select></div><div class="sample-grid">${samples.map(s=>`<article class="sample-card"><div class="sample-image"><img src="${coverSrc(s.id)}" alt="${s.title}，示例封面"><button data-preview="${s.id}" aria-label="放大查看${s.title}示例封面">${icon('expand')}</button></div><div class="sample-body"><div><h3>${s.title}</h3><p>${s.category} · 5 秒</p></div><button class="button" data-sample="${s.id}">使用同款</button></div></article>`).join('')}</div><div class="sample-pagination"><span>第 ${state.samplePage} 页 · 演示样片</span><button data-action="samples-prev" aria-label="上一页样片" ${state.samplePage===1?'disabled':''}><span style="display:inline-block;transform:rotate(180deg)">${icon('chevron')}</span></button><button data-action="samples-next" aria-label="加载下一页样片">${icon('chevron')}</button></div></div>`:''}</section>`;
}
function recentMarkup(){
  if(!state.records.length)return '';
  return `<div class="section-heading"><h2>最近创作</h2><button class="button subtle" data-tab="library">全部作品${icon('arrow')}</button></div><div class="recent-grid">${state.records.slice(0,3).map(r=>`<button class="recent-card" data-select="${r.id}" aria-pressed="${state.selected===r.id}" aria-label="查看${escapeHtml(r.title)}，${statusText[r.status]}"><div class="recent-image">${r.image?`<img src="${coverSrc(r.image)}" alt="示例封面">`:`<span class="recent-placeholder">${icon(r.status==='failed'?'alert':'clock')}</span>`}${badge(r.status)}</div><div class="recent-info"><strong>${escapeHtml(r.title)}</strong><span>${r.duration} 秒 · ${r.ratio} · ${r.time}</span></div></button>`).join('')}</div>`;
}
function mediaMarkup(record){
  if(['portrait','square'].includes(state.scene)){
    const portrait=state.scene==='portrait';
    return `<div class="media-stage ratio-stage"><div class="ratio-demo ${portrait?'portrait':'square'}"><span>${icon('video')}</span><strong>${portrait?'9:16':'1:1'}</strong><p>完整画幅 · 比例示意</p><small>此处不使用横图裁切模拟视频</small></div><span class="media-caption">中性比例示例 · 非真实视频</span></div>`;
  }
  return `<div class="media-stage"><img src="${coverSrc(record.image||'ocean')}" alt="${escapeHtml(record.title)}，静态示例封面"><span class="media-caption">示例封面 · ${record.ratio}</span><button class="play-button" data-preview="${record.image||'ocean'}" aria-label="查看${escapeHtml(record.title)}播放控件演示">${icon('play')}</button><div class="media-controls"><button data-preview="${record.image||'ocean'}" aria-label="播放控件演示">${icon('play')}</button><span class="media-time">00:00 <span>/ 00:0${record.duration}</span></span><span class="timeline"><i></i></span><small>控件演示</small><button data-action="media-note" aria-label="音量控件演示">${icon('volume')}</button><button data-preview="${record.image||'ocean'}" aria-label="查看完整示例封面">${icon('expand')}</button></div></div>`;
}
function resultMarkup(){
  if(state.scene==='load-error')return `<section class="card"><div class="empty-content"><span class="entry-icon">${icon('refresh')}</span><h2>暂时无法读取作品</h2><p>创作设置已准备好。你可以继续编辑，也可以重新读取作品记录。</p><button class="button" data-action="refresh-data">${icon('refresh')}重新读取</button></div></section>`;
  if(state.selected==='pending'||state.selected==='locating'){
    const pending=state.selected==='pending';
    return `<section class="card result-card"><div class="result-heading"><h2>${pending?'正在核实提交':'暂未定位到作品'}</h2><span class="status queued">${icon('clock')}${pending?'结果待核实':'等待同步'}</span></div><div class="state-result is-pending"><div class="state-art">${icon('clock')}<span class="state-art-ring" aria-hidden="true"></span></div><h3>${pending?'保留这一次创作的线索':'任务已受理，作品暂未出现'}</h3><p>${pending?'现在无法确认服务是否已受理。核实操作只读取任务状态，不会再次生成或产生新的费用。':'本次查找窗口已结束，这不代表任务失败。你可以手动刷新，或凭任务号查看任务历史。'}</p><button class="button" data-action="${pending?'check-pending':'check-locating'}">${icon('refresh')}${pending?'核实原任务':'重新查找'}</button></div><div class="result-meta"><div class="meta-line">task_demo_${pending?'pending':'locating'}_1028</div><button class="button subtle" data-tab="tasks">查看任务${icon('arrow')}</button></div></section>`;
  }
  const record=state.records.find(r=>r.id===state.selected);
  if(!record)return `<section class="first-state"><div class="first-state-art"><img src="${coverSrc('stilllife')}" alt="摄影灯具与镜头的示例静物画面"><span>创作起点 · 示例插画</span></div><div><h2>从一个画面开始</h2><p>描述你想拍下的瞬间，或从下面选择一份灵感。</p><div class="start-steps">创作设置 <span>→</span> 生成视频</div></div></section>`;
  let content;
  if(state.scene==='media-error'&&record.status==='ready') content=`<div class="state-result"><i class="entry-icon">${icon('video')}</i><h3>视频暂时无法读取</h3><p>已尝试重新获取播放地址。作品记录仍在，你可以手动重试；此操作不会重新生成。</p><button class="button" data-action="refresh-media">${icon('refresh')}重新读取视频</button></div>`;
  else if(record.status==='ready')content=mediaMarkup(record);
  else{
    const failed=record.status==='failed';
    const descriptions={queued:['等待开始生成','任务已进入队列。你可以继续编辑下一次创作。'],processing:['你的画面，正在成形','正在生成视频。完成后会自动更新，当前草稿可以继续修改。'],archiving:['画面已生成，正在准备播放','正在保存视频并处理封面，完成后即可播放和下载。'],failed:['这次创作未能完成','暂时无法提供结果，请查看任务状态。费用以实际结算记录为准。']};
    content=`<div class="state-result ${failed?'is-failed':'is-active'}" data-stage="${record.status}"><div class="state-art">${icon(failed?'alert':record.status==='archiving'?'cloud':'video')}<span class="state-art-ring" aria-hidden="true"></span></div><h3>${descriptions[record.status][0]}</h3><p>${descriptions[record.status][1]}</p>${failed?'<button class="button" data-tab="tasks">查看任务状态</button>':`<div class="phase-steps"><span class="${record.status==='queued'?'active':'done'}">${icon(record.status==='queued'?'clock':'check')}排队</span><i></i><span class="${record.status==='processing'?'active':record.status==='archiving'?'done':''}">${icon(record.status==='archiving'?'check':'video')}生成</span><i></i><span class="${record.status==='archiving'?'active':''}">${icon('cloud')}归档</span><i></i><span>${icon('check')}完成</span></div>`}</div>`;
  }
  return `<section class="card result-card status-${record.status}"><div class="result-heading"><div class="result-title"><h2>当前${record.status==='ready'?'作品':'任务'}</h2>${badge(record.status)}</div><time>${record.time}</time></div>${content}<div class="result-meta"><div><h3>${escapeHtml(record.title)}</h3><div class="meta-line"><span>${record.model}</span><span class="dot"></span><span>${record.duration} 秒</span><span class="dot"></span><span>${['portrait','square'].includes(state.scene)?state.scene==='portrait'?'9:16':'1:1':record.ratio}</span></div></div><div class="result-actions">${record.status==='ready'?`<button class="button" data-download="${record.id}">${icon('download')}下载</button>`:''}<button class="button icon" data-details="${record.id}" aria-label="作品详情">${icon('more')}</button></div></div></section>`;
}
function resultsColumn(){return `<div class="mobile-jump"><span>当前成果</span><button class="button subtle" data-action="jump-editor">返回编辑 ↑</button></div>${pendingMarkup()}${staleMarkup()}${resultMarkup()}${recentMarkup()}${sampleMarkup()}`;}
function entryMarkup(){
  const entries={preparing:['clock','正在准备你的创作空间','正在检查访问权限与可用模型，请稍候。',null],unavailable:['lock','当前无法访问视频工作室','此账号暂未获得视频工作室访问权限。请联系管理员确认访问范围。','返回可用入口'],'no-models':['video','暂时没有可用的视频模型','当前分组暂无可用模型。你可以稍后刷新，或联系管理员检查模型配置。','刷新模型列表']};
  const [symbol,title,text,action]=entries[state.scene];
  return `<section class="card entry-card"><div class="entry-icon">${icon(symbol)}</div><h2>${title}</h2><p>${text}</p>${action?`<button class="button ${state.scene==='unavailable'?'':'primary'}" data-action="${state.scene==='unavailable'?'available-entry':'refresh-models'}">${action}</button>`:''}</section>`;
}
function libraryMarkup(){
  const records=state.records.filter(r=>state.filter==='all'||r.status==='ready');
  const groups=[...new Set(records.map(r=>r.date))];
  return `${staleMarkup()}<div class="page-toolbar"><div><h2>作品库</h2><p>按时间回看每一段创作，随时取用已就绪的作品。</p></div><div class="segmented" aria-label="作品筛选"><button data-filter="all" aria-pressed="${state.filter==='all'}">全部</button><button data-filter="ready" aria-pressed="${state.filter==='ready'}">可播放</button></div></div>${!records.length?`<section class="card empty-content"><span class="entry-icon">${icon(state.records.length?'clock':'video')}</span><h2>${state.records.length?'还没有可播放的作品':'你的第一件作品，即将在这里'}</h2><p>${state.records.length?'其他任务仍在处理中。切回全部，可以查看最新状态。':'创作完成后，作品会保存在这里，方便随时播放与下载。'}</p><button class="button primary" ${state.records.length?'data-filter="all"':'data-tab="create"'}>${state.records.length?'查看全部任务':'开始创作'}${icon('arrow')}</button></section>`:groups.map(date=>`<h3 class="library-date">${date}</h3><div class="library-grid">${records.filter(r=>r.date===date).map(r=>`<article class="library-card ${state.selected===r.id?'is-selected':''}"><button class="library-preview" ${r.status==='ready'?`data-preview="${r.image}"`:`data-details="${r.id}"`} aria-label="查看${escapeHtml(r.title)}">${r.image?`<img src="${coverSrc(r.image)}" alt="${escapeHtml(r.title)}，示例封面">`:`<span class="library-placeholder">${icon(r.status==='failed'?'alert':'clock')}</span>`}${badge(r.status)}${r.status==='ready'?`<span class="small-play">${icon('expand')}</span>`:''}</button><div class="library-body"><h3>${escapeHtml(r.title)}</h3><div class="meta-line"><span>${r.model}</span><span class="dot"></span><span>${r.duration} 秒 · ${r.ratio}</span></div><div class="library-tools"><span>${r.time}</span><div>${r.status==='ready'?`<button class="button icon subtle" data-download="${r.id}" aria-label="下载${escapeHtml(r.title)}">${icon('download')}</button>`:''}<button class="button icon subtle" data-details="${r.id}" aria-label="查看${escapeHtml(r.title)}详情">${icon('info')}</button>${['ready','failed'].includes(r.status)?`<button class="button icon subtle" data-delete="${r.id}" aria-label="删除${escapeHtml(r.title)}">${icon('trash')}</button>`:''}</div></div></div></article>`).join('')}</div>`).join('')}${records.length?'<div class="load-more"><button class="button" data-action="load-more">加载更多作品</button></div>':''}`;
}
function tasksMarkup(){
  return `${pendingMarkup()}${staleMarkup()}<div class="page-toolbar"><div><h2>创作任务</h2><p>查看当前进展和公开结果。未就绪的任务无法播放或下载。</p></div><button class="button" data-action="refresh-tasks">${icon('refresh')}刷新状态</button></div><section class="card tasks-list"><div class="task-row task-row-head"><span>创作任务</span><span>状态 / 提交时间</span><span>操作</span></div>${state.records.length?state.records.map(r=>`<article class="task-row"><div class="task-icon">${r.image?`<img src="${coverSrc(r.image)}" alt="">`:icon('video')}</div><div class="task-info"><h3>${escapeHtml(r.title)}</h3><p><span>${r.model}</span><span>· ${r.duration} 秒</span></p><p><button class="task-copy" data-copy="${r.task}" aria-label="复制任务号${r.task}"><span>${r.task}</span>${icon('copy')}</button></p>${r.status==='failed'?'<p class="failure-line">暂时无法提供结果，请查看任务状态。</p>':''}</div><div class="task-status">${badge(r.status)}<time>${r.time}</time></div><div class="task-action">${r.status==='ready'?`<button class="button" data-view-library="${r.id}">查看作品${icon('arrow')}</button>`:`<button class="button subtle" data-details="${r.id}">查看详情</button>`}</div></article>`).join(''):'<div class="empty-content"><h2>暂无创作任务</h2><p>主动提交创作后，可在这里查看生成进度。</p><button class="button primary" data-tab="create">开始创作</button></div>'}</section>${state.records.length?'<div class="load-more"><button class="button" data-action="load-more">加载更多任务</button></div>':''}`;
}
function render(){
  const focusId=document.activeElement?.id;
  const selection=focusId==='prompt'?[document.activeElement.selectionStart,document.activeElement.selectionEnd]:null;
  document.querySelectorAll('[data-tab]').forEach(button=>button.setAttribute('aria-current',button.dataset.tab===state.page?'page':'false'));
  if(state.page==='create')$('#workspace').innerHTML=['preparing','unavailable','no-models'].includes(state.scene)?entryMarkup():`<div class="mobile-jump"><span>创作设置</span><button class="button subtle" data-action="jump-result">查看结果 ↓</button></div><div class="create-layout">${composerMarkup()}<section class="result-column" id="results" aria-label="当前成果与近期创作">${resultsColumn()}</section></div>`;
  else $('#workspace').innerHTML=state.page==='library'?libraryMarkup():tasksMarkup();
  if($('.inspiration-title'))$('.inspiration-title').insertAdjacentHTML('afterbegin',`<span class="inspiration-art"><img src="${coverSrc('stilllife')}" alt="摄影器材示例插画"></span>`);
  if(focusId&&document.getElementById(focusId)){document.getElementById(focusId).focus({preventScroll:true});if(selection)document.getElementById(focusId).setSelectionRange(...selection);}
}
function updateResults(){if(state.page==='create'&&$('#results')){$('#results').innerHTML=resultsColumn();$('.inspiration-title')?.insertAdjacentHTML('afterbegin',`<span class="inspiration-art"><img src="${coverSrc('stilllife')}" alt="摄影器材示例插画"></span>`);}else render();}
function animateQuote(){const quote=$('#quote-section');if(!quote)return;quote.classList.remove('quote-updated');void quote.offsetWidth;quote.classList.add('quote-updated');}
function previewMedia(imageName){showDialog('静态封面预览',`<img src="${coverSrc(imageName)}" alt="完整示例封面"><p>当前为高保真设计演示，仅使用静态示例封面。实际落地后，这里将播放已生成的视频。</p><p>本次查看不会连接生成服务或产生费用。</p>`);}
function details(id){const r=state.records.find(item=>item.id===id);if(!r)return;showDialog('作品详情',`<dl class="dialog-details"><dt>任务号</dt><dd>${r.task}</dd><dt>状态</dt><dd>${statusText[r.status]}</dd><dt>模型</dt><dd>${r.model}</dd><dt>输出</dt><dd>${r.duration} 秒 · ${r.ratio}</dd><dt>提交时间</dt><dd>${r.time}</dd><dt>提示词</dt><dd>${escapeHtml(r.prompt)}</dd></dl><p style="margin-top:18px">仅查看历史信息，不会覆盖当前草稿。演示记录不代表实际任务或扣费结果。</p>`);}

document.addEventListener('input',(event)=>{
  if(event.target.id==='prompt'){
    state.prompt=event.target.value;state.dirty=true;
    $('#prompt-count').textContent=`${new TextEncoder().encode(state.prompt).length} / 8000 字节`;
    $('#prompt').setAttribute('aria-invalid',String(new TextEncoder().encode(state.prompt).length>8000));
    $('#quote-section').innerHTML=quoteMarkup();
  }
});
document.addEventListener('change',(event)=>{
  const id=event.target.id;
  if(id==='scene'){applyScene(event.target.value);return;}
  if(id==='model-select'){
    const value=event.target.value;event.target.value=state.model;
    replaceWithConfirmation('更换生成模型？','当前素材会被清空，模型专属设置将恢复默认。提示词将保留。',()=>{state.model=value;state.duration=5;state.ratio='16:9';state.audio=true;state.mode=value==='frames'?'frames':value==='video'?'reference':'text';state.uploads=[];state.dirty=true;render();});
  }
  if(id==='duration'||id==='ratio'||id==='audio'){state[id]=id==='audio'?event.target.checked:event.target.value;state.dirty=true;$('#quote-section').innerHTML=quoteMarkup();animateQuote();}
  if(id==='sample-category'||id==='sample-model'){state[id==='sample-category'?'sampleCategory':'sampleModel']=event.target.value;state.samplePage=1;updateResults();}
});
document.addEventListener('click',async(event)=>{
  const button=event.target.closest('button');if(!button||button.disabled)return;
  if(button.id==='theme-button'){setTheme(state.theme==='light'?'dark':'light');syncUrl();return;}
  if(button.dataset.tab){state.page=button.dataset.tab;syncUrl();render();return;}
  if(button.dataset.mode){const mode=button.dataset.mode;if(mode===state.mode)return;replaceWithConfirmation('切换创作方式？','切换后会清空不兼容的参考素材，提示词将保留。',()=>{state.mode=mode;state.uploads=[];state.dirty=true;render();});return;}
  if(button.dataset.select){state.selected=button.dataset.select;updateResults();return;}
  if(button.dataset.ratio){state.ratio=button.dataset.ratio;state.dirty=true;document.querySelectorAll('[data-ratio]').forEach(item=>item.setAttribute('aria-pressed',String(item.dataset.ratio===state.ratio)));$('#quote-section').innerHTML=quoteMarkup();animateQuote();return;}
  if(button.dataset.preview){previewMedia(button.dataset.preview);return;}
  if(button.dataset.details){details(button.dataset.details);return;}
  if(button.dataset.download){notify('设计演示未提供视频文件；正式页面仅下载已就绪的作品。');return;}
  if(button.dataset.copy){try{await navigator.clipboard.writeText(button.dataset.copy);notify('任务号已复制');}catch{showDialog('复制任务号',`<p>当前浏览器未授权剪贴板，请选择下方任务号手动复制：</p><p><code>${button.dataset.copy}</code></p>`);}return;}
  if(button.dataset.filter){state.filter=button.dataset.filter;render();return;}
  if(button.dataset.viewLibrary){state.page='library';state.filter='all';state.selected=button.dataset.viewLibrary;syncUrl();render();details(state.selected);return;}
  if(button.dataset.delete){const id=button.dataset.delete;const record=state.records.find(r=>r.id===id);showDialog('删除这件作品？',`<p>“${escapeHtml(record.title)}”的作品记录和托管结果将被删除。</p><p>删除不会取消上游生成，不代表费用退还。已下载的副本不受影响。</p>`,'删除作品',()=>{state.records=state.records.filter(r=>r.id!==id);if(state.selected===id)state.selected=state.records[0]?.id||null;render();notify('示例作品已从当前设计演示中删除');},true);return;}
  if(button.dataset.sample){const id=button.dataset.sample;replaceWithConfirmation('使用这份灵感？','样片将替换当前提示词、模型和输出设置，并清空当前参考素材。',()=>{state.model='standard';state.mode='text';state.prompt=id==='ocean'?oceanPrompt:carPrompt;state.duration=5;state.ratio='16:9';state.uploads=[];state.dirty=true;render();notify('已套用示例设置；由你主动点击才会发起生成。');});return;}
  if(button.dataset.upload!==undefined){const index=Number(button.dataset.upload);showDialog(state.uploads[index]==='failed'?'恢复素材上传':'添加参考素材',`<p>此处演示素材从上传、检查到可用的状态。不会读取或上传你的文件。</p><p>${state.mode==='frames'?'首帧和尾帧分别对应独立槽位。':state.mode==='reference'?'当前规格需要视频参考；封面仅作为素材卡片示意。':'可使用已有示例素材体验表单。'}正式实现按当前模型与服务端限制校验文件。</p>`,state.uploads[index]==='failed'?'模拟续传':'使用示例素材',()=>{state.uploads[index]='checking';state.dirty=true;render();timerIds.push(setTimeout(()=>{state.uploads[index]='ready';if($('#upload-fields'))$('#upload-fields').innerHTML=uploadMarkup();if($('#quote-section'))$('#quote-section').innerHTML=quoteMarkup();notify('演示素材已通过检查');},1100));});return;}
  const action=button.dataset.action;
  if(action==='dialog-close'){closeDialog();return;}
  if(action==='dialog-confirm'){const callback=dialogCallback;closeDialog();callback?.();return;}
  if(action==='toggle-inspiration'){state.inspiration=!state.inspiration;updateResults();return;}
  if(action==='jump-result'||action==='jump-editor'){document.getElementById(action==='jump-result'?'results':'editor')?.scrollIntoView({behavior:matchMedia('(prefers-reduced-motion:reduce)').matches?'instant':'smooth',block:'start'});return;}
  if(action==='samples-prev'||action==='samples-next'){state.samplePage=action==='samples-next'?state.samplePage+1:Math.max(1,state.samplePage-1);updateResults();notify('分页交互演示：复用本地两张示例封面。');return;}
  if(action==='load-more'){notify('当前演示记录已全部加载；正式页面继续读取下一页。');return;}
  if(action==='check-pending'||action==='check-locating'){notify('已演示只读查询：结果仍待核实，不会重新提交生成。');return;}
  if(action==='refresh-quote'){state.quote='ready';$('#quote-section').innerHTML=quoteMarkup();animateQuote();notify('已更新演示报价，未连接实际计费服务。');return;}
  if(action==='refresh-data'||action==='refresh-media'){state.scene='ready';$('#scene').value='ready';syncUrl();render();notify('已恢复本地示例内容');return;}
  if(action==='refresh-models'){notify('演示：当前分组仍暂无可用模型。');return;}
  if(action==='available-entry'){showDialog('返回可用入口','<p>正式页面返回控制台中有访问权限的入口。本设计稿保留在当前审查页面，不跳转业务系统。</p>');return;}
  if(action==='refresh-tasks'){notify('示例任务状态已更新；未连接实际任务服务。');return;}
  if(action==='prompt-tips'){showDialog('让描述更具体一些','<p>从「主体 + 动作 + 场景」开始，再补充镜头移动、光线和画面氛围。</p><p>例如：海浪缓缓推向岸边，镜头贴近水面向前移动，清晨柔光，宁静而通透。</p><p>提示词上限按 UTF-8 字节计算，中文通常每字占用多个字节。</p>');return;}
  if(action==='media-note'){notify('静态封面演示：音量、播放与全屏将在真实视频播放器中提供。');return;}
  if(action==='help'){showDialog('把灵感变成视频','<p>① 选择支持所需创作方式的模型，填写提示词与参考素材。</p><p>② 查看当前参数的费用，主动点击生成。</p><p>③ 留在创作页等待，生成、归档完成后查看和下载作品。</p><p>这是基于产品需求文档 v1.1 的设计演示。示例模型不代表线上启用状态，分类筛选展示服务端支持分类时的场景。</p>');return;}
  if(action==='image'||action==='console'){notify('当前审查范围为视频工作室，其他控制台入口仅展示导航关系。');return;}
  if(action==='generate'){
    if(quoteBlocked())return;
    const id='demo-'+Date.now();const submission={prompt:state.prompt,model:modelLabel(),duration:Number(state.duration),ratio:state.ratio};state.busy=true;$('#quote-section').innerHTML=quoteMarkup();notify('开始模拟生成流程，不会调用模型或扣费。');
    timerIds.push(setTimeout(()=>{state.busy=false;state.records.unshift({id,title:'新的创作',...submission,image:null,status:'queued',time:'刚刚',task:'task_'+id.replace('-','_'),date:'今天 · 10月28日'});state.selected=id;state.inspiration=false;if($('#quote-section'))$('#quote-section').innerHTML=quoteMarkup();updateResults();},650));
    ['processing','archiving','ready'].forEach((status,index)=>timerIds.push(setTimeout(()=>{const record=state.records.find(r=>r.id===id);if(!record)return;record.status=status;if(status==='ready'){record.image='ocean';record.title='新的创作 · 示例结果';}updateResults();if(status==='ready')notify('演示流程已完成；使用既有示例封面表示结果。');},[1600,3600,5600][index])));
  }
});
$('#scene').innerHTML=scenes.map(([id,label])=>`<option value="${id}">${label}</option>`).join('');
document.querySelectorAll('[data-icon]').forEach(el=>el.innerHTML=icon(el.dataset.icon));
const params=new URLSearchParams(location.search);
setTheme(params.get('theme')==='light'?'light':'dark');
applyScene(params.get('scene')||'ready',['create','library','tasks'].includes(params.get('page'))?params.get('page'):undefined);
window.addEventListener('popstate',()=>{const query=new URLSearchParams(location.search);setTheme(query.get('theme')==='light'?'light':'dark');applyScene(query.get('scene')||'ready',query.get('page'));});
