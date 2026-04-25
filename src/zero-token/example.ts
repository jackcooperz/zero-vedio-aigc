import {
  ZERO_TOKEN_CAPABILITIES,
  ZeroTokenRuntime,
  buildDefaultBrowserProfiles,
  buildDefaultZeroTokenProviders,
} from "./index.js";

export async function createDefaultZeroTokenRuntime() {
  return new ZeroTokenRuntime({
    providers: buildDefaultZeroTokenProviders(),
    browserProfiles: buildDefaultBrowserProfiles(),
    authProfiles: [],
  });
}

export async function generateStoryboardWithProvider(providerRef: string, prompt: string) {
  const runtime = await createDefaultZeroTokenRuntime();
  return runtime.generate({
    requestId: `storyboard_${Date.now()}`,
    providerRef,
    capability: ZERO_TOKEN_CAPABILITIES.TEXT_IMAGE,
    input: { prompt },
  });
}

export async function generateImageWithProvider(providerRef: string, prompt: string) {
  const runtime = await createDefaultZeroTokenRuntime();
  return runtime.generate({
    requestId: `image_${Date.now()}`,
    providerRef,
    capability: ZERO_TOKEN_CAPABILITIES.TEXT_IMAGE,
    input: {
      prompt,
      aspectRatio: "16:9",
      resolution: "2K",
      count: 1,
    },
  });
}
