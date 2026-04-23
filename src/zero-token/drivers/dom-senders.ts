import type { BrowserSessionManager } from "../browser-session.js";
import type { AuthProfile, ZeroTokenRequest, WebProviderConfig } from "../types.js";
import { sendDoubaoDomPrompt } from "./dom-senders/doubao.js";
import { sendQwenDomPrompt } from "./dom-senders/qwen.js";

export type DomSendPromptContext = {
  provider: WebProviderConfig;
  request: ZeroTokenRequest;
  session: Awaited<ReturnType<BrowserSessionManager["connect"]>>;
  auth?: AuthProfile;
};

export type DomPromptSender = (context: DomSendPromptContext) => Promise<void>;

const DOM_PROMPT_SENDERS: Partial<Record<string, DomPromptSender>> = {
  doubao: sendDoubaoDomPrompt,
  qwen: sendQwenDomPrompt,
};

export function resolveDomPromptSender(providerId: string): DomPromptSender | undefined {
  return DOM_PROMPT_SENDERS[providerId];
}
