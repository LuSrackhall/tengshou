// 清理上一次的前端构建产物，但保留入库的 dist/.keep：
// go:embed 要求 web/app/dist 在编译期始终存在，干净 clone 也能 go build。
import { mkdirSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const dist = join(here, "..", "..", "internal", "httpapi", "web", "app", "dist");

mkdirSync(dist, { recursive: true });
for (const name of readdirSync(dist)) {
  if (name === ".keep") continue;
  rmSync(join(dist, name), { recursive: true, force: true });
}
writeFileSync(join(dist, ".keep"), "", { flag: "a" });
