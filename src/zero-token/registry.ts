import { ZeroTokenError } from "./errors.js";
import type { ProviderRef, WebProviderConfig, ZeroTokenCapability } from "./types.js";

export function parseProviderRef(raw: string): ProviderRef {
  const trimmed = raw.trim();
  const slash = trimmed.indexOf("/");
  if (slash <= 0 || slash === trimmed.length - 1) {
    throw new ZeroTokenError("PROVIDER_NOT_FOUND", `Invalid provider_ref: ${raw}`);
  }
  return {
    provider: trimmed.slice(0, slash),
    model: trimmed.slice(slash + 1),
  };
}

export class ProviderRegistry {
  private readonly providers = new Map<string, WebProviderConfig>();
  private readonly aliases = new Map<string, WebProviderConfig>();

  constructor(providers: WebProviderConfig[]) {
    for (const provider of providers) {
      if (!provider.enabled) {
        continue;
      }
      this.providers.set(provider.providerId, provider);
      this.aliases.set(provider.providerId, provider);
      for (const alias of provider.aliases ?? []) {
        this.aliases.set(alias, provider);
      }
    }
  }

  list(): WebProviderConfig[] {
    return [...this.providers.values()];
  }

  resolve(providerId: string): WebProviderConfig {
    const provider = this.aliases.get(providerId);
    if (!provider) {
      throw new ZeroTokenError("PROVIDER_NOT_FOUND", `Provider not found: ${providerId}`);
    }
    return provider;
  }

  assertModel(provider: WebProviderConfig, model: string, capability: ZeroTokenCapability): void {
    const capabilityConfig = provider.capabilities[capability];
    if (!capabilityConfig?.enabled) {
      throw new ZeroTokenError(
        "CAPABILITY_NOT_SUPPORTED",
        `${provider.providerId} does not support ${capability}`,
      );
    }
    if (!capabilityConfig.models.includes(model)) {
      throw new ZeroTokenError(
        "MODEL_NOT_SUPPORTED",
        `${provider.providerId} does not support model ${model} for ${capability}`,
      );
    }
  }
}

