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

  async connect(profileId?: string, startUrl?: string): Promise<BrowserSession> {
    const profile = this.resolveProfile(profileId);
    const { chromium } = await import("playwright-core");

    let browser: PlaywrightBrowser;
    if (profile.mode === "attach_only") {
      if (!profile.cdpUrl) {
        throw new ZeroTokenError("BROWSER_PROFILE_NOT_FOUND", `${profile.profileId} missing cdpUrl`);
      }
      const wsUrl = await getChromeWebSocketUrl(profile.cdpUrl, profile.defaultTimeoutMs ?? 5000).catch(
        (error) => {
          throw new ZeroTokenError(
            "BROWSER_CDP_UNAVAILABLE",
            `Failed to connect to Debug Chrome at ${profile.cdpUrl}`,
            { retryable: true, details: error },
          );
        },
      );
      browser = await chromium.connectOverCDP(wsUrl);
    } else {
      browser = await chromium.launchPersistentContext(profile.userDataDir ?? "", {
        headless: profile.headless ?? false,
      });
      const context = browser;
      const page = context.pages()[0] ?? (await context.newPage());
      if (startUrl) {
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
    const page = context.pages()[0] ?? (await context.newPage());
    if (startUrl && !page.url().includes(new URL(startUrl).hostname)) {
      await page.goto(startUrl, { waitUntil: "domcontentloaded" });
    }
    return {
      browser,
      context,
      page,
      close: async () => {
        await browser.close();
      },
    };
  }
}

