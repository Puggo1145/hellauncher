#!/usr/bin/env node
import { execFileSync } from "node:child_process";

const root = process.cwd();

console.log("▶ Running unit tests (CGO_ENABLED=0)...");
execFileSync("go", ["test", "./..."], {
  cwd: root,
  env: { ...process.env, CGO_ENABLED: "0" },
  stdio: "inherit",
});
console.log("✓ All unit tests passed.");
