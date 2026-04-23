import type { BrowserSessionManager } from "../browser-session.js";
import type { AuthProfile, NetworkCaptureConfig, ZeroTokenRequest, WebProviderConfig } from "../types.js";
import { sendDomPrompt } from "./dom.js";

type BrowserSession = Awaited<ReturnType<BrowserSessionManager["connect"]>>;
type NetworkCaptureTrigger = NetworkCaptureConfig["trigger"];

export type NetworkCaptureTriggerContext = {
  provider: WebProviderConfig;
  request: ZeroTokenRequest;
  session: BrowserSession;
  auth?: AuthProfile;
};

type NetworkCaptureTriggerHandler = (context: NetworkCaptureTriggerContext) => Promise<void>;
type NetworkCaptureStartUrlResolver = (provider: WebProviderConfig) => string | undefined;

const NETWORK_CAPTURE_TRIGGERS = {
  dom: async (context) => {
    await sendDomPrompt(context);
  },
} satisfies Record<NetworkCaptureTrigger, NetworkCaptureTriggerHandler>;

const NETWORK_CAPTURE_START_URLS = {
  dom: (provider) => provider.domDriver?.startUrl,
} satisfies Record<NetworkCaptureTrigger, NetworkCaptureStartUrlResolver>;

export function getNetworkCaptureStartUrl(provider: WebProviderConfig, trigger: NetworkCaptureTrigger): string | undefined {
  return NETWORK_CAPTURE_START_URLS[trigger](provider);
}

export async function runNetworkCaptureTrigger(
  trigger: NetworkCaptureTrigger,
  context: NetworkCaptureTriggerContext,
): Promise<void> {
  await NETWORK_CAPTURE_TRIGGERS[trigger](context);
}
