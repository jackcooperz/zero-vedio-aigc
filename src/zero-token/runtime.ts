import { BrowserSessionManager } from "./browser-session.js";
import { runDomDriver } from "./drivers/dom.js";
import { runEvalFetchDriver } from "./drivers/eval-fetch.js";
import { runHttpReplayDriver } from "./drivers/http-replay.js";
import { runNetworkCaptureDriver } from "./drivers/network-capture.js";
import { ZeroTokenError } from "./errors.js";
import { parseProviderRef, ProviderRegistry } from "./registry.js";
import type {
  AuthProfile,
  ZeroTokenRequest,
  ZeroTokenResult,
  ZeroTokenRuntimeConfig,
  ZeroTokenTransport,
} from "./types.js";

export class ZeroTokenRuntime {
  private readonly registry: ProviderRegistry;
  private readonly browser: BrowserSessionManager;
  private readonly authProfiles: AuthProfile[];

  constructor(config: ZeroTokenRuntimeConfig) {
    this.registry = new ProviderRegistry(config.providers);
    this.browser = new BrowserSessionManager(config.browserProfiles);
    this.authProfiles = config.authProfiles ?? [];
  }

  listProviders() {
    return this.registry.list();
  }

  async generate(request: ZeroTokenRequest): Promise<ZeroTokenResult> {
    const ref = parseProviderRef(request.providerRef);
    const provider = this.registry.resolve(ref.provider);
    this.registry.assertModel(provider, ref.model, request.capability);
    const auth = provider.authProfileId
      ? this.authProfiles.find((profile) => profile.authProfileId === provider.authProfileId)
      : undefined;

    const transports = this.resolveTransports(provider.transportStrategy.primary, provider.transportStrategy.fallbacks, request.transportPreference);
    let lastError: unknown;
    for (const transport of transports) {
      try {
        if (transport === "browser_dom") {
          return await runDomDriver({ provider, request, browser: this.browser, auth, transport });
        }
        if (transport === "browser_eval_fetch") {
          return await runEvalFetchDriver({ provider, request, browser: this.browser, auth, transport });
        }
        if (transport === "browser_network_capture") {
          return await runNetworkCaptureDriver({ provider, request, browser: this.browser, auth, transport });
        }
        if (transport === "web_http_replay") {
          return await runHttpReplayDriver({ provider, request, auth, transport });
        }
        if (transport === "hybrid") {
          continue;
        }
      } catch (error) {
        lastError = error;
        if (error instanceof ZeroTokenError && !error.retryable) {
          throw error;
        }
      }
    }
    if (lastError instanceof Error) {
      throw lastError;
    }
    throw new ZeroTokenError("CAPABILITY_NOT_SUPPORTED", "No usable transport for request");
  }

  private resolveTransports(
    primary: ZeroTokenTransport,
    fallbacks: ZeroTokenTransport[] = [],
    preference?: ZeroTokenTransport[],
  ): Array<Exclude<ZeroTokenTransport, "hybrid">> {
    const isConcreteTransport = (
      item: ZeroTokenTransport,
    ): item is Exclude<ZeroTokenTransport, "hybrid"> => item !== "hybrid";
    const configured = [primary, ...fallbacks].filter(isConcreteTransport);
    if (!preference?.length) {
      return configured;
    }
    return preference.filter(isConcreteTransport).filter((item) => configured.includes(item));
  }
}
