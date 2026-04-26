import type { ProviderResponseParserModule } from "../parsers.js";
import { extractImageUrlsDeep, extractSseText } from "../../utils.js";

export type DoubaoGeneratedImage = {
  url: string;
  key?: string;
  width?: number;
  height?: number;
};

export const doubaoWebParser = {
  id: "doubao-web",
  parse(params) {
    const doubaoImages = extractDoubaoGeneratedImagesFromSse(params.raw);
    const fallbackImageUrls = doubaoImages.length > 0 ? [] : extractImageUrlsDeep(params.raw);
    const doubaoText = extractDoubaoSseText(params.raw);
    const genericText = extractSseText(params.raw);
    return {
      text: pickPreferredDoubaoText(doubaoText, genericText),
      images: [
        ...doubaoImages.map((image) => ({
          url: image.url,
          width: image.width,
          height: image.height,
          metadata: {
            source: params.imageSource,
            parser: "doubao-web",
            ...(image.key ? { key: image.key } : {}),
          },
        })),
        ...fallbackImageUrls.map((url) => ({
          url,
          metadata: { source: params.imageSource, parser: "generic-url-scan" },
        })),
      ],
    };
  },
} satisfies ProviderResponseParserModule;

export function extractDoubaoSseText(raw: string): string {
  const deltaChunks: string[] = [];
  const streamLeadTextChunks: string[] = [];
  const streamLeadTtsChunks: string[] = [];
  const streamPatchTextChunks: string[] = [];
  const streamPatchTtsChunks: string[] = [];

  for (const event of parseSseEvents(raw)) {
    let data: unknown;
    try {
      data = JSON.parse(event.data);
    } catch {
      continue;
    }
    switch (event.event) {
    case "CHUNK_DELTA":
      if (isRecord(data) && typeof data.text === "string") {
        deltaChunks.push(repairMojibakeText(data.text));
      }
      break;
    case "STREAM_MSG_NOTIFY":
      streamLeadTextChunks.push(...extractDoubaoContentBlockTexts(data));
      streamLeadTtsChunks.push(...extractDoubaoTtsTexts(data));
      break;
    case "STREAM_CHUNK":
      streamPatchTextChunks.push(...extractDoubaoPatchContentBlockTexts(data));
      streamPatchTtsChunks.push(...extractDoubaoPatchTtsTexts(data));
      break;
    default:
      continue;
    }
  }

  const leadText = streamLeadTextChunks.join("");
  const leadTts = streamLeadTtsChunks.join("");
  const patchText = streamPatchTextChunks.join("");
  const patchTts = streamPatchTtsChunks.join("");
  const deltaText = deltaChunks.join("");

  return pickPreferredDoubaoText(
    `${leadText}${deltaText}`,
    `${leadTts}${deltaText}`,
    `${leadText}${patchText}`,
    `${leadTts}${patchTts}`,
    patchText,
    patchTts,
    deltaText,
  );
}

function pickPreferredDoubaoText(...values: string[]): string {
  const candidates = values
    .map((text) => normalizeExtractedText(text))
    .filter((text, index, all) => text && all.indexOf(text) === index);
  if (candidates.length === 0) {
    return "";
  }

  let best = candidates[0];
  let bestScore = scoreExtractedText(best);
  for (const candidate of candidates.slice(1)) {
    const score = scoreExtractedText(candidate);
    if (score > bestScore || (score === bestScore && candidate.length > best.length)) {
      best = candidate;
      bestScore = score;
    }
  }
  return best;
}

function normalizeExtractedText(text: string): string {
  return text.replace(/\r\n/g, "\n").trim();
}

function scoreExtractedText(text: string): number {
  if (!text) {
    return Number.NEGATIVE_INFINITY;
  }

  let score = Math.min(text.length, 20000) / 1000;
  if (looksLikeStrictJSONObject(text)) {
    score += 200;
  }
  if (looksLikeStoryboardRoot(text)) {
    score += 120;
  }
  if (looksLikeIncompleteStoryboardBody(text)) {
    score -= 80;
  }
  if (canParseJSON(text)) {
    score += 300;
  }
  return score;
}

function looksLikeStrictJSONObject(text: string): boolean {
  const trimmed = text.trim();
  return trimmed.startsWith("{") && trimmed.endsWith("}");
}

function looksLikeStoryboardRoot(text: string): boolean {
  return /"meta"\s*:/.test(text) && /"project"\s*:/.test(text) && /"scenes"\s*:/.test(text);
}

function looksLikeIncompleteStoryboardBody(text: string): boolean {
  const trimmed = text.trimStart();
  return trimmed.startsWith('"meta"') || trimmed.startsWith('"project"') || trimmed.startsWith('"scenes"');
}

function canParseJSON(text: string): boolean {
  try {
    JSON.parse(text);
    return true;
  } catch {
    return false;
  }
}

