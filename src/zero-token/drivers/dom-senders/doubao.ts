import { ZeroTokenError } from "../../errors.js";
import { getPromptFromInput } from "../../utils.js";
import { pasteInputImages } from "../dom-attachments.js";
import type { DomSendPromptContext } from "../dom-senders.js";

function isDisabled(value: unknown): boolean {
  return value === true || value === "true";
}

function normalizePromptText(value: string): string {
  return value.replace(/\r\n/g, "\n");
}

async function readDoubaoInputValue(input: any): Promise<string> {
  return input.evaluate((element: HTMLTextAreaElement | HTMLElement) => {
    if (element instanceof HTMLTextAreaElement || element instanceof HTMLInputElement) {
      return element.value ?? "";
    }
    return element.textContent ?? "";
  }).catch(() => "");
}

async function verifyDoubaoPrompt(input: any, prompt: string): Promise<boolean> {
  const actual = normalizePromptText(await readDoubaoInputValue(input));
  return actual === normalizePromptText(prompt);
}

async function writeDoubaoPrompt(input: any, page: any, prompt: string): Promise<void> {
  const normalizedPrompt = normalizePromptText(prompt);

  await input.fill("").catch(() => {});
  await input.fill(normalizedPrompt).catch(() => {});
  if (await verifyDoubaoPrompt(input, normalizedPrompt)) {
    return;
  }

  await input.click({ timeout: 5000 });
  await page.keyboard.press("Control+A").catch(() => {});
  await page.keyboard.insertText(normalizedPrompt).catch(() => {});
  if (await verifyDoubaoPrompt(input, normalizedPrompt)) {
    return;
  }

  const wroteViaDom = await input.evaluate((element: HTMLTextAreaElement | HTMLElement, value: string) => {
    if (element instanceof HTMLTextAreaElement || element instanceof HTMLInputElement) {
      element.focus();
      element.value = value;
      element.dispatchEvent(new Event("input", { bubbles: true }));
      element.dispatchEvent(new Event("change", { bubbles: true }));
      return true;
    }
    if (element instanceof HTMLElement && element.isContentEditable) {
      element.focus();
      element.textContent = value;
      element.dispatchEvent(new InputEvent("input", { bubbles: true, data: value, inputType: "insertText" }));
      return true;
    }
    return false;
  }, normalizedPrompt).catch(() => false);

  if (!wroteViaDom || !(await verifyDoubaoPrompt(input, normalizedPrompt))) {
    throw new ZeroTokenError("DOM_SEND_FAILED", "Failed to write full prompt into Doubao input box", {
      retryable: true,
      details: {
        expectedLength: normalizedPrompt.length,
        actualLength: (await readDoubaoInputValue(input)).length,
      },
    });
  }
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

  const prompt = getPromptFromInput(request.input);
  await input.click({ timeout: 5000 });
  await pasteInputImages(session.page, request);
  await writeDoubaoPrompt(input, session.page, prompt);

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
