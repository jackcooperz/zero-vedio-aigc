export type ZeroTokenCapability = "storyboard_generation" | "image_generation" | "text_generation";

export type ZeroTokenTransport =
  | "browser_dom"
  | "browser_eval_fetch"
  | "browser_network_capture"
  | "web_http_replay"
  | "hybrid";

export type ProviderResponseParser = "generic" | "doubao-web-sse";

export type ProviderRef = {
  provider: string;
  model: string;
};

export type BrowserProfile = {
  profileId: string;
  mode: "attach_only" | "managed";
  cdpUrl?: string;
  cdpPort?: number;
  userDataDir?: string;
  headless?: boolean;
  defaultTimeoutMs?: number;
};

export type AuthProfile = {
  authProfileId: string;
  providerId: string;
  authType: "api_key" | "browser_profile" | "cookie" | "oauth" | "local_none";
  cookie?: string;
  userAgent?: string;
  headers?: Record<string, string>;
  values?: Record<string, string>;
};

export type TransportStrategy = {
  primary: ZeroTokenTransport;
  fallbacks?: ZeroTokenTransport[];
  requiresCdp?: boolean;
  requiresVisibleBrowser?: boolean;
};

export type ProviderModel = {
  id: string;
  label?: string;
  capabilities: ZeroTokenCapability[];
};

export type ProviderCapabilityConfig = {
  enabled: boolean;
  models: string[];
  maxCount?: number;
  supportedAspectRatios?: string[];
  supportedResolutions?: string[];
};

export type DomAction =
  | { type: "click_if_exists"; selector: string; timeoutMs?: number }
  | { type: "click"; selector: string; timeoutMs?: number }
  | { type: "keyboard"; key: string }
  | { type: "wait"; timeoutMs: number };

export type DomDriverConfig = {
  startUrl: string;
  pageUrlPatterns?: string[];
  inputSelectors: string[];
  preActions?: DomAction[];
  sendActions: DomAction[];
  waitPolicy?: {
    type: "stable_text";
    maxWaitMs: number;
    pollIntervalMs: number;
    stableRounds: number;
  };
  outputExtractors?: Array<{
    type: "main_text";
    selector?: string;
    excludeSelectors?: string[];
    minLength?: number;
  }>;
};

export type EvalFetchStep = {
  name: string;
  method: "GET" | "POST";
  url: string;
  body?: unknown;
};

export type EvalFetchConfig = {
  startUrl: string;
  bootstrapSteps?: EvalFetchStep[];
  sendMessage: {
    method: "GET" | "POST";
    urlTemplate: string;
    bodyTemplate?: unknown;
  };
  responseMode: "json" | "text" | "stream";
  outputPath?: string;
};

export type NetworkCaptureConfig = {
  trigger: "dom";
  parser?: ProviderResponseParser;
  listen: Array<{
    name: string;
    method?: string;
    urlContains: string;
    contentTypeContains?: string;
    timeoutMs: number;
  }>;
  parsers: Array<{
    type: "sse" | "doubao_creation_block" | "text";
    events?: string[];
    extract?: "image_urls" | "text";
  }>;
};

export type HttpReplayConfig = {
  baseUrl: string;
  endpoint: string;
  method: "GET" | "POST";
  authSources?: Array<"cookie" | "user_agent" | "headers" | "query_params">;
  dynamicParams?: string[];
  responseMode: "json" | "text" | "sse";
  parser?: ProviderResponseParser;
};

export type WebProviderConfig = {
  providerId: string;
  label: string;
  type: "web";
  enabled: boolean;
  aliases?: string[];
  authProfileId?: string;
  browserProfileId?: string;
  transportStrategy: TransportStrategy;
  models: ProviderModel[];
  capabilities: Partial<Record<ZeroTokenCapability, ProviderCapabilityConfig>>;
  domDriver?: DomDriverConfig;
  evalFetch?: EvalFetchConfig;
  networkCapture?: NetworkCaptureConfig;
  httpReplay?: HttpReplayConfig;
};

export type ZeroTokenRequest = {
  requestId: string;
  projectId?: string;
  sceneId?: string;
  providerRef: string;
  capability: ZeroTokenCapability;
  transportPreference?: ZeroTokenTransport[];
  input: {
    prompt?: string;
    messages?: Array<{ role: "system" | "user" | "assistant"; content: string }>;
    aspectRatio?: string;
    resolution?: string;
    count?: number;
    schema?: unknown;
    [key: string]: unknown;
  };
  runtimeOptions?: {
    browserProfileId?: string;
    timeoutMs?: number;
    retryLimit?: number;
    saveDebugArtifacts?: boolean;
  };
};

export type GeneratedImage = {
  url?: string;
  localPath?: string;
  buffer?: Uint8Array;
  width?: number;
  height?: number;
  mimeType?: string;
  metadata?: Record<string, unknown>;
};

export type ZeroTokenResult = {
  requestId: string;
  providerRef: string;
  transportUsed: ZeroTokenTransport;
  status: "success";
  output: {
    text?: string;
    json?: unknown;
    images?: GeneratedImage[];
  };
  debug?: Record<string, unknown>;
  usage: {
    billingMode: "web";
    apiTokens: 0;
    estimatedCost: 0;
  };
};

export type ZeroTokenRuntimeConfig = {
  providers: WebProviderConfig[];
  browserProfiles?: BrowserProfile[];
  authProfiles?: AuthProfile[];
};
