import type { NetworkCaptureConfig, ProviderModel, WebProviderConfig } from "../types.js";

const commonTextInputSelectors = [
  "textarea",
  "div[contenteditable='true']",
  "[role='textbox']",
];

const commonDomOutput = [
  {
    type: "main_text" as const,
    selector: "main",
    excludeSelectors: ["nav", "form", "[class*='sidebar']", "[class*='input']"],
    minLength: 20,
  },
];

const webModel = {
  id: "web",
  label: "Web",
} satisfies ProviderModel;

function networkCapture(
  name: string,
  urlContains: string,
  options: {
    parser?: NetworkCaptureConfig["parser"];
    method?: string;
    contentTypeContains?: string;
    timeoutMs?: number;
  } = {},
): NetworkCaptureConfig {
  return {
    trigger: "dom",
    parser: options.parser,
    listen: [
      {
        name,
        method: options.method ?? "POST",
        urlContains,
        contentTypeContains: options.contentTypeContains,
        timeoutMs: options.timeoutMs ?? 180000,
      },
    ],
    parsers: [
      { type: "sse", events: ["message"], extract: "text" },
      { type: "text", extract: "image_urls" },
    ],
  };
}

function webProvider(config: {
  providerId: string;
  label: string;
  aliases?: string[];
  authProfileId: string;
  startUrl: string;
  inputSelectors?: string[];
  pageUrlPatterns?: string[];
  preActions?: WebProviderConfig["domDriver"]["preActions"];
  waitPolicy?: WebProviderConfig["domDriver"]["waitPolicy"];
  networkCapture: NetworkCaptureConfig;
  requiresVisibleBrowser?: boolean;
}): WebProviderConfig {
  return {
    providerId: config.providerId,
    label: config.label,
    type: "web",
    enabled: true,
    aliases: config.aliases,
    authProfileId: config.authProfileId,
    browserProfileId: "chrome_main",
    transportStrategy: {
      primary: "browser_network_capture",
      requiresCdp: true,
      requiresVisibleBrowser: config.requiresVisibleBrowser,
    },
    models: [webModel],
    domDriver: {
      startUrl: config.startUrl,
      pageUrlPatterns: config.pageUrlPatterns,
      inputSelectors: config.inputSelectors ?? commonTextInputSelectors,
      preActions: config.preActions,
      sendActions: [{ type: "keyboard", key: "Enter" }],
      waitPolicy: config.waitPolicy,
      outputExtractors: commonDomOutput,
    },
    networkCapture: config.networkCapture,
  };
}

