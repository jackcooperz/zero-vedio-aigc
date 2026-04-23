import { ZeroTokenError } from "../errors.js";
import type { BrowserSessionManager } from "../browser-session.js";
import type { AuthProfile, DomAction, DomDriverConfig, ZeroTokenRequest, ZeroTokenResult, ZeroTokenTransport, WebProviderConfig } from "../types.js";
import { extractImageUrlsDeep, getPromptFromInput } from "../utils.js";

async function runAction(page: any, action: DomAction): Promise<void> {
  if (action.type === "wait") {
    await page.waitForTimeout(action.timeoutMs);
    return;
  }
  if (action.type === "keyboard") {
    await page.keyboard.press(action.key);
    return;
  }
  const handle = await page.waitForSelector(action.selector, {
    timeout: action.timeoutMs ?? 5000,
    state: "visible",
  }).catch(() => null);
  if (!handle && action.type === "click") {
    throw new ZeroTokenError("DOM_SEND_FAILED", `Selector not found: ${action.selector}`, {
      retryable: true,
    });
  }
  if (handle) {
    await handle.click();
  }
}

async function readMainText(page: any, cfg: DomDriverConfig): Promise<string> {
  const extractor = cfg.outputExtractors?.[0];
  return page.evaluate((params: { selector?: string; excludeSelectors?: string[]; minLength?: number }) => {
    const { selector, excludeSelectors, minLength } = params;
    const clean = (text: string) => text.replace(/[\u200B-\u200D\uFEFF]/g, "").trim();
    const root =
      (selector ? document.querySelector(selector) : null) ??
      document.querySelector("main") ??
      document.querySelector('[role="main"]') ??
      document.body;
    const excluded = (excludeSelectors ?? [])
      .flatMap((item: string) => Array.from(document.querySelectorAll(item)) as Element[]);
    const isExcluded = (el: Element) =>
      excluded.some((excludedEl: Element) => excludedEl.contains(el));
    const candidates = Array.from(root.querySelectorAll("article, section, div, p"))
      .filter((el) => !isExcluded(el))
      .map((el) => clean((el as HTMLElement).innerText ?? ""))
      .filter((text) => text.length >= (minLength ?? 20))
      .filter((text) => !/(复制|分享|重新生成|Copy|Share|Regenerate)/i.test(text));
    return candidates.at(-1) ?? clean((root as HTMLElement).innerText ?? "");
  }, extractor ?? {});
}

async function readMainImages(page: any, cfg: DomDriverConfig): Promise<string[]> {
  const extractor = cfg.outputExtractors?.[0];
  const urls = await page.evaluate((params: { selector?: string; excludeSelectors?: string[] }) => {
    const { selector, excludeSelectors } = params;
    const root =
      (selector ? document.querySelector(selector) : null) ??
      document.querySelector("main") ??
      document.querySelector('[role="main"]') ??
      document.body;
    const excluded = (excludeSelectors ?? [])
      .flatMap((item: string) => Array.from(document.querySelectorAll(item)) as Element[]);
    const isExcluded = (el: Element) =>
      excluded.some((excludedEl: Element) => excludedEl.contains(el));
    const values: string[] = [];
    for (const el of Array.from(root.querySelectorAll("img, source, a"))) {
      if (isExcluded(el)) {
        continue;
      }
      const attr =
        el.getAttribute("src") ??
        el.getAttribute("srcset") ??
        el.getAttribute("href") ??
        "";
      if (attr) {
        values.push(attr);
      }
    }
    return values;
  }, extractor ?? {});
  return extractImageUrlsDeep(urls);
}

export async function sendDomPrompt(params: {
  provider: WebProviderConfig;
  request: ZeroTokenRequest;
  session: Awaited<ReturnType<BrowserSessionManager["connect"]>>;
  auth?: AuthProfile;
}): Promise<void> {
  const cfg = params.provider.domDriver;
  if (!cfg) {
    throw new ZeroTokenError("DOM_SEND_FAILED", `${params.provider.providerId} missing domDriver`);
  }

  if (params.auth?.cookie) {
    const domain = new URL(cfg.startUrl).hostname.replace(/^www\./, ".");
    await params.session.context.addCookies(
      params.auth.cookie
        .split(";")
        .map((raw) => raw.trim())
        .filter(Boolean)
        .map((raw) => {
          const [name, ...value] = raw.split("=");
          return { name, value: value.join("="), domain, path: "/" };
        }),
    );
  }

  if (!params.session.page.url().startsWith(cfg.startUrl)) {
    await params.session.page.goto(cfg.startUrl, { waitUntil: "domcontentloaded" });
  }

  for (const action of cfg.preActions ?? []) {
    await runAction(params.session.page, action);
  }

  let inputHandle: any = null;
  for (const selector of cfg.inputSelectors) {
    inputHandle = await params.session.page.$(selector);
    if (inputHandle) {
      break;
    }
  }
  if (!inputHandle) {
    throw new ZeroTokenError("DOM_INPUT_NOT_FOUND", "Could not find provider input box", {
      retryable: true,
    });
  }

  await inputHandle.click();
  await params.session.page.keyboard.type(getPromptFromInput(params.request.input), { delay: 15 });
  for (const action of cfg.sendActions) {
    await runAction(params.session.page, action);
  }
}

export async function runDomDriver(params: {
  provider: WebProviderConfig;
  request: ZeroTokenRequest;
  browser: BrowserSessionManager;
  auth?: AuthProfile;
  transport: ZeroTokenTransport;
}): Promise<ZeroTokenResult> {
  const cfg = params.provider.domDriver;
  if (!cfg) {
    throw new ZeroTokenError("DOM_SEND_FAILED", `${params.provider.providerId} missing domDriver`);
  }

  const session = await params.browser.connect(
    params.request.runtimeOptions?.browserProfileId ?? params.provider.browserProfileId,
    cfg.startUrl,
  );
  try {
    await sendDomPrompt({ provider: params.provider, request: params.request, session, auth: params.auth });

    const waitPolicy = cfg.waitPolicy ?? {
      type: "stable_text" as const,
      maxWaitMs: params.request.runtimeOptions?.timeoutMs ?? 120000,
      pollIntervalMs: 2000,
      stableRounds: 3,
    };
    let lastText = "";
    let lastSnapshot = "";
    let stable = 0;
    let lastImages: string[] = [];
    for (let elapsed = 0; elapsed <= waitPolicy.maxWaitMs; elapsed += waitPolicy.pollIntervalMs) {
      await session.page.waitForTimeout(waitPolicy.pollIntervalMs);
      const text = await readMainText(session.page, cfg);
      lastImages = await readMainImages(session.page, cfg);
      const snapshot = `${text}\n__images__\n${lastImages.join("\n")}`;
      if (snapshot.trim() && snapshot === lastSnapshot) {
        stable += 1;
      } else {
        stable = 0;
        lastSnapshot = snapshot;
        lastText = text;
      }
      if ((lastText || lastImages.length > 0) && stable >= waitPolicy.stableRounds) {
        return {
          requestId: params.request.requestId,
          providerRef: params.request.providerRef,
          transportUsed: params.transport,
          status: "success",
          output: {
            text: lastText,
            images: lastImages.map((url) => ({ url, metadata: { source: "dom" } })),
          },
          debug: { pageUrl: session.page.url() },
          usage: { billingMode: "web", apiTokens: 0, estimatedCost: 0 },
        };
      }
    }

    throw new ZeroTokenError("DOM_OUTPUT_TIMEOUT", "DOM output did not stabilize in time", {
      retryable: true,
    });
  } finally {
    await session.close();
  }
}
