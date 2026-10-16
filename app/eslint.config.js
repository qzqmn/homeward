// https://docs.expo.dev/guides/using-eslint/
const { defineConfig } = require('eslint/config');
const expoConfig = require("eslint-config-expo/flat");

module.exports = defineConfig([
  expoConfig,
  {
    // public/ 底下是原封不動提供出去的靜態檔（maplibre worker 等第三方打包產物），
    // 不是我們的原始碼，不該被 lint（滿滿的 no-var 都是壓縮後的產物）
    ignores: ["dist/*", "public/*", "*.min.js"],
  }
]);
