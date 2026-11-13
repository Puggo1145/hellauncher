import { defineConfig } from "mygo-cli";

export default defineConfig({
  name: "HellDriver",
  identifier: "com.superearth.helldriver",
  version: "0.2.0",
  out: "build",
  macos: {
    minimumSystemVersion: "13.0",
    signingIdentity: "Apple Development",
    infoPlist: {
      LSUIElement: true,
    },
  },
});
