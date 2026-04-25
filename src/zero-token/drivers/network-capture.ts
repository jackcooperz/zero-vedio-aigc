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

function resolveProviderResponseMode(provider: WebProviderConfig, responseUrl: string) {
  const map = provider.responseMode;
  if (!map) {
    return undefined;
  }
  for (const [key, mode] of Object.entries(map)) {
    if (responseUrl.includes(key)) {
      return mode;
    }
  }
  return undefined;
}

function safeParseJson(raw: string): unknown {
  try {
    return JSON.parse(raw);
  } catch {
    return undefined;
  }
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
  const listeners = cfg.listen;
  if (!listeners || listeners.length === 0) {
    throw new ZeroTokenError("NETWORK_RESPONSE_TIMEOUT", "networkCapture.listen is empty");
  }

  const session = await params.browser.connect(
    params.request.runtimeOptions?.browserProfileId ?? params.provider.browserProfileId,
    getNetworkCaptureStartUrl(params.provider, cfg.trigger),
    params.provider.domDriver?.pageUrlPatterns,
  );
  const sampler = createObservedRequestSampler(params.provider);

  const output = { text: undefined, json: undefined, images: [], videos: [] } as ZeroTokenResult["output"];
  const seenImages = new Set<string>();
  const seenVideos = new Set<string>();

  const mergeOutput = (partial: typeof output) => {
    if (typeof partial.text === "string" && partial.text.length > 0) {
      output.text = partial.text;
    }
    if (partial.json !== undefined) {
      output.json = partial.json;
    }
    if (Array.isArray(partial.images)) {
      for (const image of partial.images) {
        const url = image.url ?? "";
        if (!url || seenImages.has(url)) {
          continue;
        }
        seenImages.add(url);
        output.images?.push(image);
      }
    }
    if (Array.isArray(partial.videos)) {
      for (const video of partial.videos) {
        const url = video.url ?? "";
        if (!url || seenVideos.has(url)) {
          continue;
        }
        seenVideos.add(url);
        output.videos?.push(video);
      }
    }
  };

  const isDone = () => {
    if (params.request.capability === "video") {
      return Array.isArray(output.videos) && output.videos.length > 0;
    }
    return (
      (typeof output.text === "string" && output.text.length > 0) ||
      (Array.isArray(output.images) && output.images.length > 0)
    );
  };

  let resolveCapture: (result: ZeroTokenResult) => void;
  let rejectCapture: (error: Error) => void;
  const capturePromise = new Promise<ZeroTokenResult>((res, rej) => {
    resolveCapture = res;
    rejectCapture = rej;
  });

  const computedTimeoutMs = params.request.runtimeOptions?.timeoutMs ?? Math.max(...listeners.map((l) => l.timeoutMs));
  const maxTimeoutMs = params.request.capability === "video"
    ? Math.max(computedTimeoutMs, 300000)
    : computedTimeoutMs;
  const timeoutTimer = setTimeout(() => {
    rejectCapture(new ZeroTokenError("NETWORK_RESPONSE_TIMEOUT", `Timed out waiting for network responses after ${maxTimeoutMs}ms`));
  }, maxTimeoutMs);

  const onResponse = async (response: any) => {
    const matchedListener = listeners.find((l) => matchesNetworkResponse(l, response));
    if (!matchedListener) {
      return;
    }
    try {
      const raw = await response.text();
      const responseUrl = response.url();
      const headerContentType = response.headers()["content-type"] ?? "";
      const inferredMode = headerContentType.includes("text/event-stream") ? "sse" : "text";
      const responseMode = resolveProviderResponseMode(params.provider, responseUrl) ?? inferredMode;
      const json = responseMode === "json" ? safeParseJson(raw) : undefined;

      const partial = parseProviderResponse({
        parser: cfg.parser,
        raw,
        json,
        responseMode,
        imageSource: "network_capture",
      });
      mergeOutput(partial as typeof output);

      if (isDone()) {
        clearTimeout(timeoutTimer);
        resolveCapture({
          requestId: params.request.requestId,
          providerRef: params.request.providerRef,
          transportUsed: params.transport,
          status: "success",
          output,
          debug: { pageUrl: session.page.url(), rawLength: raw.length },
          usage: { billingMode: "web", apiTokens: 0, estimatedCost: 0 },
        });
      }
    } catch (error) {
    }
  };

  try {
    session.page.on("request", sampler.onRequest);
    session.page.on("response", onResponse);

    await runNetworkCaptureTrigger(cfg.trigger, { provider: params.provider, request: params.request, session, auth: params.auth });

    return await capturePromise;
  } catch (error) {
    if (error instanceof ZeroTokenError) {
      throw error;
    }
    throw new ZeroTokenError("NETWORK_RESPONSE_TIMEOUT", "Failed to capture provider response", {
      retryable: true,
      details: buildNetworkCaptureErrorDetails({
        error,
        listener: listeners[0],
        pageUrl: session.page.url(),
        observedRequests: sampler.observedRequests,
      }),
    });
  } finally {
    clearTimeout(timeoutTimer);
    session.page.off("request", sampler.onRequest);
    session.page.off("response", onResponse);
    await session.close();
  }
}
