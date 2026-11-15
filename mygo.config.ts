import { defineConfig } from "mygo-cli";

export default defineConfig({
  name: "HelLauncher",
  identifier: "com.superearth.hellauncher",
  version: "0.2.0",
  publisher: "Super Earth Command",
  resourcesExclude: ["icon.png"],
  out: "build",
  macos: {
    minimumSystemVersion: "13.0",
    signingIdentity: "Apple Development",
    infoPlist: {
      LSUIElement: true,
    },
  },
  windows: {
    displayName: "HelLauncher",
    installDirName: "HelLauncher",
  },
  linux: {
    webview: false,
  },
});
