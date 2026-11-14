#!/usr/bin/env node
import { spawn, execFileSync } from "node:child_process";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";

const root = process.cwd();
const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "hellauncher-e2e-"));
const sockPath = path.join(tmpDir, "auto.sock");
const dataDir = path.join(tmpDir, "data");
const binPath = path.join(tmpDir, process.platform === "win32" ? "hellauncher-e2e.exe" : "hellauncher-e2e");

function cleanup() {
  try {
    fs.rmSync(tmpDir, { recursive: true, force: true });
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
  execFileSync("go", ["build", "-o", binPath, "."], {
    cwd: root,
    env: { ...process.env, CGO_ENABLED: "0" },
    stdio: "inherit",
  });

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

  try {
    await waitForSocket(sockPath);
    const sock = await new Promise((resolve, reject) => {
      const c = net.createConnection(sockPath, () => resolve(c));
      c.on("error", reject);
    });
    const client = new AutomationClient(sock);

    const { windows: wins } = await client.call("windows");
    console.log(`  Found ${wins.length} live native GPU windows:`, wins.map((w) => w.title));

    const hudWin = wins.find((w) => w.title.includes("Tactical HUD"));
    const arsenalWin = wins.find((w) => w.title.includes("军械库"));
    if (!hudWin || !arsenalWin) {
      throw new Error(`Expected both Tactical HUD and Arsenal windows, got: ${JSON.stringify(wins)}`);
    }

    await client.call("idle", { window: hudWin.id, timeout: 2000 });
    const hudTree = await client.call("tree", { window: hudWin.id });
    const hudText = JSON.stringify(hudTree);
    if (!hudText.includes("STRATAGEMS") || !hudText.includes("Google Chrome")) {
      throw new Error("HUD native UI tree missing expected Stratagems header/rows");
    }

    await client.call("idle", { window: arsenalWin.id, timeout: 2000 });
    const arsenalTree = await client.call("tree", { window: arsenalWin.id });
    const arsenalText = JSON.stringify(arsenalTree);
    if (!arsenalText.includes("SUPER EARTH ARSENAL") || !arsenalText.includes("+ 新建战备")) {
      throw new Error("Arsenal native UI tree missing expected configurator elements");
    }

    const screenshotOut = process.env.HELLAUNCHER_E2E_SCREENSHOT_DIR;
    if (screenshotOut) {
      fs.mkdirSync(screenshotOut, { recursive: true });
      const hudCap = await client.call("capture", { window: hudWin.id });
      const arsCap = await client.call("capture", { window: arsenalWin.id });
      fs.writeFileSync(path.join(screenshotOut, "hud-live.png"), Buffer.from(hudCap.png, "base64"));
      fs.writeFileSync(path.join(screenshotOut, "arsenal-live.png"), Buffer.from(arsCap.png, "base64"));

      // Wait for simulated stratagem sequence -> PhaseDeploying, then sample 3 live animation frames
      await new Promise((r) => setTimeout(r, 950));
      for (let i = 1; i <= 3; i++) {
        const depCap = await client.call("capture", { window: hudWin.id });
        fs.writeFileSync(path.join(screenshotOut, `hud-deploying-${i}.png`), Buffer.from(depCap.png, "base64"));
        if (i < 3) await new Promise((r) => setTimeout(r, 230));
      }
      console.log(`  Saved live GPU window & deploying animation captures to ${screenshotOut}`);
    }

    sock.destroy();
    console.log("✓ Live MyGo GPU E2E smoke check passed.");
  } catch (err) {
    console.error("E2E failed:", err);
    if (stderrBuf) console.error("App stderr:\n" + stderrBuf);
    process.exitCode = 1;
  } finally {
    if (child.exitCode === null) {
      child.kill("SIGTERM");
      await Promise.race([
        new Promise((r) => child.once("exit", r)),
        new Promise((r) => setTimeout(r, 600)),
      ]);
      if (child.exitCode === null) {
        child.kill("SIGKILL");
      }
    }
    cleanup();
  }
}

main().then(() => process.exit(process.exitCode ?? 0));
