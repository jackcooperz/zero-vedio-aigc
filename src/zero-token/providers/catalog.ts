import type { WebProviderConfig } from "../types.js";
import type { ProviderModel } from "../types.js";

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

function textModel(id: string, label?: string): ProviderModel {
  return {
    id,
    label,
    capabilities: ["storyboard_generation", "text_generation"] as const,
  };
}

export function buildDefaultZeroTokenProviders(): WebProviderConfig[] {
  return [
    {
      providerId: "doubao-web",
      label: "Doubao Web",
      type: "web",
      enabled: true,
      aliases: ["doubao"],
      authProfileId: "auth_doubao_main",
      browserProfileId: "chrome_main",
      transportStrategy: {
        primary: "browser_network_capture",
        fallbacks: ["browser_dom", "web_http_replay"],
        requiresCdp: true,
        requiresVisibleBrowser: true,
      },
      models: [
        textModel("doubao-seed-2.0", "Doubao Seed 2.0"),
        textModel("doubao-pro", "Doubao Pro"),
        {
          id: "seedream-4.5",
          label: "Seedream 4.5",
          capabilities: ["image_generation"],
        },
      ],
      capabilities: {
        storyboard_generation: {
          enabled: true,
          models: ["doubao-seed-2.0", "doubao-pro"],
        },
        text_generation: {
          enabled: true,
          models: ["doubao-seed-2.0", "doubao-pro"],
        },
        image_generation: {
          enabled: true,
          models: ["seedream-4.5"],
          maxCount: 4,
          supportedAspectRatios: ["1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9"],
          supportedResolutions: ["1K", "2K", "4K"],
        },
      },
      domDriver: {
        startUrl: "https://www.doubao.com/chat/",
        pageUrlPatterns: ["doubao.com/chat"],
        inputSelectors: [
          "[contenteditable='true']",
          "textarea",
          "[role='textbox']",
        ],
        preActions: [
          { type: "click_if_exists", selector: "[data-testid='text-mode']", timeoutMs: 3000 },
        ],
        sendActions: [{ type: "keyboard", key: "Enter" }],
        waitPolicy: {
          type: "stable_text",
          maxWaitMs: 120000,
          pollIntervalMs: 2000,
          stableRounds: 3,
        },
        outputExtractors: commonDomOutput,
      },
      networkCapture: {
        trigger: "dom",
        parser: "doubao-web-sse",
        listen: [
          {
            name: "doubao_chat_completion",
            method: "POST",
            urlContains: "doubao.com/chat/completion",
            contentTypeContains: "text/event-stream",
            timeoutMs: 180000,
          },
        ],
        parsers: [
          { type: "sse", events: ["CHUNK_DELTA", "STREAM_CHUNK", "SSE_REPLY_END"], extract: "text" },
          { type: "doubao_creation_block", extract: "image_urls" },
        ],
      },
      httpReplay: {
        baseUrl: "https://www.doubao.com",
        endpoint: "/samantha/chat/completion",
        method: "POST",
        authSources: ["cookie", "user_agent", "query_params"],
        dynamicParams: ["msToken", "a_bogus", "fp", "tea_uuid", "device_id", "web_tab_id"],
        responseMode: "sse",
        parser: "doubao-web-sse",
      },
    },
    {
      providerId: "qwen-web",
      label: "Qwen Web",
      type: "web",
      enabled: true,
      aliases: ["qwen"],
      authProfileId: "auth_qwen_main",
      browserProfileId: "chrome_main",
      transportStrategy: {
        primary: "browser_eval_fetch",
        fallbacks: ["browser_dom"],
        requiresCdp: true,
      },
      models: [textModel("qwen3.5-plus", "Qwen 3.5 Plus")],
      capabilities: {
        storyboard_generation: { enabled: true, models: ["qwen3.5-plus"] },
        text_generation: { enabled: true, models: ["qwen3.5-plus"] },
      },
      evalFetch: {
        startUrl: "https://chat.qwen.ai/",
        bootstrapSteps: [
          {
            name: "create_chat",
            method: "POST",
            url: "https://chat.qwen.ai/api/v2/chats/new",
            body: {},
          },
        ],
        sendMessage: {
          method: "POST",
          urlTemplate: "https://chat.qwen.ai/api/v2/chat/completions?chat_id={{chat_id}}",
          bodyTemplate: {
            stream: true,
            model: "{{model}}",
            messages: [{ role: "user", content: "{{prompt}}" }],
          },
        },
        responseMode: "stream",
      },
      domDriver: {
        startUrl: "https://chat.qwen.ai/",
        inputSelectors: commonTextInputSelectors,
        sendActions: [{ type: "keyboard", key: "Enter" }],
        outputExtractors: commonDomOutput,
      },
    },
    {
      providerId: "qwen-cn-web",
      label: "Qwen CN Web",
      type: "web",
      enabled: true,
      authProfileId: "auth_qwen_cn_main",
      browserProfileId: "chrome_main",
      transportStrategy: {
        primary: "browser_eval_fetch",
        fallbacks: ["browser_dom"],
        requiresCdp: true,
      },
      models: [textModel("qwen-cn-plus", "Qwen CN Plus")],
      capabilities: {
        storyboard_generation: { enabled: true, models: ["qwen-cn-plus"] },
        text_generation: { enabled: true, models: ["qwen-cn-plus"] },
      },
      evalFetch: {
        startUrl: "https://chat2.qianwen.com/",
        sendMessage: {
          method: "POST",
          urlTemplate: "https://chat2.qianwen.com/api/v2/chat",
          bodyTemplate: {
            model: "{{model}}",
            messages: [{ role: "user", content: "{{prompt}}" }],
          },
        },
        responseMode: "json",
      },
      domDriver: {
        startUrl: "https://chat2.qianwen.com/",
        inputSelectors: commonTextInputSelectors,
        sendActions: [{ type: "keyboard", key: "Enter" }],
        outputExtractors: commonDomOutput,
      },
    },
    {
      providerId: "gemini-web",
      label: "Gemini Web",
      type: "web",
      enabled: true,
      authProfileId: "auth_gemini_main",
      browserProfileId: "chrome_main",
      transportStrategy: {
        primary: "browser_dom",
        fallbacks: [],
        requiresCdp: true,
      },
      models: [textModel("gemini-web", "Gemini Web")],
      capabilities: {
        storyboard_generation: { enabled: true, models: ["gemini-web"] },
        text_generation: { enabled: true, models: ["gemini-web"] },
      },
      domDriver: {
        startUrl: "https://gemini.google.com/app",
        inputSelectors: [
          "textarea[placeholder*='Gemini']",
          "textarea[placeholder*='问问']",
          "textarea[aria-label*='prompt']",
          ...commonTextInputSelectors,
        ],
        sendActions: [{ type: "keyboard", key: "Enter" }],
        outputExtractors: commonDomOutput,
      },
    },
    {
      providerId: "chatgpt-web",
      label: "ChatGPT Web",
      type: "web",
      enabled: true,
      authProfileId: "auth_chatgpt_main",
      browserProfileId: "chrome_main",
      transportStrategy: {
        primary: "browser_dom",
        fallbacks: ["browser_eval_fetch"],
        requiresCdp: true,
      },
      models: [textModel("gpt-web", "ChatGPT Web")],
      capabilities: {
        storyboard_generation: { enabled: true, models: ["gpt-web"] },
        text_generation: { enabled: true, models: ["gpt-web"] },
      },
      domDriver: {
        startUrl: "https://chatgpt.com/",
        inputSelectors: ["#prompt-textarea", ...commonTextInputSelectors],
        sendActions: [{ type: "keyboard", key: "Enter" }],
        outputExtractors: commonDomOutput,
      },
      evalFetch: {
        startUrl: "https://chatgpt.com/",
        sendMessage: {
          method: "POST",
          urlTemplate: "https://chatgpt.com/backend-api/conversation",
          bodyTemplate: {
            action: "next",
            messages: [{ role: "user", content: { content_type: "text", parts: ["{{prompt}}"] } }],
            model: "{{model}}",
          },
        },
        responseMode: "stream",
      },
    },
    {
      providerId: "claude-web",
      label: "Claude Web",
      type: "web",
      enabled: true,
      authProfileId: "auth_claude_main",
      browserProfileId: "chrome_main",
      transportStrategy: {
        primary: "browser_dom",
        fallbacks: ["web_http_replay", "browser_eval_fetch"],
        requiresCdp: true,
      },
      models: [
        textModel("claude-sonnet-web", "Claude Sonnet Web"),
        textModel("claude-opus-web", "Claude Opus Web"),
      ],
      capabilities: {
        storyboard_generation: { enabled: true, models: ["claude-sonnet-web", "claude-opus-web"] },
        text_generation: { enabled: true, models: ["claude-sonnet-web", "claude-opus-web"] },
      },
      domDriver: {
        startUrl: "https://claude.ai/",
        inputSelectors: commonTextInputSelectors,
        sendActions: [{ type: "keyboard", key: "Enter" }],
        outputExtractors: commonDomOutput,
      },
      httpReplay: {
        baseUrl: "https://claude.ai",
        endpoint: "/api/organizations/{{organizationId}}/chat_conversations/{{conversationId}}/completion",
        method: "POST",
        authSources: ["cookie", "user_agent", "headers"],
        responseMode: "sse",
      },
    },
    {
      providerId: "deepseek-web",
      label: "DeepSeek Web",
      type: "web",
      enabled: true,
      authProfileId: "auth_deepseek_main",
      browserProfileId: "chrome_main",
      transportStrategy: {
        primary: "hybrid",
        fallbacks: ["web_http_replay", "browser_dom"],
        requiresCdp: false,
      },
      models: [textModel("deepseek-chat-web", "DeepSeek Chat Web")],
      capabilities: {
        storyboard_generation: { enabled: true, models: ["deepseek-chat-web"] },
        text_generation: { enabled: true, models: ["deepseek-chat-web"] },
      },
      domDriver: {
        startUrl: "https://chat.deepseek.com/",
        inputSelectors: commonTextInputSelectors,
        sendActions: [{ type: "keyboard", key: "Enter" }],
        outputExtractors: commonDomOutput,
      },
      httpReplay: {
        baseUrl: "https://chat.deepseek.com",
        endpoint: "/api/v0/chat/completion",
        method: "POST",
        authSources: ["cookie", "user_agent", "headers"],
        responseMode: "sse",
      },
    },
    {
      providerId: "kimi-web",
      label: "Kimi Web",
      type: "web",
      enabled: true,
      authProfileId: "auth_kimi_main",
      browserProfileId: "chrome_main",
      transportStrategy: {
        primary: "browser_dom",
        fallbacks: ["browser_eval_fetch"],
        requiresCdp: true,
      },
      models: [textModel("kimi-web", "Kimi Web")],
      capabilities: {
        storyboard_generation: { enabled: true, models: ["kimi-web"] },
        text_generation: { enabled: true, models: ["kimi-web"] },
      },
      domDriver: {
        startUrl: "https://www.kimi.com/",
        inputSelectors: commonTextInputSelectors,
        sendActions: [{ type: "keyboard", key: "Enter" }],
        outputExtractors: commonDomOutput,
      },
      evalFetch: {
        startUrl: "https://www.kimi.com/",
        sendMessage: {
          method: "POST",
          urlTemplate: "https://www.kimi.com/apiv2/kimi.gateway.chat.v1.ChatService/Chat",
          bodyTemplate: {
            kimiplus_id: "{{model}}",
            messages: [{ role: "user", content: "{{prompt}}" }],
          },
        },
        responseMode: "stream",
      },
    },
    {
      providerId: "glm-web",
      label: "GLM Web",
      type: "web",
      enabled: true,
      authProfileId: "auth_glm_main",
      browserProfileId: "chrome_main",
      transportStrategy: {
        primary: "browser_eval_fetch",
        fallbacks: ["browser_dom"],
        requiresCdp: true,
      },
      models: [textModel("glm-web", "GLM Web")],
      capabilities: {
        storyboard_generation: { enabled: true, models: ["glm-web"] },
        text_generation: { enabled: true, models: ["glm-web"] },
      },
      evalFetch: {
        startUrl: "https://chatglm.cn/",
        sendMessage: {
          method: "POST",
          urlTemplate: "https://chatglm.cn/chatglm/backend-api/assistant/stream",
          bodyTemplate: {
            assistant_id: "{{model}}",
            prompt: "{{prompt}}",
          },
        },
        responseMode: "stream",
      },
      domDriver: {
        startUrl: "https://chatglm.cn/",
        inputSelectors: commonTextInputSelectors,
        sendActions: [{ type: "keyboard", key: "Enter" }],
        outputExtractors: commonDomOutput,
      },
    },
    {
      providerId: "glm-intl-web",
      label: "GLM International Web",
      type: "web",
      enabled: true,
      authProfileId: "auth_glm_intl_main",
      browserProfileId: "chrome_main",
      transportStrategy: {
        primary: "browser_eval_fetch",
        fallbacks: ["browser_dom"],
        requiresCdp: true,
      },
      models: [textModel("glm-intl-web", "GLM International Web")],
      capabilities: {
        storyboard_generation: { enabled: true, models: ["glm-intl-web"] },
        text_generation: { enabled: true, models: ["glm-intl-web"] },
      },
      evalFetch: {
        startUrl: "https://chat.z.ai/",
        sendMessage: {
          method: "POST",
          urlTemplate: "https://chat.z.ai/api/chat",
          bodyTemplate: {
            model: "{{model}}",
            messages: [{ role: "user", content: "{{prompt}}" }],
          },
        },
        responseMode: "stream",
      },
      domDriver: {
        startUrl: "https://chat.z.ai/",
        inputSelectors: commonTextInputSelectors,
        sendActions: [{ type: "keyboard", key: "Enter" }],
        outputExtractors: commonDomOutput,
      },
    },
    {
      providerId: "grok-web",
      label: "Grok Web",
      type: "web",
      enabled: true,
      authProfileId: "auth_grok_main",
      browserProfileId: "chrome_main",
      transportStrategy: {
        primary: "browser_dom",
        fallbacks: ["browser_eval_fetch"],
        requiresCdp: true,
      },
      models: [textModel("grok-web", "Grok Web")],
      capabilities: {
        storyboard_generation: { enabled: true, models: ["grok-web"] },
        text_generation: { enabled: true, models: ["grok-web"] },
      },
      domDriver: {
        startUrl: "https://grok.com/",
        inputSelectors: commonTextInputSelectors,
        sendActions: [{ type: "keyboard", key: "Enter" }],
        outputExtractors: commonDomOutput,
      },
      evalFetch: {
        startUrl: "https://grok.com/",
        sendMessage: {
          method: "POST",
          urlTemplate: "https://grok.com/rest/app-chat/conversations",
          bodyTemplate: {
            message: "{{prompt}}",
            modelName: "{{model}}",
          },
        },
        responseMode: "json",
      },
    },
    {
      providerId: "perplexity-web",
      label: "Perplexity Web",
      type: "web",
      enabled: true,
      authProfileId: "auth_perplexity_main",
      browserProfileId: "chrome_main",
      transportStrategy: {
        primary: "browser_dom",
        fallbacks: [],
        requiresCdp: true,
      },
      models: [textModel("perplexity-web", "Perplexity Web")],
      capabilities: {
        storyboard_generation: { enabled: true, models: ["perplexity-web"] },
        text_generation: { enabled: true, models: ["perplexity-web"] },
      },
      domDriver: {
        startUrl: "https://www.perplexity.ai/",
        inputSelectors: commonTextInputSelectors,
        sendActions: [{ type: "keyboard", key: "Enter" }],
        outputExtractors: commonDomOutput,
      },
    },
    {
      providerId: "xiaomimo-web",
      label: "Xiaomi MiMo Web",
      type: "web",
      enabled: true,
      authProfileId: "auth_xiaomimo_main",
      browserProfileId: "chrome_main",
      transportStrategy: {
        primary: "browser_dom",
        fallbacks: ["web_http_replay"],
        requiresCdp: true,
      },
      models: [textModel("xiaomimo-web", "Xiaomi MiMo Web")],
      capabilities: {
        storyboard_generation: { enabled: true, models: ["xiaomimo-web"] },
        text_generation: { enabled: true, models: ["xiaomimo-web"] },
      },
      domDriver: {
        startUrl: "https://mimo.mi.com/",
        inputSelectors: commonTextInputSelectors,
        sendActions: [{ type: "keyboard", key: "Enter" }],
        outputExtractors: commonDomOutput,
      },
      httpReplay: {
        baseUrl: "https://mimo.mi.com",
        endpoint: "/api/chat",
        method: "POST",
        authSources: ["cookie", "user_agent", "headers"],
        responseMode: "json",
      },
    },
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
