import { ZeroTokenError } from "../../errors.js";
import { ZERO_TOKEN_CAPABILITIES } from "../../types.js";
import type { DomSendPromptContext } from "../dom-senders.js";
import { getPromptFromInput } from "../../utils.js";
import { pasteInputImages } from "../dom-attachments.js";

function isElementDisabled(value: unknown): boolean {
  return value === true || value === "true";
}

function normalizePromptText(value: string): string {
  return value.replace(/\r\n/g, "\n");
}

async function readQwenInputValue(input: any): Promise<string> {
  return input.evaluate((element: HTMLTextAreaElement | HTMLElement) => {
    if (element instanceof HTMLTextAreaElement || element instanceof HTMLInputElement) {
      return element.value ?? "";
    }
    return element.textContent ?? "";
  }).catch(() => "");
}

async function verifyQwenPrompt(input: any, prompt: string): Promise<boolean> {
  const actual = normalizePromptText(await readQwenInputValue(input));
  return actual === normalizePromptText(prompt);
}

async function writeQwenPrompt(input: any, page: any, prompt: string): Promise<void> {
  const normalizedPrompt = normalizePromptText(prompt);

  await input.fill("").catch(() => {});
  await input.fill(normalizedPrompt).catch(() => {});
  if (await verifyQwenPrompt(input, normalizedPrompt)) {
    return;
  }

  await input.click({ timeout: 5000 });
  await page.keyboard.press("Control+A").catch(() => {});
  await page.keyboard.insertText(normalizedPrompt).catch(() => {});
  if (await verifyQwenPrompt(input, normalizedPrompt)) {
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

  if (!wroteViaDom || !(await verifyQwenPrompt(input, normalizedPrompt))) {
    throw new ZeroTokenError("DOM_SEND_FAILED", "Failed to write full prompt into Qwen input box", {
      retryable: true,
      details: {
        expectedLength: normalizedPrompt.length,
        actualLength: (await readQwenInputValue(input)).length,
      },
    });
  }
}

function getQwenComposer(input: any) {
  return input.locator("xpath=ancestor::div[contains(@class,'message-input-wrapper')][1]");
}

async function isQwenVideoModeActive(composer: any): Promise<boolean> {
  const composerCount = await composer.count().catch(() => 0);
  if (composerCount === 0) {
    console.log("[QwenDebug] Composer not found");
    return false;
  }

  // 直接获取模式文本内容，这是最可靠的判断方式
  const modeText = await composer.locator(".mode-select-current-mode").textContent().catch(() => "");
  console.log(`[QwenDebug] Current mode text: "${modeText?.trim()}"`);
  
  const isActive = modeText.includes("视频") || modeText.includes("Video");
  return isActive;
}

function getQwenVideoModeChip(composer: any) {
  // 使用文本定位器找到包含“视频”字样的模式按钮
  return composer.locator(".mode-select-current-mode").filter({
    hasText: /视频|Video/,
  }).first();
}

async function ensureQwenCapabilityMode(context: DomSendPromptContext): Promise<void> {
  const { session, request, provider } = context;
  const wantsVideo = request.capability === ZERO_TOKEN_CAPABILITIES.VIDEO;
  const inputSelector = provider.domDriver.inputSelectors[0] ?? "textarea.message-input-textarea";
  const input = session.page.locator(inputSelector).first();
  const composer = getQwenComposer(input);
  
  console.log(`[QwenDebug] Checking mode for capability: ${request.capability}`);
  const videoModeActive = await isQwenVideoModeActive(composer);
  console.log(`[QwenDebug] videoModeActive: ${videoModeActive}`);

  if (wantsVideo && videoModeActive) {
    return;
  }

  if (!wantsVideo && videoModeActive) {
    const videoModeChip = getQwenVideoModeChip(composer);
    const chipCount = await videoModeChip.count().catch(() => 0);
    if (chipCount > 0) {
      const chipHtml = await videoModeChip.evaluate((el: HTMLElement) => el.outerHTML).catch(() => "could not get chip html");
      console.log(`[QwenDebug] videoModeChip HTML: ${chipHtml}`);

      // 1. 先悬停在整个 chip 上，使关闭按钮（x）显示出来
      await videoModeChip.hover({ force: true }).catch(() => {});
      await session.page.waitForTimeout(350);
      
      // 2. 定位并点击关闭按钮
      // 直接定位 chip 内部的关闭按钮 span
      const closeButton = videoModeChip.locator(".mode-select-current-mode-close").first();
      const closeExists = await closeButton.count().catch(() => 0);
      
      console.log(`[QwenDebug] closeButton count: ${closeExists}`);
      
      if (closeExists > 0) {
        console.log("[QwenDebug] Clicking close button");
        await closeButton.click({ timeout: 3000, force: true }).catch((e: any) => {
          console.log(`[QwenDebug] Click close button failed: ${e.message}`);
        });
      } else {
        // 如果类名匹配不到，尝试更宽泛的定位方式
        const fallbackClose = videoModeChip.locator("span[class*='close']").first();
        if (await fallbackClose.count() > 0) {
          console.log("[QwenDebug] Clicking fallback close button");
          await fallbackClose.click({ timeout: 3000, force: true }).catch(() => {});
        } else {
          console.log("[QwenDebug] No close button found, clicking chip as last resort");
          await videoModeChip.click({ timeout: 3000, force: true }).catch(() => {});
        }
      }
      
      await session.page.waitForTimeout(800);
      if (await isQwenVideoModeActive(composer)) {
        throw new ZeroTokenError("DOM_SEND_FAILED", "Could not exit Qwen video mode", {
          retryable: true,
        });
      }
    }
    return;
  }

  if (!wantsVideo) {
    return;
  }

  const modeOpenButton = composer.locator(".mode-select-open").first();
  const openCount = await modeOpenButton.count().catch(() => 0);
  if (openCount === 0) {
    throw new ZeroTokenError("DOM_SEND_FAILED", "Could not find Qwen mode selector", {
      retryable: true,
    });
  }

  for (let attempt = 0; attempt < 3; attempt += 1) {
    await modeOpenButton.click({ timeout: 3000, force: true }).catch(() => {});
    const createVideoItem = session.page.locator("li[role='menuitem'][data-menu-id$='-t2v']").first();
    const itemVisible = await createVideoItem.waitFor({ state: "visible", timeout: 2000 })
      .then(() => true)
      .catch(() => false);
    if (!itemVisible) {
      await session.page.waitForTimeout(300);
      continue;
    }
    const clicked = await createVideoItem.click({ timeout: 3000, force: true })
      .then(() => true)
      .catch(() => false);
    if (!clicked) {
      await session.page.waitForTimeout(300);
      continue;
    }
    await session.page.waitForTimeout(700);
    if (await isQwenVideoModeActive(composer)) {
      return;
    }
  }

  throw new ZeroTokenError("DOM_SEND_FAILED", "Could not enable Qwen video mode", {
    retryable: true,
  });
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

  await ensureQwenCapabilityMode(context);
  await input.click({ timeout: 5000 });
  await pasteInputImages(session.page, request);
  await waitForQwenAttachmentsReady(session, request.input.images?.length ?? 0);
  const prompt = getPromptFromInput(request.input);
  await writeQwenPrompt(input, session.page, prompt);

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
