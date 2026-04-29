import { ZeroTokenError } from "./errors.js";
import type { BrowserProfile } from "./types.js";

type PlaywrightBrowser = any;
type PlaywrightBrowserContext = any;
type PlaywrightPage = any;

export type BrowserSession = {
  browser: PlaywrightBrowser;
  context: PlaywrightBrowserContext;
  page: PlaywrightPage;
  close: () => Promise<void>;
};

function matchesPageUrl(pageUrl: string, startUrl?: string, pageUrlPatterns?: string[]): boolean {
  if (!pageUrl) {
    return false;
  }
  if (pageUrlPatterns?.some((pattern) => pageUrl.includes(pattern))) {
    return true;
  }
  if (!startUrl) {
    return false;
  }
  try {
    const currentUrl = new URL(pageUrl);
    const targetUrl = new URL(startUrl);
    return currentUrl.hostname === targetUrl.hostname;
  } catch {
    return pageUrl.startsWith(startUrl);
  }
}

function pickExistingPage(
  context: PlaywrightBrowserContext,
  startUrl?: string,
  pageUrlPatterns?: string[],
): PlaywrightPage | undefined {
  return context.pages().find((page: PlaywrightPage) => matchesPageUrl(page.url(), startUrl, pageUrlPatterns));
}

async function getChromeWebSocketUrl(cdpUrl: string, timeoutMs: number): Promise<string> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const res = await fetch(`${cdpUrl.replace(/\/$/, "")}/json/version`, {
      signal: controller.signal,
    });
    if (!res.ok) {
      throw new Error(`Chrome version endpoint returned ${res.status}`);
    }
    const data = (await res.json()) as { webSocketDebuggerUrl?: string };
    if (!data.webSocketDebuggerUrl) {
      throw new Error("Chrome version endpoint missing webSocketDebuggerUrl");
    }
    return data.webSocketDebuggerUrl;
  } finally {
    clearTimeout(timer);
  }
}

export class BrowserSessionManager {
  private readonly profiles: BrowserProfile[];
  private readonly attachedBrowsers = new Map<string, Promise<PlaywrightBrowser>>();

  constructor(profiles: BrowserProfile[] = []) {
    this.profiles = profiles;
  }

  resolveProfile(profileId?: string): BrowserProfile {
    const profile =
      this.profiles.find((candidate) => candidate.profileId === profileId) ??
      this.profiles[0];
    if (!profile) {
      throw new ZeroTokenError(
        "BROWSER_PROFILE_NOT_FOUND",
        "No browser profile configured for ZeroToken Web Runtime",
      );
    }
    return profile;
  }

  private async connectAttachedBrowser(profile: BrowserProfile): Promise<PlaywrightBrowser> {
    const { chromium } = await import("playwright-core");
    if (!profile.cdpUrl) {
      throw new ZeroTokenError("BROWSER_PROFILE_NOT_FOUND", `${profile.profileId} missing cdpUrl`);
    }
    const wsUrl = await getChromeWebSocketUrl(profile.cdpUrl, profile.defaultTimeoutMs ?? 5000).catch((error) => {
      throw new ZeroTokenError(
        "BROWSER_CDP_UNAVAILABLE",
        `Failed to connect to Debug Chrome at ${profile.cdpUrl}`,
        { retryable: true, details: error },
      );
    });
    const browser = await chromium.connectOverCDP(wsUrl);
    browser.on?.("disconnected", () => {
      this.attachedBrowsers.delete(profile.profileId);
    });
    return browser;
  }

  async shutdown(): Promise<void> {
    const browsers = Array.from(this.attachedBrowsers.values());
    this.attachedBrowsers.clear();
    const settled = await Promise.allSettled(browsers);
    await Promise.allSettled(
      settled
        .filter((item): item is PromiseFulfilledResult<PlaywrightBrowser> => item.status === "fulfilled")
        .map((item) => item.value.close()),
    );
  }

  async connect(profileId?: string, startUrl?: string, pageUrlPatterns?: string[]): Promise<BrowserSession> {
    const profile = this.resolveProfile(profileId);

    let browser: PlaywrightBrowser;
    if (profile.mode === "attach_only") {
      let browserPromise = this.attachedBrowsers.get(profile.profileId);
      if (!browserPromise) {
        browserPromise = this.connectAttachedBrowser(profile);
        this.attachedBrowsers.set(profile.profileId, browserPromise);
      }
      try {
        browser = await browserPromise;
      } catch (error) {
        this.attachedBrowsers.delete(profile.profileId);
        throw error;
      }
    } else {
      const { chromium } = await import("playwright-core");
      browser = await chromium.launchPersistentContext(profile.userDataDir ?? "", {
        headless: profile.headless ?? false,
      });
      const context = browser;
      const page = pickExistingPage(context, startUrl, pageUrlPatterns) ?? (await context.newPage());
      if (startUrl && !matchesPageUrl(page.url(), startUrl, pageUrlPatterns)) {
        await page.goto(startUrl, { waitUntil: "domcontentloaded" });
      }
      return {
        browser,
        context,
        page,
        close: async () => {
          await context.close();
        },
      };
    }

    const context = browser.contexts()[0] ?? (await browser.newContext());
    const page = pickExistingPage(context, startUrl, pageUrlPatterns) ?? (await context.newPage());
    if (startUrl && !matchesPageUrl(page.url(), startUrl, pageUrlPatterns)) {
      await page.goto(startUrl, { waitUntil: "domcontentloaded" });
    }
    return {
      browser,
      context,
      page,
      close: async () => {},
    };
  }
}
