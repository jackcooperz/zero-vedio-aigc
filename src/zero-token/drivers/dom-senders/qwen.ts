import { ZeroTokenError } from "../../errors.js";
import type { DomSendPromptContext } from "../dom-senders.js";
import { getPromptFromInput } from "../../utils.js";
import { pasteInputImages } from "../dom-attachments.js";

function isElementDisabled(value: unknown): boolean {
  return value === true || value === "true";
}

export async function sendQwenDomPrompt(context: DomSendPromptContext): Promise<void> {
  const { session, provider, request } = context;
  const inputSelector = provider.domDriver.inputSelectors[0] ?? "textarea.message-input-textarea";
  const input = session.page.locator(inputSelector).first();
  const inputCount = await input.count().catch(() => 0);
  if (inputCount === 0) {
    throw new ZeroTokenError("DOM_INPUT_NOT_FOUND", "Could not find Qwen input box", {
      retryable: true,
    });
  }

  await input.click({ timeout: 5000 });
  await pasteInputImages(session.page, request);
  await session.page.keyboard.type(getPromptFromInput(request.input), { delay: 15 });

  const sendButton = session.page.locator("button.send-button").first();
  for (let attempt = 0; attempt < 4; attempt += 1) {
    const buttonCount = await sendButton.count().catch(() => 0);
    if (buttonCount > 0) {
      const disabled = await sendButton.evaluate((element: HTMLButtonElement) => {
        return element.disabled || element.getAttribute("aria-disabled");
      }).catch(() => true);
      if (!isElementDisabled(disabled)) {
        const clicked = await sendButton.evaluate((element: HTMLButtonElement) => {
          element.click();
          return true;
        }).catch(() => false);
        if (clicked) {
          return;
        }
      }
    }
    await session.page.waitForTimeout(250);
  }

  throw new ZeroTokenError("DOM_SEND_FAILED", "Qwen send button did not become clickable", {
    retryable: true,
  });
}
