#!/usr/bin/env node
import { spawn, execFileSync } from "node:child_process";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";

const root = process.cwd();
const isWin = process.platform === "win32";
const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "hellauncher-e2e-"));
const sockPath = path.join(tmpDir, "auto.sock");
const dataDir = path.join(tmpDir, "data");
const binPath = path.join(tmpDir, isWin ? "hellauncher-e2e.exe" : "hellauncher-e2e");
const bridgeBinPath = path.join(tmpDir, "unixbridge.exe");

function cleanup() {
  try {
    fs.rmSync(tmpDir, { recursive: true, force: true, maxRetries: 6, retryDelay: 80 });
  } catch {}
}

async function waitForSocket(file, timeoutMs = 10000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (fs.existsSync(file)) {
      return;
    }
    await new Promise((r) => setTimeout(r, 60));
  }
  throw new Error(`Timed out waiting for MYGO_AUTOMATION socket at ${file}`);
}

async function connectAutomation(file) {
  if (!isWin) {
    await waitForSocket(file);
    return await new Promise((resolve, reject) => {
      const c = net.createConnection(file, () => resolve(c));
      c.on("error", reject);
    });
  }

  // On Windows, Node's net.createConnection(string) targets Named Pipes rather
  // than AF_UNIX reparse-point sockets; use Go's native AF_UNIX dialer bridge.
  const bridge = spawn(bridgeBinPath, [file], {
    cwd: root,
    stdio: ["pipe", "pipe", "pipe"],
  });

  await new Promise((resolve, reject) => {
    let errLog = "";
    const onErrData = (chunk) => {
      errLog += chunk.toString("utf8");
      if (errLog.includes("__READY__")) {
        bridge.stderr.off("data", onErrData);
        resolve();
      }
    };
    bridge.stderr.on("data", onErrData);
    bridge.once("error", reject);
    bridge.once("exit", (code) => {
      if (!errLog.includes("__READY__")) {
        reject(new Error(`unixbridge exited (${code}): ${errLog.trim()}`));
      }
    });
  });

  return {
    setEncoding(enc) {
      bridge.stdout.setEncoding(enc);
    },
    on(ev, fn) {
      bridge.stdout.on(ev, fn);
    },
    write(data) {
      bridge.stdin.write(data);
    },
    destroy() {
      try {
        bridge.stdin.end();
      } catch {}
      if (bridge.exitCode === null) {
        bridge.kill();
      }
    },
  };
}

class AutomationClient {
  constructor(socket) {
    this.socket = socket;
    this.nextId = 1;
    this.pending = new Map();
    let buf = "";
    socket.setEncoding("utf8");
    socket.on("data", (chunk) => {
      buf += chunk;
      let idx;
      while ((idx = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, idx).trim();
        buf = buf.slice(idx + 1);
        if (!line) continue;
        const msg = JSON.parse(line);
        const entry = this.pending.get(msg.id);
        if (entry) {
          this.pending.delete(msg.id);
          if (msg.error) {
            entry.reject(new Error(`${msg.error.code}: ${msg.error.message}`));
          } else {
            entry.resolve(msg.result);
          }
        }
      }
    });
  }

  call(method, params = undefined) {
    const id = this.nextId++;
    const payload = JSON.stringify(params ? { id, method, params } : { id, method }) + "\n";
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.socket.write(payload);
    });
  }
}

