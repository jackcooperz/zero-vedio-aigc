import type { GeneratedImage, GeneratedVideo, ProviderResponseMode, ProviderResponseParser } from "../types.js";
import { extractImageUrlsDeep, extractSseText } from "../utils.js";
import { doubaoWebParser } from "./doubao/parser.js";
import { qwenWebParser } from "./qwen/parser.js";

export type ParsedProviderResponse = {
  text?: string;
  json?: unknown;
  images?: GeneratedImage[];
  videos?: GeneratedVideo[];
};

export type ProviderResponseParserParams = {
  parser?: ProviderResponseParser;
  raw: string;
  json?: unknown;
  responseMode?: ProviderResponseMode;
  imageSource: string;
};

export type ProviderResponseParserModule = {
  id: ProviderResponseParser;
  parse: (params: ProviderResponseParserParams) => ParsedProviderResponse;
};

const genericParser = {
  id: "generic",
  parse: parseGenericResponse,
} satisfies ProviderResponseParserModule;

const RESPONSE_PARSERS = Object.fromEntries(
  [genericParser, doubaoWebParser, qwenWebParser].map((parser) => [parser.id, parser.parse]),
) as Record<ProviderResponseParser, ProviderResponseParserModule["parse"]>;

export function parseProviderResponse(params: ProviderResponseParserParams): ParsedProviderResponse {
  return RESPONSE_PARSERS[params.parser ?? "generic"](params);
}

function parseGenericResponse(params: ProviderResponseParserParams): ParsedProviderResponse {
  return {
    text: params.responseMode === "sse" || params.responseMode === "stream"
      ? extractSseText(params.raw)
      : params.raw,
    images: extractImageUrlsDeep(params.json ?? params.raw).map((url) => ({
      url,
      metadata: { source: params.imageSource, parser: "generic-url-scan" },
    })),
  };
}
