import { ZeroTokenError } from "../errors.js";
import type { BrowserSessionManager } from "../browser-session.js";
import type { AuthProfile, ZeroTokenRequest, ZeroTokenResult, ZeroTokenTransport, WebProviderConfig } from "../types.js";
import { getPromptFromInput, interpolateTemplate } from "../utils.js";

export async function runEvalFetchDriver(params: {
  provider: WebProviderConfig;
  request: ZeroTokenRequest;
  browser: BrowserSessionManager;
  auth?: AuthProfile;
  transport: ZeroTokenTransport;
}): Promise<ZeroTokenResult> {
  const cfg = params.provider.evalFetch;
  if (!cfg) {
    throw new ZeroTokenError("EVAL_FETCH_FAILED", `${params.provider.providerId} missing evalFetch`);
  }
  const session = await params.browser.connect(
    params.request.runtimeOptions?.browserProfileId ?? params.provider.browserProfileId,
    cfg.startUrl,
  );
  try {
    const vars: Record<string, unknown> = {
      prompt: getPromptFromInput(params.request.input),
      model: params.request.providerRef.split("/").slice(1).join("/"),
      ...params.request.input,
    };
    for (const step of cfg.bootstrapSteps ?? []) {
      const result = await session.page.evaluate(
        async (params: { step: { name: string; method: "GET" | "POST"; url: string; body?: unknown } }) => {
          const { step } = params;
          const res = await fetch(step.url, {
            method: step.method,
            headers: { "Content-Type": "application/json" },
            body: step.method === "POST" ? JSON.stringify(step.body ?? {}) : undefined,
            credentials: "include",
          });
          const text = await res.text();
          return { ok: res.ok, status: res.status, text };
        },
        { step: interpolateTemplate(step, vars) },
      );
      if (!result.ok) {
        throw new ZeroTokenError("EVAL_FETCH_FAILED", `${step.name} failed with ${result.status}`, {
          retryable: true,
          details: result.text,
        });
      }
      try {
        const data = JSON.parse(result.text);
        vars[`${step.name}_result`] = data;
        vars.chat_id = data.data?.id ?? data.chat_id ?? data.id ?? vars.chat_id;
      } catch {
        vars[`${step.name}_result`] = result.text;
      }
    }

    const send = interpolateTemplate(cfg.sendMessage, vars);
    const response = await session.page.evaluate(
      async (params: {
        send: {
          method: "GET" | "POST";
          urlTemplate: string;
          bodyTemplate?: unknown;
        };
      }) => {
        const { send } = params;
        const res = await fetch(send.urlTemplate, {
          method: send.method,
          headers: { "Content-Type": "application/json" },
          body: send.method === "POST" ? JSON.stringify(send.bodyTemplate ?? {}) : undefined,
          credentials: "include",
        });
        return {
          ok: res.ok,
          status: res.status,
          contentType: res.headers.get("content-type") ?? "",
          text: await res.text(),
        };
      },
      { send },
    );
    if (!response.ok) {
      throw new ZeroTokenError("EVAL_FETCH_FAILED", `Provider request failed with ${response.status}`, {
        retryable: true,
        details: response.text,
      });
    }

    let output: { text?: string; json?: unknown } = { text: response.text };
    if (response.contentType.includes("json")) {
      try {
        output = { json: JSON.parse(response.text), text: response.text };
      } catch {
        output = { text: response.text };
      }
    }

    return {
      requestId: params.request.requestId,
      providerRef: params.request.providerRef,
      transportUsed: params.transport,
      status: "success",
      output,
      debug: { pageUrl: session.page.url() },
      usage: { billingMode: "web", apiTokens: 0, estimatedCost: 0 },
    };
  } finally {
    await session.close();
  }
}
