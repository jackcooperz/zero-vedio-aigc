import { ZeroTokenError } from "../errors.js";
import type { BrowserSessionManager } from "../browser-session.js";
import type { AuthProfile, ZeroTokenRequest, ZeroTokenResult, ZeroTokenTransport, WebProviderConfig } from "../types.js";
import { parseProviderResponse } from "../providers/parsers.js";
import { getNetworkCaptureStartUrl, runNetworkCaptureTrigger } from "./network-triggers.js";

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
  );
  try {
    const responsePromise = session.page.waitForResponse(
      (response: any) => {
        const request = response.request();
        const contentType = response.headers()["content-type"] ?? "";
        return (
          (!listener.method || request.method() === listener.method) &&
          response.url().includes(listener.urlContains) &&
          (!listener.contentTypeContains || contentType.includes(listener.contentTypeContains))
        );
      },
      { timeout: listener.timeoutMs },
    );

    await runNetworkCaptureTrigger(cfg.trigger, { provider: params.provider, request: params.request, session, auth: params.auth });
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
      details: error,
    });
  } finally {
    await session.close();
  }
}