function extractDoubaoContentBlockTexts(value: unknown): string[] {
  const records = isRecord(value) && isRecord(value.content)
    ? normalizeContentBlocks(value.content.content_block)
    : isRecord(value)
      ? normalizeContentBlocks(value.content_block)
      : [];
  return records
    .map((item) => readNestedString(item, ["content", "text_block", "text"]))
    .filter((text): text is string => typeof text === "string")
    .map(repairMojibakeText);
}

function extractDoubaoTtsTexts(value: unknown): string[] {
  if (!isRecord(value)) {
    return [];
  }
  const ttsContent = isRecord(value.content) ? value.content.tts_content : value.tts_content;
  return typeof ttsContent === "string" && ttsContent.length > 0 ? [repairMojibakeText(ttsContent)] : [];
}

function extractDoubaoPatchContentBlockTexts(value: unknown): string[] {
  if (!isRecord(value) || !Array.isArray(value.patch_op)) {
    return [];
  }
  const chunks: string[] = [];
  for (const patch of value.patch_op) {
    if (!isRecord(patch) || !isRecord(patch.patch_value)) {
      continue;
    }
    chunks.push(...extractDoubaoContentBlockTexts(patch.patch_value));
  }
  return chunks;
}

function extractDoubaoPatchTtsTexts(value: unknown): string[] {
  if (!isRecord(value) || !Array.isArray(value.patch_op)) {
    return [];
  }
  const chunks: string[] = [];
  for (const patch of value.patch_op) {
    if (!isRecord(patch) || !isRecord(patch.patch_value)) {
      continue;
    }
    chunks.push(...extractDoubaoTtsTexts(patch.patch_value));
  }
  return chunks;
}

function normalizeContentBlocks(value: unknown): Array<Record<string, unknown>> {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter((item): item is Record<string, unknown> => isRecord(item));
}

export function extractDoubaoGeneratedImagesFromSse(raw: string): DoubaoGeneratedImage[] {
  const images = new Map<string, DoubaoGeneratedImage>();
  for (const payload of parseSseDataPayloads(raw)) {
    collectDoubaoGeneratedImages(payload, images);
  }
  const collected = [...images.values()];
  const finalized = collected.filter(isFinalDoubaoGeneratedImage);
  return finalized.length > 0 ? finalized : collected;
}

export function extractDoubaoGeneratedImagesFromValue(value: unknown): DoubaoGeneratedImage[] {
  const images = new Map<string, DoubaoGeneratedImage>();
  collectDoubaoGeneratedImages(value, images);
  const collected = [...images.values()];
  const finalized = collected.filter(isFinalDoubaoGeneratedImage);
  return finalized.length > 0 ? finalized : collected;
}

function parseSseEvents(raw: string): Array<{ event: string; data: string }> {
  const events: Array<{ event: string; data: string }> = [];
  let event = "";
  let dataLines: string[] = [];
  const flush = () => {
    if (dataLines.length === 0) {
      return;
    }
    const data = dataLines.join("\n").trim();
    dataLines = [];
    if (!data || data === "[DONE]") {
      return;
    }
    events.push({ event, data });
  };

  for (const line of raw.split(/\r?\n/)) {
    if (!line.trim()) {
      flush();
      event = "";
      continue;
    }
    if (line.startsWith("event:")) {
      event = line.slice("event:".length).trim();
      continue;
    }
    if (line.startsWith("data:")) {
      dataLines.push(line.slice("data:".length).trimStart());
    }
  }
  flush();
  return events;
}

function parseSseDataPayloads(raw: string): unknown[] {
  const payloads: unknown[] = [];
  let dataLines: string[] = [];
  const flush = () => {
    if (dataLines.length === 0) {
      return;
    }
    const payload = dataLines.join("\n").trim();
    dataLines = [];
    if (!payload || payload === "[DONE]") {
      return;
    }
    try {
      payloads.push(JSON.parse(payload));
    } catch {
      // Ignore non-JSON SSE payloads.
    }
  };

  for (const line of raw.split(/\r?\n/)) {
    if (!line.trim()) {
      flush();
      continue;
    }
    if (line.startsWith("data:")) {
      dataLines.push(line.slice("data:".length).trimStart());
    }
  }
  flush();
  return payloads;
}

function collectDoubaoGeneratedImages(
  value: unknown,
  images: Map<string, DoubaoGeneratedImage>,
  depth = 0,
): void {
  if (depth > 12 || value === null || value === undefined) {
    return;
  }
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (
      (trimmed.startsWith("{") || trimmed.startsWith("[")) &&
      (trimmed.includes("creation_block") || trimmed.includes("image_ori"))
    ) {
      try {
        collectDoubaoGeneratedImages(JSON.parse(trimmed), images, depth + 1);
      } catch {
        // Not every nested string is complete JSON.
      }
    }
    return;
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      collectDoubaoGeneratedImages(item, images, depth + 1);
    }
    return;
  }
  if (!isRecord(value)) {
    return;
  }

  const image = value.image;
  if (isRecord(image)) {
    addDoubaoGeneratedImage(images, image);
  }
  for (const nested of Object.values(value)) {
    collectDoubaoGeneratedImages(nested, images, depth + 1);
  }
}

