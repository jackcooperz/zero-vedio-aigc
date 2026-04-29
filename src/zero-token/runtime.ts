import { BrowserSessionManager } from "./browser-session.js";
import { isConcreteTransport, runTransportDriver, type ConcreteZeroTokenTransport } from "./drivers/registry.js";
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

  async shutdown(): Promise<void> {
    await this.browser.shutdown();
  }

  async generate(request: ZeroTokenRequest): Promise<ZeroTokenResult> {
    const ref = parseProviderRef(request.providerRef);
    const provider = this.registry.resolve(ref.provider);
    const auth = provider.authProfileId
      ? this.authProfiles.find((profile) => profile.authProfileId === provider.authProfileId)
      : undefined;

    const transports = this.resolveTransports(provider.transportStrategy.primary, provider.transportStrategy.fallbacks, request.transportPreference);
    let lastError: unknown;
    for (const transport of transports) {
      try {
        return await runTransportDriver({ provider, request, browser: this.browser, auth, transport });
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
  ): ConcreteZeroTokenTransport[] {
    const configured = [primary, ...fallbacks].filter(isConcreteTransport);
    if (!preference?.length) {
      return configured;
    }
    return preference.filter(isConcreteTransport).filter((item) => configured.includes(item));
  }
}
