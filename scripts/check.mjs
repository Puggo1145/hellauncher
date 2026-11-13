#!/usr/bin/env node
import { execFileSync } from "node:child_process";

const root = process.cwd();

function run(label, cmd, args, extraEnv = {}) {
  process.stdout.write(`▶ ${label}... `);
  try {
    const out = execFileSync(cmd, args, {
      cwd: root,
      env: { ...process.env, CGO_ENABLED: "0", ...extraEnv },
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
    if (label === "gofmt" && out.trim().length > 0) {
      console.error("FAILED\nUnformatted Go files:\n" + out);
      process.exit(1);
    }
    console.log("OK");
  } catch (err) {
    console.error("FAILED");
    if (err.stdout) process.stderr.write(err.stdout);
    if (err.stderr) process.stderr.write(err.stderr);
    process.exit(1);
  }
}

run("gofmt", "gofmt", ["-l", "."]);
run("mygo vet (UI ownership & goroutine capture)", "bun", ["x", "mygo", "vet", "."]);
run("go vet (darwin/arm64 CGO_ENABLED=0)", "go", ["vet", "./..."], {
  GOOS: "darwin",
  GOARCH: "arm64",
});
run("go vet (windows/amd64 CGO_ENABLED=0)", "go", ["vet", "./..."], {
  GOOS: "windows",
  GOARCH: "amd64",
});
run("go vet (linux/amd64 CGO_ENABLED=0)", "go", ["vet", "./..."], {
  GOOS: "linux",
  GOARCH: "amd64",
});

console.log("✓ All static & cross-platform checks passed.");
