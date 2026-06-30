import { ZeroTokenError } from "./errors.js";
import type { ProviderRef, ZeroTokenProviderConfig } from "./types.js";

export function parseProviderRef(raw: string): ProviderRef {
  const trimmed = raw.trim();
  if (!trimmed) {
    throw new ZeroTokenError("PROVIDER_NOT_FOUND", "Invalid provider_ref: empty");
  }
  const slash = trimmed.indexOf("/");
  if (slash < 0) {
    return {
      provider: trimmed,
      model: "web",
    };
  }
  if (slash <= 0 || slash === trimmed.length - 1) {
    throw new ZeroTokenError("PROVIDER_NOT_FOUND", `Invalid provider_ref: ${raw}`);
  }
  return {
    provider: trimmed.slice(0, slash),
    model: trimmed.slice(slash + 1),
  };
}

export class ProviderRegistry {
  private readonly providers = new Map<string, ZeroTokenProviderConfig>();
  private readonly aliases = new Map<string, ZeroTokenProviderConfig>();

  constructor(providers: ZeroTokenProviderConfig[]) {
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

  list(): ZeroTokenProviderConfig[] {
    return [...this.providers.values()];
  }

  resolve(providerId: string): ZeroTokenProviderConfig {
    const provider = this.aliases.get(providerId);
    if (!provider) {
      throw new ZeroTokenError("PROVIDER_NOT_FOUND", `Provider not found: ${providerId}`);
    }
    return provider;
  }
}
