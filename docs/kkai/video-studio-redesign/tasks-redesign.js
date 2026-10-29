/* Standalone review prototype. It makes no API or generation requests. */
const icon = (name) => `<svg aria-hidden="true"><use href="#i-${name}"/></svg>`;
const escapeHtml = (value) =>
  String(value).replaceAll(
    /[&<>"']/g,
    (character) =>
      ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&#39;",
      })[character],
  );

const tasks = [
  {
    id: 62,
    taskId: "task_D1Jr0FoDmrtyKcjTvMIXgF40bZwJOGON",
    status: "ready",
    time: "10月11日 21:11",
    model: "sd_2.0_fast_special_720p",
    mode: "文生视频",
    ratio: "16:9",
    duration: "5 秒",
    image: "assets/city-ride-cinematic.webp",
    prompt:
      "生成一段严格 5 秒、16:9、24fps、写实电影级科幻质感的视频。单镜头连续拍摄，不切镜、不跳帧，优先保证主体外观和动作的时间一致性。\n\n场景：黎明前的雨后城市屋顶，深蓝色天空与远处暖金色天际线形成冷暖对比。镜头从街道低位平稳推进，水珠反射微弱的灯光。骑手始终保持同一外观，画面光影、金属反射和镜头运动保持自然连贯。",
  },
  {
    id: 61,
    taskId: "task_FTmGyLm9BqIB6daxNXhPKE6IfoqZO6qx",
    status: "failed",
    time: "10月11日 15:08",
    model: "sd_2.0_fast_special_720p",
    mode: "文生视频",
    ratio: "16:9",
    duration: "5 秒",
    prompt:
      "生成一段严格 5 秒、16:9、24fps、写实电影级科幻质感的视频。镜头穿过雨后街道，霓虹在湿润的路面形成柔和反射。保持画面主体与镜头运动的连续性。",
    failure: "任务提交失败，请稍后重试。",
  },
  {
    id: 60,
    taskId: "task_gWlH8HPWt0SqJxAN0q8xDycIrtP0yuS0",
    status: "ready",
    time: "8月26日 15:26",
    model: "sd_2.0_fast_special_720p",
    mode: "图生视频",
    ratio: "16:9",
    duration: "5 秒",
    image: "assets/cinema-tools-still-life.webp",
    prompt:
      "金属机械装置的镜头在暗色摄影棚内缓缓移动。保持物体结构稳定，突出表面水珠、边缘轮廓与柔和的侧面光。镜头平稳前进，不切镜。",
  },
];

const statusNames = {
  ready: "可播放",
  failed: "视频不可用",
  processing: "正在生成视频",
  queued: "排队中",
  archiving: "正在准备视频",
};
let scene = "ready";
let selectedId = 62;
let toastTimer;
const $ = (selector) => document.querySelector(selector);

function currentTasks() {
  if (scene === "empty" || scene === "error") return [];
  if (["processing", "queued", "archiving"].includes(scene)) {
    return [{ ...tasks[0], status: scene, image: undefined }, ...tasks.slice(1)];
  }
  return tasks;
}

function statusMarkup(status) {
  let symbol = "clock";
  if (status === "ready") symbol = "check";
  if (status === "failed") symbol = "alert";
  return `<span class="status ${status}">${icon(symbol)}${statusNames[status]}</span>`;
}

function taskMarkup(task) {
  const cover = task.image
    ? `<img class="task-cover" src="${task.image}" alt="" loading="lazy">`
    : `<span class="task-placeholder">${icon("film")}</span>`;
  return `<button type="button" class="task-row" data-task="${task.id}" aria-pressed="${selectedId === task.id}">
    ${cover}
    <span class="task-copy">
      <span class="task-topline">${statusMarkup(task.status)}<time>${task.time}</time></span>
      <span class="task-summary">${escapeHtml(task.prompt)}</span>
      <span class="task-meta"><span>${escapeHtml(task.model)}</span><span>·</span><span>${task.mode}</span></span>
      <span class="task-bottom">${task.status === "failed" ? escapeHtml(task.failure) : "查看任务详情"} ${icon("arrow")}</span>
    </span>
  </button>`;
}

