import type { GeneratedImage, ProviderResponseParser } from "../types.js";
import { extractImageUrlsDeep, extractSseText } from "../utils.js";
import {
  extractDoubaoGeneratedImagesFromSse,
  extractDoubaoSseText,
} from "./doubao/parser.js";

export type ParsedProviderResponse = {
  text?: string;
  images?: GeneratedImage[];
};

export function parseProviderResponse(params: {
  parser?: ProviderResponseParser;
  raw: string;
  json?: unknown;
  responseMode?: "json" | "text" | "sse" | "stream";
  imageSource: string;
}): ParsedProviderResponse {
  if (params.parser === "doubao-web-sse") {
    return parseDoubaoWebSse(params.raw, params.imageSource);
  }
  return parseGenericResponse(params);
}

function parseDoubaoWebSse(raw: string, imageSource: string): ParsedProviderResponse {
  const doubaoImages = extractDoubaoGeneratedImagesFromSse(raw);
  const fallbackImageUrls = doubaoImages.length > 0 ? [] : extractImageUrlsDeep(raw);
  return {
    text: extractDoubaoSseText(raw) || extractSseText(raw),
    images: [
      ...doubaoImages.map((image) => ({
        url: image.url,
        width: image.width,
        height: image.height,
        metadata: {
          source: imageSource,
          parser: "doubao-web-sse",
          ...(image.key ? { key: image.key } : {}),
        },
      })),
      ...fallbackImageUrls.map((url) => ({
        url,
        metadata: { source: imageSource, parser: "generic-url-scan" },
      })),
    ],
  };
}

function parseGenericResponse(params: {
  raw: string;
  json?: unknown;
  responseMode?: "json" | "text" | "sse" | "stream";
  imageSource: string;
}): ParsedProviderResponse {
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
