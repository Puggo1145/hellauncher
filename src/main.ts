import { Hud, events, type HudSnapshot, type Stratagem } from "./mygo";

const DIRECTION_ROT: Record<string, number> = {
  L: 0,
  U: 90,
  R: 180,
  D: -90,
};

const INLINE_TACTICAL_ICONS: Record<string, string> = {
  "tactical-chrome": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 126 126" fill="#fff">
    <path fill="#4FB3D0" d="M8 28V8h20L8 28Zm110 0V8H98l20 20ZM8 98v20h20L8 98Zm110 0v20H98l20-20Z"/>
    <path fill="#4FB3D0" d="M63 16a47 47 0 0 1 40.7 23.5H63a23.5 23.5 0 0 0-20.3 11.8L22.3 27.8A46.8 46.8 0 0 1 63 16Z"/>
    <path d="M20 44.3 40.4 79.6A23.5 23.5 0 0 1 40 63c0-4.2 1.1-8.1 3-11.5L23.5 28A47 47 0 0 0 16 63c0 18.6 10.8 34.7 26.5 42.3L63 70a7 7 0 0 1-6-3.5"/>
    <path d="M63 45.5h43.5A47 47 0 0 1 110 63c0 25.9-21 47-47 47l20.3-35.2A23.5 23.5 0 0 0 63 45.5Z"/>
    <circle cx="63" cy="63" r="15" fill="#fff"/>
    <circle cx="63" cy="63" r="8" fill="#4FB3D0"/>
  </svg>`,
  "tactical-zed": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 126 126" fill="#fff">
    <path fill="#4FB3D0" d="M18 14h90l10 10v78l-10 10H18L8 102V24l10-10Zm8 14v70h74V28H26Z"/>
    <path d="M36 38h54v13L54 88h36v12H36V87l36-37H36V38Z"/>
    <path fill="#4FB3D0" d="M59 10h8v10h-8zm0 96h8v10h-8zM10 59h10v8H10zm96 0h10v8h-10z"/>
  </svg>`,
  "tactical-terminal": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 126 126" fill="#fff">
    <path fill="#679452" d="M12 20h102v86H12V20Zm10 18v58h82V38H22Z"/>
    <path d="m32 50 18 15-18 15-6-7 11-8-11-8 6-7Zm24 24h32v9H56v-9Z"/>
  </svg>`,
};

function renderArrowSVG(dir: string, extraClass = ""): string {
  const rot = DIRECTION_ROT[dir] ?? 0;
  return `<svg class="hd2-arrow-svg ${extraClass}" viewBox="0 0 20 20" style="transform: rotate(${rot}deg)">
    <polygon points="13 20 13 15 20 15 20 5 13 5 13 0 0 10 13 20"></polygon>
  </svg>`;
}

function renderStratagemIcon(icon: string): string {
  if (INLINE_TACTICAL_ICONS[icon]) {
    return INLINE_TACTICAL_ICONS[icon];
  }
  return `<img src="/assets/icons/${icon}" alt="" draggable="false" />`;
}

function hasPrefix(code: string[], typed: string[]): boolean {
  if (typed.length > code.length) return false;
  for (let i = 0; i < typed.length; i++) {
    if (code[i] !== typed[i]) return false;
  }
  return true;
}

function triggerHintLabel(mode: string): string {
  switch (mode) {
    case "hold_left_ctrl":
      return "按住左 CTRL 唤出";
    case "right_ctrl_or_alt":
      return "右 CTRL / CTRL+ALT 唤出";
    default:
      return "双击并按住 CTRL 唤出";
  }
}

const hudEl = document.getElementById("stratagem-hud") as HTMLElement;
const badgeEl = document.getElementById("hud-state-badge") as HTMLElement;
const permBannerEl = document.getElementById("permission-banner") as HTMLElement;
const listEl = document.getElementById("hud-stratagem-list") as HTMLElement;
const liveArrowsEl = document.getElementById("live-typed-arrows") as HTMLElement;
const footerHintEl = document.getElementById("hud-footer-hint") as HTMLElement;

let lastErrorSeq = 0;
let lastStepSeq = 0;
let errorTimer: number | null = null;

function render(snap: HudSnapshot): void {
  const typed = snap.typed ?? [];
  const isJustStepped = snap.stepSeq !== lastStepSeq;
  lastStepSeq = snap.stepSeq;

  if (snap.errorSeq !== lastErrorSeq) {
    lastErrorSeq = snap.errorSeq;
    hudEl.classList.remove("input-error");
    void hudEl.offsetWidth;
    hudEl.classList.add("input-error");
    if (errorTimer !== null) {
      window.clearTimeout(errorTimer);
    }
    errorTimer = window.setTimeout(() => {
      hudEl.classList.remove("input-error");
    }, 280);
  }

  const isDeploying = snap.phase === "DEPLOYING";
  const isDeployed = snap.phase === "DEPLOYED" || snap.phase === "EXECUTED";

  // Container phase & 3D visor curvature classes
  hudEl.classList.toggle("visor-curved", snap.visorCurved);
  hudEl.classList.toggle("phase-idle", snap.phase === "IDLE");
  hudEl.classList.toggle("phase-open", snap.phase === "OPEN");
  hudEl.classList.toggle("phase-preview", snap.phase === "PREVIEW");
  hudEl.classList.toggle("phase-deploying", isDeploying);
  hudEl.classList.toggle("phase-executed", isDeployed);

  // Header badge & footer hint
  if (isDeploying) {
    badgeEl.textContent = "空投部署中";
    footerHintEl.textContent = "战备部署中 · 窗口出现后自动收起";
  } else if (isDeployed) {
    badgeEl.textContent = "✓ 已就绪";
    footerHintEl.textContent = "战略配备部署完成";
  } else if (snap.phase === "PREVIEW") {
    badgeEl.textContent = "终端就绪";
    footerHintEl.textContent = triggerHintLabel(snap.triggerMode);
  } else {
    badgeEl.textContent = typed.length > 0 ? `输入中 ${typed.length}` : "WASD // ↑↓←→";
    footerHintEl.textContent = "松开 CTRL 取消 · ESC 退出";
  }

  // macOS Accessibility permission notice
  const needPerm = snap.platform === "darwin" && !snap.trusted;
  permBannerEl.classList.toggle("hidden", !needPerm);

  // Render Stratagem Cards
  listEl.innerHTML = (snap.stratagems ?? [])
    .map((s: Stratagem) => {
      const isDeployingTarget = isDeploying && snap.matchedId === s.id;
      const isDeployedTarget = isDeployed && snap.matchedId === s.id;
      const isMatchedTarget = isDeployingTarget || isDeployedTarget;
      const matchesPrefix = typed.length === 0 || hasPrefix(s.code, typed);

      let rowClass = "strat-row";
      if (isDeployingTarget) {
        rowClass += " row-deploying";
      } else if (isDeployedTarget) {
        rowClass += " row-executed";
      } else if (isDeploying || isDeployed) {
        rowClass += " prefix-dimmed";
      } else if (typed.length > 0) {
        rowClass += matchesPrefix ? " prefix-active" : " prefix-dimmed";
      }

      const arrowsHTML = s.code
        .map((dir, idx) => {
          let cls = "";
          if (isMatchedTarget || (matchesPrefix && idx < typed.length)) {
            cls = "matched";
            if (isJustStepped && idx === typed.length - 1) {
              cls += " just-matched";
            }
          }
          return renderArrowSVG(dir, cls);
        })
        .join("");

      let subHTML: string;
      if (isDeployingTarget) {
        const actionLabel = s.kind === "close_active_window" ? "正在执行清场…" : "正在唤醒窗口…";
        subHTML = `<span class="deploy-status-pill"><span class="deploy-spinner" aria-hidden="true"></span>${actionLabel}</span>`;
      } else if (isDeployedTarget) {
        subHTML = "✓ 已就绪 DEPLOYED";
      } else {
        subHTML = escapeHTML(s.subtitle);
      }

      const progressBarHTML = isDeployingTarget
        ? `<div class="deploy-progress-track" aria-hidden="true"><div class="deploy-progress-bar"></div></div>`
        : isDeployedTarget
          ? `<div class="deploy-progress-track deploy-progress-done" aria-hidden="true"></div>`
          : "";

      return `
        <div class="${rowClass}" data-id="${escapeHTML(s.id)}">
          <div class="strat-icon-box cat-${escapeHTML(s.category)}">
            ${renderStratagemIcon(s.icon)}
          </div>
          <div class="strat-info">
            <div class="strat-title-line">
              <span class="strat-name">${escapeHTML(s.name)}</span>
              <span class="strat-subtitle">${subHTML}</span>
            </div>
            <div class="strat-arrows-row">${arrowsHTML}</div>
          </div>
          ${progressBarHTML}
        </div>
      `;
    })
    .join("");

  // Render Live Typed Sequence Strip
  if (typed.length === 0) {
    liveArrowsEl.innerHTML = `<span class="live-typed-placeholder">等待输入方向指令…</span>`;
  } else {
    liveArrowsEl.innerHTML = typed
      .map((dir, idx) =>
        renderArrowSVG(dir, idx === typed.length - 1 && isJustStepped ? "matched just-matched" : "matched"),
      )
      .join("");
  }
}

function escapeHTML(str: string): string {
  return str
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

// Subscribe to real-time HUD state updates from Go
events.hudState.on((snap) => {
  render(snap);
});

(window as unknown as { __helldriverRender?: typeof render }).__helldriverRender = render;

// Initial hydration
Hud.snapshot()
  .then((snap) => render(snap))
  .catch((err) => console.error("[HellDriver] snapshot error:", err));
