import { ZeroTokenError } from "../../errors.js";
import { getPromptFromInput } from "../../utils.js";
import type { DomSendPromptContext } from "../dom-senders.js";

function isDisabled(value: unknown): boolean {
  return value === true || value === "true";
}

export async function sendDoubaoDomPrompt(context: DomSendPromptContext): Promise<void> {
  const { session, request } = context;
  const input = session.page.locator("textarea.semi-input-textarea").first();
  const inputCount = await input.count().catch(() => 0);
  if (inputCount === 0) {
    throw new ZeroTokenError("DOM_INPUT_NOT_FOUND", "Could not find Doubao input box", {
      retryable: true,
    });
  }

  await input.click({ timeout: 5000 });
  await session.page.keyboard.type(getPromptFromInput(request.input), { delay: 15 });

  const sendButton = session.page.locator(
    "button[data-dbx-name='button'][data-disabled='false']",
  ).filter({
    hasNot: session.page.locator(":scope >> text=/^(快速|图像生成|视频生成|帮我写作|超能模式|PPT 生成|更多)$/"),
  }).last();

  for (let attempt = 0; attempt < 6; attempt += 1) {
    const buttonCount = await sendButton.count().catch(() => 0);
    if (buttonCount > 0) {
      const metadata = await sendButton.evaluate((element: HTMLButtonElement) => {
        const rect = element.getBoundingClientRect();
        return {
          disabled: element.disabled || element.getAttribute("aria-disabled") || element.getAttribute("data-disabled"),
          width: Math.round(rect.width),
          height: Math.round(rect.height),
          text: (element.textContent || "").trim(),
        };
      }).catch(() => null);
      if (
        metadata &&
        !isDisabled(metadata.disabled) &&
        metadata.width >= 28 &&
        metadata.width <= 48 &&
        metadata.height >= 28 &&
        metadata.height <= 48 &&
        !metadata.text
      ) {
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

  await session.page.keyboard.press("Enter").catch(() => {});
}
