export type ZeroTokenErrorCode =
  | "PROVIDER_NOT_FOUND"
  | "CAPABILITY_NOT_SUPPORTED"
  | "MODEL_NOT_SUPPORTED"
  | "BROWSER_CDP_UNAVAILABLE"
  | "BROWSER_PROFILE_NOT_FOUND"
  | "BROWSER_LOGIN_EXPIRED"
  | "BROWSER_PAGE_OPEN_FAILED"
  | "DOM_INPUT_NOT_FOUND"
  | "DOM_SEND_FAILED"
  | "DOM_OUTPUT_TIMEOUT"
  | "EVAL_FETCH_FAILED"
  | "NETWORK_RESPONSE_TIMEOUT"
  | "SSE_PARSE_FAILED"
  | "IMAGE_URL_NOT_FOUND"
  | "IMAGE_DOWNLOAD_FAILED"
  | "WEB_HTTP_REPLAY_FAILED"
  | "WEB_DYNAMIC_PARAM_EXPIRED"
  | "WEB_CAPTCHA_REQUIRED"
  | "WEB_RATE_LIMITED"
  | "WEB_PAGE_CHANGED"
  | "CODEX_CLI_UNAVAILABLE";

export class ZeroTokenError extends Error {
  readonly code: ZeroTokenErrorCode;
  readonly retryable: boolean;
  readonly details?: unknown;

  constructor(code: ZeroTokenErrorCode, message: string, opts: { retryable?: boolean; details?: unknown } = {}) {
    super(message);
    this.name = "ZeroTokenError";
    this.code = code;
    this.retryable = opts.retryable ?? false;
    this.details = opts.details;
  }
}
