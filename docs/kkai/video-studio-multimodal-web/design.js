/* Interactive design preview for new-api / QuantumNous. No production services. */
(() => {
  'use strict';

  const byId = (id) => document.getElementById(id);
  const mediaPaths = {
    city: '../video-studio-redesign/assets/city-ride-cinematic.webp',
    ocean: '../video-works/assets/demo/ocean.webp',
    coupe: '../video-works/assets/demo/coupe.webp',
  };
  const models = {
    sd25video: { name: 'Seedance 2.5 · 视频参考', family: 'Seedance 2.5', maxDuration: 30, videoRequired: true },
    sd20video: { name: 'Seedance 2.0 · 视频参考', family: 'Seedance 2.0', maxDuration: 15, videoRequired: true },
    sd25image: { name: 'Seedance 2.5 · 图片参考', family: 'Seedance 2.5', maxDuration: 30, videoRequired: false },
  };
  const kindNames = { image: '图片', video: '视频', audio: '音频' };
  const kindIcons = { image: 'image', video: 'film', audio: 'audio' };
  const limits = { image: 9, video: 3, audio: 3 };
  let referenceSequence = 3;
  let taskSequence = 0;
  let toastTimer;
  let messageAction = null;
  let currentTaskId = 'city';
  const initialPrompt = byId('prompt').value;

  const initialReferences = [
    { id: 'ref-1', kind: 'image', name: '骑行者参考.jpg', seconds: 0, scene: 'city' },
    { id: 'ref-2', kind: 'video', name: '跟随镜头.mp4', seconds: 6, scene: 'city' },
    { id: 'ref-3', kind: 'audio', name: '城市氛围.mp3', seconds: 5, scene: 'city' },
  ];
  const copyReferences = (references) => references.map((reference) => ({ ...reference }));
  const state = {
    references: copyReferences(initialReferences),
    excluded: new Set(),
    lastRemoved: null,
    quoteChanged: false,
  };
  const tasks = {
    city: {
      title: '雨后，沿海城市', scene: 'city', status: 'ready', time: '今天 14:28',
      settings: { model: 'sd25video', ratio: '16:9', resolution: '720p', duration: '8', sound: true, prompt: initialPrompt, references: copyReferences(initialReferences) },
    },
    ocean: {
      title: '日落时的海上之门', scene: 'ocean', status: 'processing', time: '示例任务',
      settings: { model: 'sd25image', ratio: '16:9', resolution: '1080p', duration: '10', sound: true, prompt: '日落时分，镜头缓慢接近海面上的一扇门。保持参考图的构图与暖金色光线，让海浪自然起伏，声音安静、空灵。', references: [{ id: 'ref-ocean', kind: 'image', name: '海上之门.jpg', seconds: 0, scene: 'ocean' }] },
    },
    coupe: {
      title: '暮色中的汽车短片', scene: 'coupe', status: 'queued', time: '示例任务',
      settings: { model: 'sd20video', ratio: '16:9', resolution: '720p', duration: '8', sound: true, prompt: '保留参考图中汽车的造型，参考视频的低机位运镜。镜头沿车身缓慢移动，暮色映在金属表面，配上克制的环境声音。', references: [{ id: 'ref-coupe-image', kind: 'image', name: '汽车主体.jpg', seconds: 0, scene: 'coupe' }, { id: 'ref-coupe-video', kind: 'video', name: '低机位镜头.mp4', seconds: 6, scene: 'coupe' }] },
    },
  };

  function escapeHTML(value) {
    return String(value).replace(/[&<>"']/g, (character) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[character]);
  }

  function icons(root = document) {
    if (typeof window.renderIcons === 'function') window.renderIcons(root);
  }

  function toast(message) {
    clearTimeout(toastTimer);
    byId('toast').textContent = message;
    byId('toast').classList.add('visible');
    toastTimer = setTimeout(() => byId('toast').classList.remove('visible'), 4200);
  }

  function showMessage(title, body, confirmText, onConfirm) {
    byId('messageTitle').textContent = title;
    byId('messageBody').innerHTML = body;
    byId('messageActions').innerHTML = confirmText
      ? '<button class="button secondary" data-close="messageDialog">取消</button><button class="button primary" id="confirmMessage"></button>'
      : '<button class="button primary" data-close="messageDialog">知道了</button>';
    messageAction = onConfirm || null;
    if (confirmText) byId('confirmMessage').textContent = confirmText;
    const dialog = byId('messageDialog');
    if (!dialog.open) dialog.showModal();
  }

  function referenceGroups() {
    const model = byId('modelSelect').value;
    const excluded = state.references.filter((item) => state.excluded.has(item.id));
    const pending = state.references.filter((item) => !state.excluded.has(item.id) && model === 'sd25image' && item.kind === 'video');
    const active = state.references.filter((item) => !state.excluded.has(item.id) && !pending.includes(item));
    const restorable = excluded.filter((item) => model !== 'sd25image' || item.kind !== 'video');
    return { active, pending, excluded, restorable };
  }

  function referenceTotals(references) {
    return references.reduce((totals, item) => {
      totals[item.kind] += 1;
      if (item.kind !== 'image') totals[item.kind + 'Seconds'] += item.seconds;
      return totals;
    }, { image: 0, video: 0, audio: 0, videoSeconds: 0, audioSeconds: 0 });
  }

  function validateDraft() {
    const groups = referenceGroups();
    const totals = referenceTotals(groups.active);
    const model = models[byId('modelSelect').value];
    const messages = [];
    if (groups.pending.length) messages.push('当前模型不接受视频参考。请切换模型，或明确排除待处理的视频。');
    if (totals.videoSeconds > 15) messages.push(`参考视频总时长为 ${totals.videoSeconds} 秒，最多 15 秒。请移除或替换一个视频。`);
    if (totals.audioSeconds > 15) messages.push(`参考音频总时长为 ${totals.audioSeconds} 秒，最多 15 秒。请移除或替换一段音频。`);
    if (totals.audio && !totals.image && !totals.video) messages.push('参考音频不能单独使用，请添加至少一张图片或一个视频。');
    if (model.videoRequired && !totals.video) messages.push('所选视频参考模型需要至少一个有效的视频参考，不会自动切换模型。');
    if (totals.audio && !byId('sound').checked) messages.push('使用参考音频时，请开启视频声音，或移除参考音频。');
    if (Number(byId('duration').value) > model.maxDuration) messages.push(`所选模型最多生成 ${model.maxDuration} 秒视频。请手动调整输出时长，当前值不会自动修改。`);
    const prompt = byId('prompt').value.trim();
    if (!prompt) messages.push('请填写画面描述。');
    if (new TextEncoder().encode(prompt).length > 8000) messages.push('画面描述超过 8000 字节，请缩短后继续。');
    return { ...groups, totals, messages };
  }

  function renderReferences() {
    const { active, pending, excluded, restorable, totals, messages } = validateDraft();
    byId('refCount').textContent = String(state.references.length);
    byId('referenceList').innerHTML = state.references.length
      ? state.references.map((item) => {
        const isPending = pending.includes(item);
        const isExcluded = excluded.includes(item);
        const status = isPending ? '待处理：与当前模型不兼容' : isExcluded ? '本次未使用，保留在草稿中' : '已就绪';
        const visual = item.kind === 'audio'
          ? '<div class="audio-art" aria-hidden="true">' + [10, 18, 13, 25, 18, 28, 22, 14, 24, 17, 11, 20].map((height) => `<span style="height:${height}px"></span>`).join('') + '</div>'
          : `<img src="${mediaPaths[item.scene]}" alt="${escapeHTML(item.name)}的示例静帧"${item.kind === 'image' && item.scene === 'city' ? ' class="person-crop"' : ''}>`;
        return `<article class="reference-card${isPending ? ' pending' : ''}${isExcluded ? ' excluded' : ''}" aria-label="${escapeHTML(item.name)}，${status}"><div class="thumbnail${item.kind === 'audio' ? ' audio-thumb' : ''}">${visual}<span class="media-badge"><i data-icon="${kindIcons[item.kind]}"></i>${kindNames[item.kind]}</span>${item.seconds ? `<span class="media-duration">${item.seconds} 秒</span>` : ''}<button type="button" class="reference-preview" data-preview="${item.id}" aria-label="预览${escapeHTML(item.name)}"><i data-icon="${item.kind === 'image' ? 'eye' : 'play'}"></i></button><button type="button" class="remove-reference" data-remove="${item.id}" aria-label="移除${escapeHTML(item.name)}"><i data-icon="x"></i></button></div><div class="reference-name"><span>${escapeHTML(item.name)}</span><i data-icon="${isPending ? 'info' : isExcluded ? 'x' : 'check'}" aria-hidden="true"></i></div></article>`;
      }).join('')
      : '<button class="empty-references" id="emptyAdd"><i data-icon="plus"></i><strong>添加图片、视频或音频</strong><span>也可以选择图片参考模型，只用文字创作</span></button>';
    const summaries = [`图片 ${totals.image}/9`];
    if (byId('modelSelect').value !== 'sd25image') summaries.push(`视频 ${totals.video}/3 · ${totals.videoSeconds}/15 秒`);
    summaries.push(`音频 ${totals.audio}/3 · ${totals.audioSeconds}/15 秒`);
    if (pending.length) summaries.push(`${pending.length} 项待处理`);
    byId('referenceSummary').innerHTML = summaries.map((summary) => `<span>${summary}</span>`).join('');

    const referenceMessages = messages.filter((message) => !message.startsWith('请填写') && !message.startsWith('画面描述') && !message.startsWith('所选模型最多'));
    byId('referenceIssue').hidden = referenceMessages.length === 0;
    byId('referenceIssue').innerHTML = referenceMessages.map((message) => `<div>${escapeHTML(message)}</div>`).join('') + (pending.length ? '<button id="excludePending">已检查描述，本次不使用这些视频</button>' : '');
    byId('excludedBar').hidden = excluded.length === 0;
    byId('excludedBar').innerHTML = excluded.length
      ? `<span>${excluded.length} 项素材本次未使用，仍保留在草稿中</span>${restorable.length ? '<button id="restoreExcluded">恢复这些素材</button>' : '<span>切换到支持的模型后可恢复</span>'}`
      : '';
    byId('undoBar').hidden = !state.lastRemoved;
    if (state.lastRemoved) byId('undoText').textContent = '已移除 ' + state.lastRemoved.item.name;
    byId('referenceList').dataset.activeCount = String(active.length);
    icons(byId('referenceList'));
  }

  function renderDraft() {
    const model = models[byId('modelSelect').value];
    byId('capabilityText').textContent = model.videoRequired
      ? '支持图片、视频、音频组合参考 · 视频必填'
      : '支持文字生成、图片与音频参考 · 不接受视频';
    for (const option of byId('duration').options) option.disabled = Number(option.value) > model.maxDuration;
    const promptBytes = new TextEncoder().encode(byId('prompt').value.trim()).length;
    byId('promptCount').textContent = `${promptBytes} / 8000 字节`;
    renderReferences();
    const { messages } = validateDraft();
    byId('generate').disabled = messages.length > 0;
    byId('submissionIssue').textContent = messages[0] || '';
    byId('price').innerHTML = `${state.quoteChanged ? '待重新询价' : '生成前询价'} <span>／次</span>`;
    const autosave = document.querySelector('.autosave');
    autosave.setAttribute('aria-label', '草稿仅保存在当前设计预览内存中，刷新页面会恢复示例');
  }

  function draftChanged() {
    state.quoteChanged = true;
    renderDraft();
  }

  function snapshotSettings() {
    return {
      model: byId('modelSelect').value,
      ratio: byId('ratio').value,
      resolution: byId('resolution').value,
      duration: byId('duration').value,
      sound: byId('sound').checked,
      prompt: byId('prompt').value.trim(),
      references: copyReferences(referenceGroups().active),
    };
  }

  function applySettings(settings) {
    byId('modelSelect').value = settings.model;
    byId('ratio').value = settings.ratio;
    byId('resolution').value = settings.resolution;
    byId('duration').value = settings.duration;
    byId('sound').checked = settings.sound;
    byId('prompt').value = settings.prompt;
    state.references = copyReferences(settings.references);
    state.excluded.clear();
    state.lastRemoved = null;
    draftChanged();
  }

  function renderResult() {
    const task = tasks[currentTaskId];
    const settings = task.settings;
    const ready = task.status === 'ready';
    byId('resultTitle').textContent = task.title;
    byId('resultImage').src = mediaPaths[task.scene];
    byId('resultImage').alt = `${task.title}的示例静帧`;
    byId('stageSpec').textContent = `${settings.ratio} · ${settings.resolution}`;
    byId('resultStatus').className = 'status ' + (ready ? 'ready' : 'pending');
    byId('resultStatus').innerHTML = `<span></span>${ready ? '已完成 · 示例' : task.status === 'processing' ? '生成中 · 示例' : '排队中 · 示例'}`;
    byId('resultModel').textContent = models[settings.model].family;
    byId('resultMeta').textContent = `${settings.duration} 秒 · ${settings.resolution} · ${settings.sound ? '含声音' : '静音'} · ${task.time}`;
    byId('taskStage').hidden = ready;
    byId('taskStage').innerHTML = ready ? '' : `<i data-icon="${task.status === 'processing' ? 'loader' : 'clock'}"></i><h3>${task.status === 'processing' ? '正在生成你的画面' : '已加入生成队列'}</h3><p>${task.status === 'processing' ? '你可以继续编辑下一份创作，任务会保留在这里。' : '可以继续编辑，或从近期任务查看其他作品。'}</p><small>仅为设计示例，不会发起真实请求，也不会自动模拟完成。</small>`;
    byId('previewImage').disabled = !ready;
    byId('expandImage').disabled = !ready;
    byId('downloadResult').disabled = !ready;
    const totals = referenceTotals(settings.references);
    document.querySelector('.result-references').innerHTML = '<span>本次使用</span>' + ['image', 'video', 'audio'].filter((kind) => totals[kind]).map((kind) => `<span><i data-icon="${kindIcons[kind]}"></i>${totals[kind]} ${kind === 'image' ? '张图片' : kind === 'video' ? '个视频' : '段音频'}</span>`).join('') + (!settings.references.length ? '<span>纯文字描述</span>' : '');
    for (const card of byId('recentGrid').querySelectorAll('[data-task]')) {
      const selected = card.dataset.task === currentTaskId;
      card.classList.toggle('current', selected);
      card.setAttribute('aria-pressed', String(selected));
      const mark = card.querySelector('.recent-check');
      if (mark) mark.hidden = !selected;
    }
    byId('taskCount').textContent = String(Object.values(tasks).filter((item) => item.status !== 'ready').length);
    byId('recentCount').textContent = String(Object.keys(tasks).length);
    icons(document.querySelector('.result-panel'));
  }

  function addReference(kind) {
    if (!Object.hasOwn(kindNames, kind)) return;
    if (state.references.filter((item) => item.kind === kind).length >= limits[kind]) {
      toast(`草稿中最多保留 ${limits[kind]} 份${kindNames[kind]}，请先移除一份。`);
      return;
    }
    referenceSequence += 1;
    const index = state.references.filter((item) => item.kind === kind).length + 1;
    state.references.push({ id: 'ref-' + referenceSequence, kind, name: `${kind === 'image' ? '主体参考' : kind === 'video' ? '镜头参考' : '声音参考'}-${index}.${kind === 'image' ? 'jpg' : kind === 'video' ? 'mp4' : 'mp3'}`, seconds: kind === 'video' ? 6 : kind === 'audio' ? 5 : 0, scene: 'city' });
    byId('addDialog').close();
    draftChanged();
    toast(`已添加示例${kindNames[kind]}，没有上传真实文件。`);
  }

  function submitSimulation() {
    const { messages } = validateDraft();
    if (messages.length) { toast(messages[0]); return; }
    const settings = snapshotSettings();
    const title = Array.from(settings.prompt).slice(0, 14).join('') || '新的创作';
    const scene = settings.references.find((item) => item.kind !== 'audio')?.scene || 'city';
    taskSequence += 1;
    const id = 'simulation-' + taskSequence;
    tasks[id] = { title, scene, status: 'queued', time: '刚刚 · 模拟提交', settings };
    const card = document.createElement('button');
    card.className = 'recent-card';
    card.type = 'button';
    card.dataset.task = id;
    card.innerHTML = `<div class="recent-image queued"><img src="${mediaPaths[scene]}" alt="${escapeHTML(title)}的参考静帧"><span class="task-cover"><i data-icon="clock"></i>等待生成</span></div><div class="recent-body"><strong>${escapeHTML(title)}</strong><span>${models[settings.model].family}<span class="status-text muted">排队中</span></span></div>`;
    byId('recentGrid').prepend(card);
    currentTaskId = id;
    state.quoteChanged = true;
    renderDraft();
    renderResult();
    icons(card);
    toast('已展示模拟排队状态。未创建真实任务，也未扣费。');
  }

  byId('addMedia').addEventListener('click', () => byId('addDialog').showModal());
  byId('addDialog').addEventListener('click', (event) => {
    const button = event.target.closest('[data-add]');
    if (button) addReference(button.dataset.add);
  });
  byId('referenceList').addEventListener('click', (event) => {
    if (event.target.closest('#emptyAdd')) { byId('addDialog').showModal(); return; }
    const preview = event.target.closest('[data-preview]');
    if (preview) {
      const item = state.references.find((reference) => reference.id === preview.dataset.preview);
      if (!item) return;
      let content;
      if (item.kind === 'image') content = `<img class="reference-preview-image" src="${mediaPaths[item.scene]}" alt="${escapeHTML(item.name)}的示例图片"><p>图片参考 · JPG 格式示例</p>`;
      else if (item.kind === 'video') content = `<video class="reference-preview-video" controls preload="none" poster="${mediaPaths[item.scene]}" aria-label="${escapeHTML(item.name)}的参考视频播放器示意"></video><p>视频参考 · MP4 · ${item.seconds} 秒示例</p><p>设计预览仅提供静帧；正式页面在此播放参考视频。</p>`;
      else content = `<audio class="reference-preview-audio" controls preload="none" aria-label="${escapeHTML(item.name)}的参考音频试听控件示意"></audio><p>音频参考 · MP3 · ${item.seconds} 秒示例</p><p>试听控件示意，未加载实际音频。</p>`;
      showMessage(item.name + ' · 参考素材预览', content);
      return;
    }
    const button = event.target.closest('[data-remove]');
    if (!button) return;
    const index = state.references.findIndex((item) => item.id === button.dataset.remove);
    if (index < 0) return;
    const item = state.references[index];
    state.lastRemoved = { item, index, excluded: state.excluded.has(item.id) };
    state.references.splice(index, 1);
    state.excluded.delete(item.id);
    draftChanged();
    byId('undoRemove').focus();
  });
  byId('undoRemove').addEventListener('click', () => {
    const removed = state.lastRemoved;
    if (!removed) return;
    if (state.references.filter((item) => item.kind === removed.item.kind).length >= limits[removed.item.kind]) { toast('此类素材已达到数量上限，请先移除一份再撤销。'); return; }
    state.references.splice(Math.min(removed.index, state.references.length), 0, removed.item);
    if (removed.excluded) state.excluded.add(removed.item.id);
    state.lastRemoved = null;
    draftChanged();
    byId('addMedia').focus();
  });
  byId('referenceIssue').addEventListener('click', (event) => {
    if (!event.target.closest('#excludePending')) return;
    for (const item of referenceGroups().pending) state.excluded.add(item.id);
    draftChanged();
    toast('已明确排除不兼容素材；草稿仍保留，描述未自动修改。');
  });
  byId('excludedBar').addEventListener('click', (event) => {
    if (!event.target.closest('#restoreExcluded')) return;
    for (const item of referenceGroups().restorable) state.excluded.delete(item.id);
    draftChanged();
    toast('已恢复素材，生成条件和报价需要重新确认。');
  });
  for (const id of ['modelSelect', 'ratio', 'resolution', 'duration', 'sound']) byId(id).addEventListener('change', draftChanged);
  byId('prompt').addEventListener('input', draftChanged);
  byId('clearPrompt').addEventListener('click', () => { byId('prompt').value = ''; draftChanged(); byId('prompt').focus(); });
  byId('generate').addEventListener('click', () => {
    if (validateDraft().messages.length) return;
    showMessage('确认模拟生成', '<p>本页面是交互设计稿，没有上传真实文件、请求模型或取得真实报价。</p><p>点击后仅在右侧展示排队状态，不创建真实任务，也不会扣费。正式产品会先获取并确认当前参数对应的报价。</p>', '模拟提交', submitSimulation);
  });
  byId('newDraft').addEventListener('click', () => showMessage('开始新的创作？', '<p>将清空左侧画面描述与参考素材，当前结果和近期任务会保留。此操作只影响本次设计预览。</p>', '新建创作', () => {
    state.references = [];
    state.excluded.clear();
    state.lastRemoved = null;
    byId('prompt').value = '';
    draftChanged();
    byId('prompt').focus();
  }));
  byId('reuseSettings').addEventListener('click', () => {
    const task = tasks[currentTaskId];
    showMessage('复用这份创作设置？', `<p>将用「${escapeHTML(task.title)}」的模型、描述、素材和参数替换左侧草稿。只加载设置，不提交生成。</p>`, '载入设置', () => { applySettings(task.settings); toast('已载入创作设置，请检查素材和描述后重新询价。'); });
  });
  byId('inspirationButton').addEventListener('click', () => showMessage('试用海上之门样片', '<p>将载入海上之门的示例图片、描述和参数，替换左侧草稿。右侧当前作品保持不变。</p>', '载入灵感', () => { applySettings(tasks.ocean.settings); toast('已载入海上之门示例，可以继续修改描述。'); }));
  byId('recentGrid').addEventListener('click', (event) => {
    const card = event.target.closest('[data-task]');
    if (!card || !tasks[card.dataset.task]) return;
    currentTaskId = card.dataset.task;
    renderResult();
  });
  for (const id of ['previewImage', 'expandImage']) byId(id).addEventListener('click', () => {
    const task = tasks[currentTaskId];
    if (task.status !== 'ready') return;
    byId('imageTitle').textContent = task.title + ' · 静帧示例';
    byId('dialogImage').src = mediaPaths[task.scene];
    byId('dialogImage').alt = task.title + '的静帧示例，不是可播放视频';
    byId('imageDialog').showModal();
  });
  byId('downloadResult').addEventListener('click', () => {
    const task = tasks[currentTaskId];
    if (task.status !== 'ready') return;
    showMessage('下载静帧示例图片', '<p>此设计稿仅提供 WebP 静帧示例，没有生成视频。下载内容为图片文件，不是视频。</p>', '下载示例图片', () => {
      const anchor = document.createElement('a');
      anchor.href = mediaPaths[task.scene];
      anchor.download = `kkai-design-still-${task.scene}.webp`;
      document.body.append(anchor);
      anchor.click();
      anchor.remove();
      toast('正在下载静帧示例图片，文件格式为 WebP。');
    });
  });
  byId('limitsButton').addEventListener('click', () => showMessage('模型素材限制', `<p><strong>${models[byId('modelSelect').value].name}</strong> 的拟启用能力：</p><ul><li>图片最多 9 张；视频最多 3 个，合计最多 15 秒；音频最多 3 段，合计最多 15 秒。</li><li>视频与音频每段为 2–15 秒；视频小于 50 MB，音频小于 15 MB。</li><li>音频不能独立使用，需要搭配图片或视频。</li><li>${models[byId('modelSelect').value].videoRequired ? '该视频参考模型必须包含至少一个有效视频。' : '该模型接受纯文字、图片或图片与音频组合，不接受视频参考。'}</li></ul><p>设计预览的媒体卡是固定示例，不代表已验证的实际模型服务。</p>`));
  byId('priceHelp').addEventListener('click', () => showMessage('费用如何确认', '<p>正式产品根据当前模型、输出参数与有效参考素材取得报价。模型、素材或参数变化后，旧报价失效，必须重新询价。</p><p>参考视频和参考音频的时长上限用于素材校验，不会直接变成输出时长。当前设计稿不连接计费服务，也不显示虚构金额。</p>'));
  byId('resultMore').addEventListener('click', () => {
    const task = tasks[currentTaskId];
    const settings = task.settings;
    showMessage('作品详情 · 设计示例', `<p><strong>${escapeHTML(task.title)}</strong></p><p>${models[settings.model].name} · ${settings.ratio} · ${settings.resolution} · ${settings.duration} 秒</p><p>${escapeHTML(settings.prompt)}</p><p>素材 ${settings.references.length} 项；${settings.sound ? '生成配套声音' : '静音输出'}。本作品仅用于页面设计演示，没有真实任务 ID 或账单。</p>`);
  });
  byId('themeToggle').addEventListener('click', () => {
    const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = next;
    byId('themeToggle').setAttribute('aria-label', next === 'dark' ? '切换浅色主题' : '切换深色主题');
    byId('themeToggle').innerHTML = `<i data-icon="${next === 'dark' ? 'sun' : 'moon'}"></i>`;
    icons(byId('themeToggle'));
  });
  document.addEventListener('click', (event) => {
    const close = event.target.closest('[data-close]');
    if (close) { byId(close.dataset.close).close(); return; }
    const info = event.target.closest('[data-info]');
    if (info) toast(`${info.dataset.info}不在本次视频创作页设计范围内。`);
    if (event.target.closest('#confirmMessage')) {
      const action = messageAction;
      messageAction = null;
      byId('messageDialog').close();
      if (action) action();
    }
  });
  byId('messageDialog').addEventListener('close', () => { messageAction = null; });
  for (const dialog of document.querySelectorAll('dialog')) dialog.addEventListener('click', (event) => {
    if (event.target !== dialog) return;
    const bounds = dialog.getBoundingClientRect();
    if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) dialog.close();
  });

  renderDraft();
  renderResult();
  icons();
})();
