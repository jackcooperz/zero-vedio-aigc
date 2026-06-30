import type { BrowserSessionManager } from "../browser-session.js";
import type {
  AuthProfile,
  ZeroTokenProviderConfig,
  ZeroTokenRequest,
  ZeroTokenResult,
  ZeroTokenTransport,
} from "../types.js";
import { ZeroTokenError } from "../errors.js";
import { runCodexCliDriver } from "./codex-cli.js";
import { runNetworkCaptureDriver } from "./network-capture.js";

export type ConcreteZeroTokenTransport = ZeroTokenTransport;

export type TransportDriverContext = {
  provider: ZeroTokenProviderConfig;
  request: ZeroTokenRequest;
  browser: BrowserSessionManager;
  auth?: AuthProfile;
  transport: ConcreteZeroTokenTransport;
};

export type TransportDriver = (context: TransportDriverContext) => Promise<ZeroTokenResult>;

export function isConcreteTransport(transport: ZeroTokenTransport): transport is ConcreteZeroTokenTransport {
  return transport === "browser_network_capture" || transport === "codex_cli";
}

export async function runTransportDriver(context: TransportDriverContext): Promise<ZeroTokenResult> {
  if (context.transport === "browser_network_capture") {
    if (context.provider.type !== "web") {
      throw new ZeroTokenError("CAPABILITY_NOT_SUPPORTED", `${context.provider.providerId} is not a web provider`);
    }
    return runNetworkCaptureDriver({
      provider: context.provider,
      request: context.request,
      browser: context.browser,
      auth: context.auth,
      transport: context.transport,
    });
  }
  if (context.transport === "codex_cli") {
    return runCodexCliDriver(context);
  }
  throw new ZeroTokenError("CAPABILITY_NOT_SUPPORTED", `Unsupported transport: ${context.transport}`);
}