async function main() {
  console.log("▶ Building E2E binary (CGO_ENABLED=0)...");
  execFileSync("go", ["build", "-tags", "e2e", "-o", binPath, "."], {
    cwd: root,
    env: { ...process.env, CGO_ENABLED: "0" },
    stdio: "inherit",
  });
  if (isWin) {
    execFileSync("go", ["build", "-o", bridgeBinPath, "./scripts/e2e/unixbridge"], {
      cwd: root,
      env: { ...process.env, CGO_ENABLED: "0" },
      stdio: "inherit",
    });
  }

  console.log("▶ Launching HelLauncher with MYGO_AUTOMATION...");
  const child = spawn(binPath, [], {
    cwd: root,
    env: {
      ...process.env,
      MYGO_ENV: "development",
      MYGO_AUTOMATION: sockPath,
      HELLAUNCHER_E2E: "1",
      HELLAUNCHER_E2E_DEPLOY: process.env.HELLAUNCHER_E2E_SCREENSHOT_DIR ? "1" : "0",
      HELLAUNCHER_NO_AUTO_GUIDE: "1",
      HELLAUNCHER_OPEN_ARSENAL: "1",
      HELLAUNCHER_DATA_DIR: dataDir,
    },
    stdio: ["ignore", "pipe", "pipe"],
  });

  let stderrBuf = "";
  child.stderr.on("data", (d) => {
    stderrBuf += d.toString();
  });

  let sock = null;
  try {
    sock = await connectAutomation(sockPath);
    const client = new AutomationClient(sock);

    const { windows: wins } = await client.call("windows");
    console.log(
      `  Found ${wins.length} live native GPU windows:`,
      wins.map((w) => w.title),
    );

    const hudWin = wins.find((w) => w.title.includes("Tactical HUD"));
    const arsenalWin = wins.find((w) => w.title.includes("军械库") || w.title.includes("Arsenal"));
    if (!hudWin || !arsenalWin) {
      throw new Error(`Expected both Tactical HUD and Arsenal windows, got: ${JSON.stringify(wins)}`);
    }

    await client.call("idle", { window: hudWin.id, timeout: 2000 });
    const hudTree = await client.call("tree", { window: hudWin.id });
    const hudText = JSON.stringify(hudTree);
    if (
      !hudText.includes("轨道精准攻击：关闭窗口") ||
      !hudText.includes("呼叫文件补给箱") ||
      !hudText.includes("加入绝地潜兵")
    ) {
      throw new Error("HUD native UI tree missing expected default Stratagem rows");
    }

    await client.call("idle", { window: arsenalWin.id, timeout: 2000 });
    const arsenalTree = await client.call("tree", { window: arsenalWin.id });
    const arsenalText = JSON.stringify(arsenalTree);
    if (
      !arsenalText.includes("战备配置") ||
      !arsenalText.includes("面板外观") ||
      !arsenalText.includes("通用设置") ||
      !arsenalText.includes("+ 新建") ||
      !arsenalText.includes("加入绝地潜兵")
    ) {
      throw new Error("Arsenal native UI tree missing expected top navigation or configurator elements");
    }

    const screenshotOut = process.env.HELLAUNCHER_E2E_SCREENSHOT_DIR;
    if (!screenshotOut) {
      // Verify live Onboarding GPU window opens cleanly as a single seamless layer during fast smoke check
      await client.call("act", { window: arsenalWin.id, key: "nav-settings", action: "press" });
      await client.call("idle", { window: arsenalWin.id, timeout: 1200 });
      await client.call("act", { window: arsenalWin.id, key: "btn-replay-onboarding", action: "press" });
      await new Promise((r) => setTimeout(r, 260));
      const { windows: winsNow } = await client.call("windows");
      const onbWin = winsNow.find((w) => w.title.includes("入列") || w.title.includes("Enlistment"));
      if (!onbWin) {
        throw new Error(`Expected Onboarding window in smoke check, got: ${JSON.stringify(winsNow)}`);
      }
      if (winsNow.some((w) => w.title.includes("军械库") || w.title.includes("Arsenal"))) {
        throw new Error(`Expected Arsenal window to close when entering Onboarding, got: ${JSON.stringify(winsNow)}`);
      }
      await new Promise((r) => setTimeout(r, 180));
      const onbTree = JSON.stringify(await client.call("tree", { window: onbWin.id }));
      if (!onbTree.includes("跳过播片") || onbTree.includes("全频段强制插播")) {
        throw new Error("Onboarding native UI tree did not match clean HD broadcast layout");
      }
      await client.call("act", { window: onbWin.id, key: "onb-btn-skip-all", action: "press" });
      await new Promise((r) => setTimeout(r, 200));
    }
    if (screenshotOut) {
      fs.mkdirSync(screenshotOut, { recursive: true });
      const findNodeIdByLabel = (treeObj, label) => {
        const hit = (treeObj.nodes || []).find((n) => n.label === label && (n.actions || []).includes("press"));
        return hit ? hit.id : null;
      };

      // 1. Capture Tab 0: 战备配置 (Stratagems)
      const arsStratCap = await client.call("capture", { window: arsenalWin.id });
      fs.writeFileSync(path.join(screenshotOut, "arsenal-tab-stratagems.png"), Buffer.from(arsStratCap.png, "base64"));

      // Wait for simulated stratagem sequence -> PhaseDeploying, capture Classic deploying frame
      await new Promise((r) => setTimeout(r, 850));
      const depClassic = await client.call("capture", { window: hudWin.id });
      fs.writeFileSync(path.join(screenshotOut, "hud-classic-deploying.png"), Buffer.from(depClassic.png, "base64"));

      // Open 111-preset Official Stratagem Picker and capture, then close popover
      await client.call("act", { window: arsenalWin.id, key: "btn-pick-icon", action: "press" });
      await client.call("idle", { window: arsenalWin.id, timeout: 1000 });
      const arsPickerCap = await client.call("capture", { window: arsenalWin.id });
      fs.writeFileSync(path.join(screenshotOut, "arsenal-icon-picker.png"), Buffer.from(arsPickerCap.png, "base64"));
      await client.call("act", { window: arsenalWin.id, key: "btn-pick-icon", action: "press" });
      await client.call("idle", { window: arsenalWin.id, timeout: 1000 });

      // Select System Stratagem: 加入绝地潜兵 (Enlistment Terminal) and capture
      await client.call("act", { window: arsenalWin.id, key: "roster-item-sys-join-helldivers", action: "press" });
      await client.call("idle", { window: arsenalWin.id, timeout: 1000 });
      const arsEnlistCap = await client.call("capture", { window: arsenalWin.id });
      fs.writeFileSync(path.join(screenshotOut, "arsenal-enlistment.png"), Buffer.from(arsEnlistCap.png, "base64"));

      // 2. Switch to Tab 1: 面板外观 (Appearance — Classic theme: NO 3D Visor option!) and capture
      await client.call("act", { window: arsenalWin.id, key: "nav-appearance", action: "press" });
      await client.call("idle", { window: arsenalWin.id, timeout: 1500 });
      const arsAppCap = await client.call("capture", { window: arsenalWin.id });
      fs.writeFileSync(path.join(screenshotOut, "arsenal-tab-appearance.png"), Buffer.from(arsAppCap.png, "base64"));

      // Wait for deployment + hold + exit fade to finish, then toggle theme cards to re-trigger Classic HUD
      await new Promise((r) => setTimeout(r, 2350));
      await client.call("act", { window: arsenalWin.id, key: "theme-card-tactical", action: "press" });
      await client.call("idle", { window: arsenalWin.id, timeout: 1000 });
      await client.call("act", { window: arsenalWin.id, key: "theme-card-classic", action: "press" });
      await client.call("idle", { window: arsenalWin.id, timeout: 1000 });
      await new Promise((r) => setTimeout(r, 260));
      const hudCap = await client.call("capture", { window: hudWin.id });
      fs.writeFileSync(path.join(screenshotOut, "hud-classic-live.png"), Buffer.from(hudCap.png, "base64"));

      // 3. Switch to Tab 2: 通用设置 (Settings) and capture
      await client.call("act", { window: arsenalWin.id, key: "nav-settings", action: "press" });
      await client.call("idle", { window: arsenalWin.id, timeout: 1500 });
      await client.call("act", { window: arsenalWin.id, key: "btn-ship-random", action: "press" });
      await client.call("idle", { window: arsenalWin.id, timeout: 1000 });
      const arsSetCap = await client.call("capture", { window: arsenalWin.id });
      fs.writeFileSync(path.join(screenshotOut, "arsenal-tab-settings.png"), Buffer.from(arsSetCap.png, "base64"));

      // 4. Switch Language to EN on Settings tab, then open Appearance tab and select Tactical theme
      let treeNow = await client.call("tree", { window: arsenalWin.id });
      const enId = findNodeIdByLabel(treeNow, "EN");
      if (enId) {
        await client.call("act", { window: arsenalWin.id, id: enId, action: "press" });
        await client.call("idle", { window: arsenalWin.id, timeout: 1500 });
      }
      await client.call("act", { window: arsenalWin.id, key: "nav-appearance", action: "press" });
      await client.call("idle", { window: arsenalWin.id, timeout: 1500 });
      await client.call("act", { window: arsenalWin.id, key: "theme-card-tactical", action: "press" });
      await client.call("idle", { window: arsenalWin.id, timeout: 1500 });
      await new Promise((r) => setTimeout(r, 260));

      const arsEnCap = await client.call("capture", { window: arsenalWin.id });
      fs.writeFileSync(path.join(screenshotOut, "arsenal-en-tactical.png"), Buffer.from(arsEnCap.png, "base64"));
      const hudTacCap = await client.call("capture", { window: hudWin.id });
      fs.writeFileSync(path.join(screenshotOut, "hud-tactical-en.png"), Buffer.from(hudTacCap.png, "base64"));

      // Switch back to 中文 + Classic before testing the full Desktop-Immersed Onboarding flow!
      await client.call("act", { window: arsenalWin.id, key: "theme-card-classic", action: "press" });
      await client.call("idle", { window: arsenalWin.id, timeout: 1000 });
      await client.call("act", { window: arsenalWin.id, key: "nav-settings", action: "press" });
      await client.call("idle", { window: arsenalWin.id, timeout: 1200 });
      treeNow = await client.call("tree", { window: arsenalWin.id });
      const zhId = findNodeIdByLabel(treeNow, "中文");
      if (zhId) {
        await client.call("act", { window: arsenalWin.id, id: zhId, action: "press" });
        await client.call("idle", { window: arsenalWin.id, timeout: 1200 });
      }

      // 5. Launch Seamless Single-Canvas Onboarding & capture all clean moments!
      await client.call("act", { window: arsenalWin.id, key: "btn-replay-onboarding", action: "press" });
      await new Promise((r) => setTimeout(r, 4400));
      const { windows: winsWithOnb } = await client.call("windows");
      const onbWin = winsWithOnb.find(
        (w) => w.title.includes("入列") || w.title.includes("Enlistment"),
      );
      if (!onbWin) {
        throw new Error(`Expected Onboarding window to open, got: ${JSON.stringify(winsWithOnb)}`);
      }
      if (winsWithOnb.some((w) => w.title.includes("军械库") || w.title.includes("Arsenal"))) {
        throw new Error(`Arsenal window must not remain open behind Onboarding: ${JSON.stringify(winsWithOnb)}`);
      }

      // 5a. Stage 0A: HD 2560x1200 @ 30fps Helldivers 2 MP4 montage (Act II mid-action + Act IV ridge climax)
      const onbMontageCap = await client.call("capture", { window: onbWin.id });
      if (!(onbMontageCap.width > 0 && onbMontageCap.height > 0)) {
        throw new Error(`Expected non-zero onboarding surface dimensions, got ${onbMontageCap.width}x${onbMontageCap.height}`);
      }
      console.log(`  Onboarding fullscreen surface: ${onbMontageCap.width}x${onbMontageCap.height}`);
      fs.writeFileSync(path.join(screenshotOut, "onboarding-0a-montage.png"), Buffer.from(onbMontageCap.png, "base64"));

      await new Promise((r) => setTimeout(r, 8250));
      const onbClimaxCap = await client.call("capture", { window: onbWin.id });
      fs.writeFileSync(path.join(screenshotOut, "onboarding-0a-climax-ridge.png"), Buffer.from(onbClimaxCap.png, "base64"));

      // 5b. Stage 0B: Authentic Helldivers 2 Hero Logo + angular CTA
      const preHeroTree = JSON.stringify(await client.call("tree", { window: onbWin.id }));
      if (!preHeroTree.includes("onb-btn-enlist-now")) {
        await client.call("act", { window: onbWin.id, key: "onb-btn-skip-video", action: "press" });
      }
      await client.call("idle", { window: onbWin.id, timeout: 1200 });
      const onbHeroCap = await client.call("capture", { window: onbWin.id });
      fs.writeFileSync(path.join(screenshotOut, "onboarding-0b-hero-title.png"), Buffer.from(onbHeroCap.png, "base64"));

      // 5c. Stage 1 (01 / 04): HUD Theme + live upper-left HUD (no 上一步 button on step 1)
      await client.call("act", { window: onbWin.id, key: "onb-btn-enlist-now", action: "press" });
      await client.call("idle", { window: onbWin.id, timeout: 1200 });
      const stage1Tree = JSON.stringify(await client.call("tree", { window: onbWin.id }));
      if (stage1Tree.includes("上一步")) {
        throw new Error("Stage 1 must not show 上一步 button");
      }
      const onbThemeCap = await client.call("capture", { window: onbWin.id });
      fs.writeFileSync(path.join(screenshotOut, "onboarding-1-theme.png"), Buffer.from(onbThemeCap.png, "base64"));

      // 5d. Stage 2 (02 / 04): Summon Key + live upper-left HUD
      await client.call("act", { window: onbWin.id, key: "onb-next", action: "press" });
      await client.call("idle", { window: onbWin.id, timeout: 1200 });
      const onbHotkeyCap = await client.call("capture", { window: onbWin.id });
      fs.writeFileSync(path.join(screenshotOut, "onboarding-2-hotkey.png"), Buffer.from(onbHotkeyCap.png, "base64"));

      // 5e. Stage 3 (03 / 04): Loadout Pod Grid on the SAME canvas (zero extra windows!)
      await client.call("act", { window: onbWin.id, key: "onb-next", action: "press" });
      await client.call("idle", { window: onbWin.id, timeout: 1200 });
      await client.call("act", { window: onbWin.id, key: "onb-pod-split", action: "press" });
      await client.call("idle", { window: onbWin.id, timeout: 800 });
      await client.call("act", { window: onbWin.id, key: "onb-pod-browser", action: "press" });
      await client.call("idle", { window: onbWin.id, timeout: 800 });
      const { windows: winsStage3 } = await client.call("windows");
      if (winsStage3.some((w) => w.title.includes("军械库") || w.title.includes("Arsenal"))) {
        throw new Error(`Stage 3 Loadout must never pop open an Arsenal window: ${JSON.stringify(winsStage3)}`);
      }
      const onbLoadoutCap = await client.call("capture", { window: onbWin.id });
      fs.writeFileSync(path.join(screenshotOut, "onboarding-3-loadout.png"), Buffer.from(onbLoadoutCap.png, "base64"));

      // 5f. Stage 4 (04 / 04): Call In Resupply visual equation + completion
      await client.call("act", { window: onbWin.id, key: "onb-next", action: "press" });
      await client.call("idle", { window: onbWin.id, timeout: 1200 });
      const onbDrillBeforeCap = await client.call("capture", { window: onbWin.id });
      fs.writeFileSync(path.join(screenshotOut, "onboarding-4a-drill.png"), Buffer.from(onbDrillBeforeCap.png, "base64"));

      for (const stepIdx of [0, 1, 2, 3]) {
        await client.call("act", { window: onbWin.id, key: `drill-step-${stepIdx}`, action: "press" });
        await client.call("idle", { window: onbWin.id, timeout: 600 });
      }
      const onbDrillCap = await client.call("capture", { window: onbWin.id });
      fs.writeFileSync(path.join(screenshotOut, "onboarding-4b-drill-ready.png"), Buffer.from(onbDrillCap.png, "base64"));

      await client.call("act", { window: onbWin.id, key: "onb-btn-finish-dive", action: "press" });
      await new Promise((r) => setTimeout(r, 350));
      const hudAfterCap = await client.call("capture", { window: hudWin.id });
      fs.writeFileSync(path.join(screenshotOut, "hud-after-onboarding.png"), Buffer.from(hudAfterCap.png, "base64"));

      console.log(`  Saved live GPU window & deploying animation captures to ${screenshotOut}`);
    }

    try {
      await Promise.race([
        client.call("quit"),
        new Promise((r) => setTimeout(r, 400)),
      ]);
    } catch {}
    sock.destroy();
    sock = null;
    console.log("✓ Live MyGo GPU E2E smoke check passed.");
  } catch (err) {
    console.error("E2E failed:", err);
    if (stderrBuf) console.error("App stderr:\n" + stderrBuf);
    process.exitCode = 1;
  } finally {
    if (sock) {
      try {
        sock.destroy();
      } catch {}
    }
    if (child.exitCode === null) {
      child.kill("SIGTERM");
      await Promise.race([
        new Promise((r) => child.once("exit", r)),
        new Promise((r) => setTimeout(r, 800)),
      ]);
      if (child.exitCode === null) {
        child.kill("SIGKILL");
        await Promise.race([
          new Promise((r) => child.once("exit", r)),
          new Promise((r) => setTimeout(r, 400)),
        ]);
      }
    }
    cleanup();
  }
}

main().then(() => process.exit(process.exitCode ?? 0));
