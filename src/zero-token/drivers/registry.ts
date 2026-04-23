import type { BrowserSessionManager } from "../browser-session.js";
import type {
  AuthProfile,
  WebProviderConfig,
  ZeroTokenRequest,
  ZeroTokenResult,
  ZeroTokenTransport,
} from "../types.js";
import { runNetworkCaptureDriver } from "./network-capture.js";

export type ConcreteZeroTokenTransport = ZeroTokenTransport;

export type TransportDriverContext = {
  provider: WebProviderConfig;
  request: ZeroTokenRequest;
  browser: BrowserSessionManager;
  auth?: AuthProfile;
  transport: ConcreteZeroTokenTransport;
};

export type TransportDriver = (context: TransportDriverContext) => Promise<ZeroTokenResult>;

const TRANSPORT_DRIVERS = {
  browser_network_capture: runNetworkCaptureDriver,
} satisfies Record<ConcreteZeroTokenTransport, TransportDriver>;

export function isConcreteTransport(transport: ZeroTokenTransport): transport is ConcreteZeroTokenTransport {
  return transport === "browser_network_capture";
}

export async function runTransportDriver(context: TransportDriverContext): Promise<ZeroTokenResult> {
  return TRANSPORT_DRIVERS[context.transport](context);
}