function taskIdMarkup(task) {
  return `<div class="task-id"><code>${escapeHtml(task.taskId)}</code><button type="button" class="icon-button" data-action="copy-task" data-task="${task.id}" aria-label="复制任务号" title="复制任务号">${icon("copy")}</button></div>`;
}

function metadataMarkup(task) {
  return `<dl class="metadata">
    <div><dt>模型</dt><dd>${escapeHtml(task.model)}</dd></div>
    <div><dt>生成模式</dt><dd>${task.mode}</dd></div>
    <div><dt>画幅比例</dt><dd>${task.ratio}</dd></div>
    <div><dt>时长</dt><dd>${task.duration}</dd></div>
    <div><dt>创建时间</dt><dd>${task.time}</dd></div>
  </dl>`;
}

function detailMarkup(task, mobile = false) {
  const title = task.status === "ready" ? "当前作品" : "任务详情";
  const heading = task.status === "failed" ? "这项任务未能完成" : `视频任务 #${task.id}`;
  const header = `<header class="detail-header"><div><p class="detail-label">${title}</p><h3>${heading}</h3></div><div class="detail-actions">${statusMarkup(task.status)}</div>${mobile ? `<button type="button" class="icon-button drawer-close" data-action="close-detail" aria-label="关闭详情">${icon("close")}</button>` : ""}</header>`;
  const prompt = `<section class="info-section"><h4>完整提示词</h4><p class="full-prompt">${escapeHtml(task.prompt)}</p></section>`;
  const meta = `<section class="info-section"><h4>生成信息</h4>${metadataMarkup(task)}</section>`;

  if (task.status === "failed") {
    return `${header}<div class="failure-body"><div class="failure-message">${icon("alert")}<div><strong>视频暂不可用</strong><p>${escapeHtml(task.failure)}</p></div></div><div class="failure-grid"><div>${prompt}</div><div>${meta}${taskIdMarkup(task)}</div></div></div><footer class="detail-footer"><p>任务信息已保留</p><button type="button" class="quiet-action" data-action="open-task">查看任务 ${icon("arrow")}</button></footer>`;
  }

  let stageMessage = "视频正在生成，完成后可在这里查看。";
  if (task.status === "queued") stageMessage = "任务已受理，等待开始。";
  if (task.status === "archiving") {
    stageMessage = "生成已完成，正在准备播放文件。";
  }
  const media =
    task.status === "ready"
      ? `<div class="media-frame"><img src="${task.image}" alt="视频封面预览"><button type="button" data-action="preview" data-task="${task.id}" aria-label="放大查看封面" title="放大查看封面">${icon("expand")}</button></div><div class="media-caption"><strong>视频封面</strong><span>${task.ratio} · ${task.duration}</span></div>`
      : `<div class="stage-frame"><div>${icon("film")}<strong>${statusNames[task.status]}</strong><p>${stageMessage}</p></div></div>`;
  const action =
    task.status === "ready"
      ? `<button type="button" class="primary-action" data-action="view-work">查看作品 ${icon("arrow")}</button>`
      : `<button type="button" class="quiet-action" data-action="open-task">查看任务 ${icon("arrow")}</button>`;
  return `${header}<div class="detail-body"><div class="detail-media">${media}</div><div class="detail-info">${prompt}${meta}${taskIdMarkup(task)}</div></div><footer class="detail-footer"><p>${task.status === "ready" ? "已完成 · 可前往作品库查看" : "状态更新后可刷新查看"}</p>${action}</footer>`;
}

function notify(message) {
  const toast = $("#toast");
  clearTimeout(toastTimer);
  toast.textContent = message;
  toast.classList.add("visible");
  toastTimer = setTimeout(() => toast.classList.remove("visible"), 2600);
}

