import { ZeroTokenError } from "../errors.js";
import type { ZeroTokenRequest } from "../types.js";

type PlaywrightPage = any;

export async function pasteInputImages(page: PlaywrightPage, request: ZeroTokenRequest): Promise<void> {
  const images = request.input.images ?? [];
  if (!images.length) {
    return;
  }

  const pasteResult = await page.evaluate(async (payload: typeof images) => {
    const target = document.activeElement as HTMLElement | null;
    if (!target) {
      return {
        hasTarget: false,
        dispatched: false,
      };
    }

    const dataTransfer = new DataTransfer();
    for (const image of payload) {
      const binary = atob(image.dataBase64);
      const bytes = new Uint8Array(binary.length);
      for (let index = 0; index < binary.length; index += 1) {
        bytes[index] = binary.charCodeAt(index);
      }
      const file = new File([bytes], image.name ?? `image-${Date.now()}.png`, {
        type: image.mimeType,
      });
      dataTransfer.items.add(file);
    }

    const pasteEvent = new Event("paste", { bubbles: true, cancelable: true, composed: true });
    Object.defineProperty(pasteEvent, "clipboardData", {
      configurable: true,
      enumerable: true,
      value: dataTransfer,
    });

    target.dispatchEvent(pasteEvent);
    return {
      hasTarget: true,
      dispatched: true,
    };
  }, images);

  if (!pasteResult.hasTarget || !pasteResult.dispatched) {
    throw new ZeroTokenError("DOM_SEND_FAILED", "Failed to paste uploaded images into provider input", {
      retryable: true,
    });
  }

  await page.waitForTimeout(600);
}
