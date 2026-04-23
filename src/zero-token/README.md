# ZeroToken Provider Module

统一封装 `doubao`、`qwen`、`gemini` 等 Web Provider，对外提供稳定的 Provider Runtime。

## 能力

- 通过 `providerRef` 选择 Provider，例如 `doubao/web`
- 所有 Provider 固定使用 `web` 模型
- 统一使用 DOM 输入 + `browser_network_capture` 捕获结果
- 支持 Debug Chrome / CDP 连接

## 快速使用

```ts
import {
  ZeroTokenRuntime,
  buildDefaultBrowserProfiles,
  buildDefaultZeroTokenProviders,
} from "./zero-token/index.js";

const runtime = new ZeroTokenRuntime({
  providers: buildDefaultZeroTokenProviders(),
  browserProfiles: buildDefaultBrowserProfiles(),
  authProfiles: [
    {
      authProfileId: "auth_doubao_main",
      providerId: "doubao",
      authType: "cookie",
      cookie: "sessionid=xxx; ttwid=yyy",
      userAgent: "Mozilla/5.0",
    },
  ],
});

const result = await runtime.generate({
  requestId: "req_001",
  providerRef: "doubao/web",
  capability: "text_generation",
  input: {
    prompt: "children storybook illustration, a cute cloud floating in the night sky",
    aspectRatio: "16:9",
    resolution: "2K",
    count: 1,
  },
});

console.log(result.output.images);
```

## Storyboard 生成

```ts
const result = await runtime.generate({
  requestId: "req_storyboard_001",
  providerRef: "qwen/web",
  capability: "storyboard_generation",
  input: {
    prompt: "请将下面的儿童故事转换为严格 JSON storyboard...",
  },
});
```

## Debug Chrome

默认浏览器配置连接：

```text
http://127.0.0.1:9222
```

推荐使用项目脚本启动 Chrome Debug 模式：

```bash
npm run chrome:debug
```

该脚本会：

- 自动探测 macOS / Linux / WSL / Windows Git Bash
- 自动查找 Chrome / Chromium 路径
- 使用独立的 ZeroToken Chrome 用户数据目录
- 开放 CDP 端口 `9222`
- 默认优先复用已启动的 Debug Chrome
- 打开常用 Web Provider 登录页

可选环境变量：

```bash
ZERO_TOKEN_CHROME_DEBUG_PORT=9222 npm run chrome:debug
ZERO_TOKEN_OPEN_LOGIN_PAGES=0 npm run chrome:debug
ZERO_TOKEN_CHROME_USER_DATA_DIR=/tmp/zero-token-chrome npm run chrome:debug
ZERO_TOKEN_RESTART_CHROME=1 npm run chrome:debug
CHROME_PATH=/path/to/chrome npm run chrome:debug
```

默认策略：

```text
如果 http://127.0.0.1:9222/json/version 可访问，脚本会直接复用现有 Chrome，不会 kill 旧进程。
只有显式设置 ZERO_TOKEN_RESTART_CHROME=1 时，脚本才会尝试停止旧 Debug Chrome 并重新启动。
```

Windows PowerShell 也可以手动启动：

```powershell
& "C:\Program Files\Google\Chrome\Application\chrome.exe" `
  --remote-debugging-port=9222 `
  --user-data-dir="C:\tmp\zero-token-chrome"
```

## Provider Ref

格式：

```text
provider/model
```

示例：

```text
doubao/web
qwen/web
gemini/web
glm/web
```

## 首版建议

视频生产系统首版建议启用：

- `doubao/web`：Storyboard + 图片生成
- `qwen/web`：Storyboard fallback
- `gemini/web`：Storyboard fallback
- `glm/web`：Storyboard fallback

## Web Demo

启动体验页面：

```bash
npm run dev:web
```

默认地址：

```text
http://127.0.0.1:4317
```

页面提供：

- Provider 列表
- Prompt 输入
- 结果 JSON 预览
- 图片结果预览

真实调用 Web Provider 前，需要先启动 Debug Chrome，并在对应站点完成登录。

本项目不需要执行 OpenClaw 的 `./onboard.sh webauth`。登录态直接保存在 Debug Chrome 使用的独立用户数据目录中，ZeroToken Runtime 通过 CDP 复用这个浏览器会话。