function render() {
  const items = currentTasks();
  $("#review-scene").value = scene;
  const workspace = $("#task-workspace");
  if (scene === "empty" || scene === "error") {
    workspace.innerHTML = `<section class="${scene === "empty" ? "empty-state" : "error-state"}">${icon(scene === "empty" ? "film" : "alert")}<h3>${scene === "empty" ? "还没有视频任务" : "任务暂时无法加载"}</h3><p>${scene === "empty" ? "开始一次创作，任务会出现在这里。" : "请检查网络后重试，已有作品不会受影响。"}</p><button class="${scene === "empty" ? "primary-action" : "quiet-action"}" type="button" data-action="${scene === "empty" ? "create" : "retry"}">${scene === "empty" ? "开始创作" : "重试"} ${icon("arrow")}</button></section>`;
    return;
  }
  if (!items.some((task) => task.id === selectedId)) selectedId = items[0].id;
  workspace.innerHTML = `<section class="queue" aria-label="视频任务列表"><div class="queue-heading"><h3>最近任务</h3><span id="loaded-count-inner">已加载 ${items.length} 项</span></div><div class="task-list">${items.map(taskMarkup).join("")}</div><div class="load-more"><button type="button" data-action="load-more">加载更多 ${icon("arrow")}</button></div></section><section class="detail" id="desktop-detail" aria-label="当前任务详情">${detailMarkup(items.find((task) => task.id === selectedId))}</section>`;
  $("#mobile-detail").innerHTML = detailMarkup(
    items.find((task) => task.id === selectedId),
    true,
  );
}

$("#review-scene").addEventListener("change", (event) => {
  scene = event.target.value;
  selectedId = scene === "failed" ? 61 : 62;
  $("#mobile-detail").close();
  render();
});

$("#refresh-button").addEventListener("click", (event) => {
  event.currentTarget.classList.add("is-busy");
  setTimeout(() => event.currentTarget.classList.remove("is-busy"), 650);
  notify("设计稿仅演示刷新反馈，未连接任务服务");
});

document.addEventListener("click", async (event) => {
  const row = event.target.closest("[data-task].task-row");
  if (row) {
    selectedId = Number(row.dataset.task);
    render();
    if (matchMedia("(max-width:1100px)").matches) {
      $("#mobile-detail").showModal();
    } else {
      $(`.task-row[data-task="${selectedId}"]`).focus();
    }
    return;
  }
  const action = event.target.closest("[data-action]");
  if (!action) return;
  if (action.dataset.action === "close-detail") {
    $("#mobile-detail").close();
  } else if (action.dataset.action === "close-preview") {
    $("#preview-dialog").close();
  } else if (action.dataset.action === "preview") {
    const task = currentTasks().find((item) => item.id === Number(action.dataset.task));
    $("#preview-dialog img").src = task.image;
    $("#preview-dialog").showModal();
  } else if (action.dataset.action === "copy-task") {
    const task = currentTasks().find((item) => item.id === Number(action.dataset.task));
    try {
      await navigator.clipboard.writeText(task.taskId);
      notify("任务号已复制");
    } catch {
      notify("当前浏览器不支持复制，请手动选择任务号");
    }
  } else if (action.dataset.action === "retry") {
    scene = "ready";
    render();
    notify("已重新加载示例任务");
  } else if (action.dataset.action === "load-more") {
    notify("设计稿仅展示当前示例任务");
  } else if (
    action.dataset.action === "view-work" ||
    action.dataset.action === "open-task" ||
    action.dataset.action === "create"
  ) {
    notify("此操作在正式页面中跳转；设计稿不连接业务服务");
  }
});

const initialScene = new URLSearchParams(location.search).get("scene");
if (
  Object.hasOwn(statusNames, initialScene) ||
  initialScene === "empty" ||
  initialScene === "error"
) {
  scene = initialScene;
  selectedId = scene === "failed" ? 61 : 62;
}
render();
