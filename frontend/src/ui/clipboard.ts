// 剪贴板工具：设置页的 MCP 接入片段与公章片段共用。
//
// 优先 navigator.clipboard（本机回环地址属于安全上下文）；不可用时回落
// execCommand，与迁移前的 app.js copyText 行为一致。

/** 复制文本成功返回 true；不可用或命令失败返回 false。 */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const area = document.createElement("textarea");
    area.value = text;
    document.body.appendChild(area);
    area.select();
    let ok = false;
    try {
      ok = document.execCommand("copy");
    } catch {
      /* ignore */
    }
    area.remove();
    return ok;
  }
}