export function buildDefaultZeroTokenProviders(): WebProviderConfig[] {
  return [
    webProvider({
      providerId: "doubao",
      label: "Doubao",
      aliases: ["doubao-web"],
      authProfileId: "auth_doubao_main",
      startUrl: "https://www.doubao.com/chat/",
      pageUrlPatterns: ["doubao.com/chat"],
      inputSelectors: ["[contenteditable='true']", "textarea", "[role='textbox']"],
      preActions: [{ type: "click_if_exists", selector: "[data-testid='text-mode']", timeoutMs: 3000 }],
      waitPolicy: {
        type: "stable_text",
        maxWaitMs: 120000,
        pollIntervalMs: 2000,
        stableRounds: 3,
      },
      networkCapture: networkCapture("doubao_chat_completion", "doubao.com/chat/completion", {
        parser: "doubao-sse",
        contentTypeContains: "text/event-stream",
      }),
      requiresVisibleBrowser: true,
    }),
    webProvider({
      providerId: "qwen",
      label: "Qwen",
      aliases: ["qwen-web"],
      authProfileId: "auth_qwen_main",
      startUrl: "https://chat.qwen.ai/",
      networkCapture: networkCapture("qwen_chat_completion", "chat.qwen.ai/api/v2/chat/completions"),
    }),
    webProvider({
      providerId: "qwen-cn",
      label: "Qwen CN",
      aliases: ["qwen-cn-web"],
      authProfileId: "auth_qwen_cn_main",
      startUrl: "https://chat2.qianwen.com/",
      networkCapture: networkCapture("qwen_cn_chat", "chat2.qianwen.com/api/v2/chat"),
    }),
    webProvider({
      providerId: "gemini",
      label: "Gemini",
      aliases: ["gemini-web"],
      authProfileId: "auth_gemini_main",
      startUrl: "https://gemini.google.com/app",
      inputSelectors: [
        "textarea[placeholder*='Gemini']",
        "textarea[placeholder*='问问']",
        "textarea[aria-label*='prompt']",
        ...commonTextInputSelectors,
      ],
      networkCapture: networkCapture("gemini_generate", "gemini.google.com"),
    }),
    webProvider({
      providerId: "chatgpt",
      label: "ChatGPT",
      aliases: ["chatgpt-web"],
      authProfileId: "auth_chatgpt_main",
      startUrl: "https://chatgpt.com/",
      inputSelectors: ["#prompt-textarea", ...commonTextInputSelectors],
      networkCapture: networkCapture("chatgpt_conversation", "chatgpt.com/backend-api/conversation"),
    }),
    webProvider({
      providerId: "claude",
      label: "Claude",
      aliases: ["claude-web"],
      authProfileId: "auth_claude_main",
      startUrl: "https://claude.ai/",
      networkCapture: networkCapture("claude_completion", "claude.ai/api"),
    }),
    webProvider({
      providerId: "deepseek",
      label: "DeepSeek",
      aliases: ["deepseek-web"],
      authProfileId: "auth_deepseek_main",
      startUrl: "https://chat.deepseek.com/",
      networkCapture: networkCapture("deepseek_completion", "chat.deepseek.com/api/v0/chat/completion"),
    }),
    webProvider({
      providerId: "kimi",
      label: "Kimi",
      aliases: ["kimi-web"],
      authProfileId: "auth_kimi_main",
      startUrl: "https://www.kimi.com/",
      networkCapture: networkCapture("kimi_chat", "kimi.gateway.chat.v1.ChatService/Chat"),
    }),
    webProvider({
      providerId: "glm",
      label: "GLM",
      aliases: ["glm-web"],
      authProfileId: "auth_glm_main",
      startUrl: "https://chatglm.cn/",
      networkCapture: networkCapture("glm_stream", "chatglm.cn/chatglm/backend-api/assistant/stream"),
    }),
    webProvider({
      providerId: "glm-intl",
      label: "GLM International",
      aliases: ["glm-intl-web"],
      authProfileId: "auth_glm_intl_main",
      startUrl: "https://chat.z.ai/",
      networkCapture: networkCapture("glm_intl_chat", "chat.z.ai/api/chat"),
    }),
    webProvider({
      providerId: "grok",
      label: "Grok",
      aliases: ["grok-web"],
      authProfileId: "auth_grok_main",
      startUrl: "https://grok.com/",
      networkCapture: networkCapture("grok_conversation", "grok.com/rest/app-chat/conversations"),
    }),
    webProvider({
      providerId: "perplexity",
      label: "Perplexity",
      aliases: ["perplexity-web"],
      authProfileId: "auth_perplexity_main",
      startUrl: "https://www.perplexity.ai/",
      networkCapture: networkCapture("perplexity_search", "perplexity.ai"),
    }),
    webProvider({
      providerId: "xiaomimo",
      label: "Xiaomi MiMo",
      aliases: ["xiaomimo-web"],
      authProfileId: "auth_xiaomimo_main",
      startUrl: "https://mimo.mi.com/",
      networkCapture: networkCapture("xiaomimo_chat", "mimo.mi.com/api/chat"),
    }),
  ];
}

export function buildDefaultBrowserProfiles() {
  return [
    {
      profileId: "chrome_main",
      mode: "attach_only" as const,
      cdpUrl: "http://127.0.0.1:9222",
      headless: false,
      defaultTimeoutMs: 120000,
    },
  ];
}
