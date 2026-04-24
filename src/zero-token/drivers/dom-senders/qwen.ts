import { ZeroTokenError } from "../../errors.js";
import type { DomSendPromptContext } from "../dom-senders.js";
import { getPromptFromInput } from "../../utils.js";
import { pasteInputImages } from "../dom-attachments.js";

function isElementDisabled(value: unknown): boolean {
  return value === true || value === "true";
}

async function waitForQwenAttachmentsReady(session: DomSendPromptContext["session"], expectedCount: number): Promise<void> {
  if (expectedCount <= 0) {
    await session.page.waitForTimeout(200);
    return;
  }

  let stableRounds = 0;
  let lastCount = -1;
  for (let attempt = 0; attempt < 16; attempt += 1) {
    const state = await session.page.evaluate(() => {
      const closeButtons = document.querySelectorAll("button.close-button").length;
      const sendButton = document.querySelector("button.send-button");
      return {
        closeButtons,
        sendButtonPresent: Boolean(sendButton),
      };
    }).catch(() => ({ closeButtons: 0, sendButtonPresent: false }));

    if (state.closeButtons >= expectedCount && state.sendButtonPresent) {
      if (state.closeButtons === lastCount) {
        stableRounds += 1;
      } else {
        stableRounds = 0;
        lastCount = state.closeButtons;
      }
      if (stableRounds >= 2) {
        await session.page.waitForTimeout(1200);
        return;
      }
    }

    await session.page.waitForTimeout(500);
  }

  await session.page.waitForTimeout(2000);
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
  await waitForQwenAttachmentsReady(session, request.input.images?.length ?? 0);
  await session.page.keyboard.type(getPromptFromInput(request.input), { delay: 15 });

  const sendButton = session.page.locator("button.send-button").last();
  for (let attempt = 0; attempt < 10; attempt += 1) {
    const buttonCount = await sendButton.count().catch(() => 0);
    if (buttonCount > 0) {
      const buttonState = await sendButton.evaluate((element: HTMLButtonElement) => {
        const rect = element.getBoundingClientRect();
        return {
          disabled: element.disabled || element.getAttribute("aria-disabled"),
          width: Math.round(rect.width),
          height: Math.round(rect.height),
        };
      }).catch(() => null);
      if (
        buttonState &&
        !isElementDisabled(buttonState.disabled) &&
        buttonState.width >= 24 &&
        buttonState.height >= 24
      ) {
        const clickedViaLocator = await sendButton.click({
          timeout: 1000,
          force: true,
        }).then(() => true).catch(() => false);
        if (clickedViaLocator) {
          return;
        }
        const clickedViaEvaluate = await sendButton.evaluate((element: HTMLButtonElement) => {
          element.click();
          return true;
        }).catch(() => false);
        if (clickedViaEvaluate) {
          return;
        }
      }
    }
    await session.page.waitForTimeout(350);
  }

  const sentViaEnter = await session.page.keyboard.press("Enter").then(() => true).catch(() => false);
  if (sentViaEnter) {
    return;
  }

  throw new ZeroTokenError("DOM_SEND_FAILED", "Qwen send button did not become clickable", { retryable: true });
}
