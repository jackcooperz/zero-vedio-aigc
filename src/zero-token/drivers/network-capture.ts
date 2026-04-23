import { ZeroTokenError } from "../errors.js";
import type { BrowserSessionManager } from "../browser-session.js";
import type { AuthProfile, ZeroTokenRequest, ZeroTokenResult, ZeroTokenTransport, WebProviderConfig } from "../types.js";
import { parseProviderResponse } from "../providers/parsers.js";
import { getNetworkCaptureStartUrl, runNetworkCaptureTrigger } from "./network-triggers.js";

type ObservedRequest = {
  method: string;
  url: string;
};

function createObservedRequestSampler(provider: WebProviderConfig) {
  const observedRequests: ObservedRequest[] = [];
  const host = new URL(provider.domDriver.startUrl).hostname;

  return {
    observedRequests,
    onRequest(request: { url: () => string; method: () => string }) {
      const url = request.url();
      if (!url.includes(host)) {
        return;
      }
      observedRequests.push({ method: request.method(), url });
      if (observedRequests.length > 20) {
        observedRequests.shift();
      }
    },
  };
}

function matchesNetworkListener(
  listener: NonNullable<WebProviderConfig["networkCapture"]>["listen"][number],
  request: { method: string; url: string },
): boolean {
  return (!listener.method || request.method === listener.method) && request.url.includes(listener.urlContains);
}

function matchesNetworkResponse(
  listener: NonNullable<WebProviderConfig["networkCapture"]>["listen"][number],
  response: any,
): boolean {
  const request = response.request();
  const contentType = response.headers()["content-type"] ?? "";
  return (
    (!listener.method || request.method() === listener.method) &&
    response.url().includes(listener.urlContains) &&
    (!listener.contentTypeContains || contentType.includes(listener.contentTypeContains))
  );
}

function buildNetworkCaptureErrorDetails(params: {
  error: unknown;
  listener: NonNullable<WebProviderConfig["networkCapture"]>["listen"][number];
  pageUrl: string;
  observedRequests: ObservedRequest[];
}) {
  return {
    cause: params.error instanceof Error
      ? { name: params.error.name, message: params.error.message, stack: params.error.stack }
      : params.error,
    listener: params.listener,
    pageUrl: params.pageUrl,
    matchingRequestCount: params.observedRequests.filter((request) => matchesNetworkListener(params.listener, request)).length,
    recentRequests: params.observedRequests,
  };
}

export async function runNetworkCaptureDriver(params: {
  provider: WebProviderConfig;
  request: ZeroTokenRequest;
  browser: BrowserSessionManager;
  auth?: AuthProfile;
  transport: ZeroTokenTransport;
}): Promise<ZeroTokenResult> {
  const cfg = params.provider.networkCapture;
  if (!cfg) {
    throw new ZeroTokenError(
      "NETWORK_RESPONSE_TIMEOUT",
      `${params.provider.providerId} missing networkCapture`,
    );
  }
  const listener = cfg.listen[0];
  if (!listener) {
    throw new ZeroTokenError("NETWORK_RESPONSE_TIMEOUT", "networkCapture.listen is empty");
  }

  const session = await params.browser.connect(
    params.request.runtimeOptions?.browserProfileId ?? params.provider.browserProfileId,
    getNetworkCaptureStartUrl(params.provider, cfg.trigger),
    params.provider.domDriver?.pageUrlPatterns,
  );
  const sampler = createObservedRequestSampler(params.provider);
  let responsePromise: Promise<any> | undefined;
  try {
    session.page.on("request", sampler.onRequest);
    const pendingResponse = session.page.waitForResponse(
      (response: any) => matchesNetworkResponse(listener, response),
      { timeout: listener.timeoutMs },
    );
    pendingResponse.catch(() => undefined);
    responsePromise = pendingResponse;

    await runNetworkCaptureTrigger(cfg.trigger, { provider: params.provider, request: params.request, session, auth: params.auth });
    if (!responsePromise) {
      throw new ZeroTokenError("NETWORK_RESPONSE_TIMEOUT", "Network response listener was not initialized");
    }
    const response = await responsePromise;
    const raw = await response.text();
    const output = parseProviderResponse({
      parser: cfg.parser,
      raw,
      responseMode: "sse",
      imageSource: "network_capture",
    });
    return {
      requestId: params.request.requestId,
      providerRef: params.request.providerRef,
      transportUsed: params.transport,
      status: "success",
      output,
      debug: { pageUrl: session.page.url(), rawLength: raw.length },
      usage: { billingMode: "web", apiTokens: 0, estimatedCost: 0 },
    };
  } catch (error) {
    if (error instanceof ZeroTokenError) {
      throw error;
    }
    throw new ZeroTokenError("NETWORK_RESPONSE_TIMEOUT", "Failed to capture provider response", {
      retryable: true,
      details: buildNetworkCaptureErrorDetails({
        error,
        listener,
        pageUrl: session.page.url(),
        observedRequests: sampler.observedRequests,
      }),
    });
  } finally {
    session.page.off("request", sampler.onRequest);
    if (responsePromise) {
      await responsePromise.catch(() => undefined);
    }
    await session.close();
  }
}