function addDoubaoGeneratedImage(
  images: Map<string, DoubaoGeneratedImage>,
  image: Record<string, unknown>,
): void {
  const url =
    readNestedString(image, ["image_ori_raw", "url"]) ??
    readNestedString(image, ["image_ori", "url"]) ??
    readNestedString(image, ["image_preview", "url"]) ??
    readNestedString(image, ["image_thumb", "url"]);
  if (!url) {
    return;
  }
  if (isExcludedDoubaoImageUrl(url)) {
    return;
  }
  const key = typeof image.key === "string" ? image.key : url;
  if (images.has(key)) {
    return;
  }
  const width =
    readNestedNumber(image, ["image_ori_raw", "width"]) ??
    readNestedNumber(image, ["image_ori", "width"]) ??
    readNestedNumber(image, ["image_preview", "width"]) ??
    readNestedNumber(image, ["image_thumb", "width"]);
  const height =
    readNestedNumber(image, ["image_ori_raw", "height"]) ??
    readNestedNumber(image, ["image_ori", "height"]) ??
    readNestedNumber(image, ["image_preview", "height"]) ??
    readNestedNumber(image, ["image_thumb", "height"]);
  images.set(key, {
    url,
    ...(typeof image.key === "string" ? { key: image.key } : {}),
    ...(typeof width === "number" ? { width } : {}),
    ...(typeof height === "number" ? { height } : {}),
  });
}

function isExcludedDoubaoImageUrl(url: string): boolean {
  const normalized = url.toLowerCase();
  return (
    /filebiztype\.(?:biz_)?bot_icon/.test(normalized) ||
    /(?:^|[/_.-])(icon|logo|avatar|emoji|sticker|badge|thumb)(?:$|[/_.-])/.test(normalized) ||
    /\/bot[_-]/.test(normalized)
  );
}

function isFinalDoubaoGeneratedImage(image: DoubaoGeneratedImage): boolean {
  const searchable = `${image.key ?? ""} ${image.url}`.toLowerCase();
  return /(?:^|[/_\s-])rc_gen_image(?:$|[/_\s-])/.test(searchable);
}

function repairMojibakeText(text: string): string {
  if (!/[ÃÂâãåäæçèéï]/.test(text) || /\p{Script=Han}/u.test(text)) {
    return text;
  }
  const bytes: number[] = [];
  for (const char of text) {
    const codePoint = char.codePointAt(0);
    if (codePoint === undefined) {
      return text;
    }
    if (codePoint <= 0xff) {
      bytes.push(codePoint);
      continue;
    }
    const mapped = cp1252ReverseMap.get(codePoint);
    if (mapped === undefined) {
      return text;
    }
    bytes.push(mapped);
  }
  try {
    const repaired = new TextDecoder("utf-8", { fatal: true }).decode(new Uint8Array(bytes));
    return repaired || text;
  } catch {
    return text;
  }
}

function readNestedString(value: Record<string, unknown>, path: string[]): string | undefined {
  let current: unknown = value;
  for (const key of path) {
    if (!isRecord(current)) {
      return undefined;
    }
    current = current[key];
  }
  return typeof current === "string" && current.trim() ? current : undefined;
}

function readNestedNumber(value: Record<string, unknown>, path: string[]): number | undefined {
  let current: unknown = value;
  for (const key of path) {
    if (!isRecord(current)) {
      return undefined;
    }
    current = current[key];
  }
  return typeof current === "number" ? current : undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

const cp1252ReverseMap = new Map<number, number>([
  [0x20ac, 0x80],
  [0x201a, 0x82],
  [0x0192, 0x83],
  [0x201e, 0x84],
  [0x2026, 0x85],
  [0x2020, 0x86],
  [0x2021, 0x87],
  [0x02c6, 0x88],
  [0x2030, 0x89],
  [0x0160, 0x8a],
  [0x2039, 0x8b],
  [0x0152, 0x8c],
  [0x017d, 0x8e],
  [0x2018, 0x91],
  [0x2019, 0x92],
  [0x201c, 0x93],
  [0x201d, 0x94],
  [0x2022, 0x95],
  [0x2013, 0x96],
  [0x2014, 0x97],
  [0x02dc, 0x98],
  [0x2122, 0x99],
  [0x0161, 0x9a],
  [0x203a, 0x9b],
  [0x0153, 0x9c],
  [0x017e, 0x9e],
  [0x0178, 0x9f],
]);
