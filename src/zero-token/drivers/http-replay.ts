import { ZeroTokenError } from "../errors.js";
import type { AuthProfile, ZeroTokenRequest, ZeroTokenResult, ZeroTokenTransport, WebProviderConfig } from "../types.js";
import { parseProviderResponse } from "../providers/parsers.js";
import { getPromptFromInput, interpolateTemplate } from "../utils.js";

export async function runHttpReplayDriver(params: {
  provider: WebProviderConfig;
  request: ZeroTokenRequest;
  auth?: AuthProfile;
  transport: ZeroTokenTransport;
}): Promise<ZeroTokenResult> {
  const cfg = params.provider.httpReplay;
  if (!cfg) {
    throw new ZeroTokenError("WEB_HTTP_REPLAY_FAILED", `${params.provider.providerId} missing httpReplay`);
  }
  const url = new URL(cfg.endpoint, cfg.baseUrl);
  const vars: Record<string, unknown> = {
    prompt: getPromptFromInput(params.request.input),
    model: params.request.providerRef.split("/").slice(1).join("/"),
    ...params.auth?.values,
    ...params.request.input,
  };
  for (const key of cfg.dynamicParams ?? []) {
    const value = vars[key];
    if (typeof value === "string") {
      url.searchParams.set(key, value);
    }
  }

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...params.auth?.headers,
  };
  if (params.auth?.cookie) {
    headers.Cookie = params.auth.cookie;
  }
  if (params.auth?.userAgent) {
    headers["User-Agent"] = params.auth.userAgent;
  }

  const body = cfg.method === "POST" ? JSON.stringify(interpolateTemplate(params.request.input.body ?? {}, vars)) : undefined;
  const res = await fetch(url, { method: cfg.method, headers, body });
  const raw = await res.text();
  if (!res.ok) {
    throw new ZeroTokenError("WEB_HTTP_REPLAY_FAILED", `HTTP replay failed with ${res.status}`, {
      retryable: true,
      details: raw,
    });
  }

  let json: unknown;
  if (cfg.responseMode === "json") {
    json = JSON.parse(raw);
  }
  const output = parseProviderResponse({
    parser: cfg.parser,
    raw,
    json,
    responseMode: cfg.responseMode,
    imageSource: "http_replay",
  });

  return {
    requestId: params.request.requestId,
    providerRef: params.request.providerRef,
    transportUsed: params.transport,
    status: "success",
    output: {
      ...output,
      json,
    },
    debug: { url: url.toString(), rawLength: raw.length },
    usage: { billingMode: "web", apiTokens: 0, estimatedCost: 0 },
  };
}
