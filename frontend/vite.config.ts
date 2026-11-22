import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

// 阶段 0：Vue 以「岛」的形式挂在存量页面里。构建产物固定文件名
// tengshou-ui.js / tengshou-ui.css，由 Go 侧 //go:embed 打进单二进制；
// 页面仍然先加载旧 app.js，新旧入口共存。
export default defineConfig(({ mode }) => ({
  plugins: [vue()],
  define: {
    // lib 模式不会像普通 app 构建那样替换 process.env.NODE_ENV；
    // 浏览器里没有 process，必须显式注入，否则整包在浏览器中抛错。
    "process.env.NODE_ENV": JSON.stringify(mode === "production" ? "production" : "development"),
    __VUE_OPTIONS_API__: "true",
    __VUE_PROD_DEVTOOLS__: "false",
    __VUE_PROD_HYDRATION_MISMATCH_DETAILS__: "false",
  },
  server: {
    // 开发预览页直接引用仓库内真实的 app.css（位于前端工程目录之外）。
    fs: { allow: [".."] },
  },
  build: {
    outDir: "../internal/httpapi/web/app/dist",
    // dist 里有入库的 .keep（go:embed 需要目录始终存在），清理交给
    // scripts/clean-dist.mjs，避免 Vite 把它一起删掉。
    emptyOutDir: false,
    target: "es2022",
    sourcemap: false,
    lib: {
      entry: "src/main.ts",
      formats: ["es"],
      fileName: () => "tengshou-ui.js",
    },
    rollupOptions: {
      output: {
        assetFileNames: "tengshou-ui[extname]",
        chunkFileNames: "tengshou-ui-[name].js",
      },
    },
  },
}));
