import type { ProviderResponseParserModule } from "../parsers.js";
import { extractSseText } from "../../utils.js";

export const qwenWebParser = {
  id: "qwen-web",
  parse(params) {
    if (params.responseMode === "json") {
      const json = params.json ?? safeParseJson(params.raw);
      const videoUrls = extractQwenGeneratedVideosFromPolling(json);
      return {
        json,
        videos: videoUrls.map((url) => ({
          url,
          metadata: { source: params.imageSource, parser: "qwen-web" },
        })),
      };
    }

    const images = extractQwenGeneratedImagesFromSse(params.raw);
    return {
      text: extractSseText(params.raw),
      images: images.map((url) => ({
        url,
        metadata: { source: params.imageSource, parser: "qwen-web" },
      })),
    };
  },
} satisfies ProviderResponseParserModule;

export function extractQwenGeneratedVideosFromPolling(json: unknown): string[] {
  if (!isRecord(json)) {
    return [];
  }
  const data = json.data;
  if (!isRecord(data)) {
    return [];
  }
  const task = data.task;
  if (!isRecord(task)) {
    return [];
  }
  const status = task.task_status;
  if (status !== "SUCCEEDED") {
    return [];
  }
  const result = task.task_result;
  if (!isRecord(result)) {
    return [];
  }
  const videoUrl = result.video_url;
  return typeof videoUrl === "string" && videoUrl.length > 0 ? [videoUrl] : [];
}

export function extractQwenGeneratedImagesFromSse(raw: string): string[] {
  const images = new Set<string>();
  for (const payload of parseSseDataPayloads(raw)) {
    collectQwenGeneratedImages(payload, images);
  }
  return [...images];
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

function collectQwenGeneratedImages(value: unknown, images: Set<string>, depth = 0): void {
  if (depth > 12 || value === null || value === undefined) {
    return;
  }
  if (typeof value === "string") {
    return;
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      collectQwenGeneratedImages(item, images, depth + 1);
    }
    return;
  }
  if (!isRecord(value)) {
    return;
  }

  const imageList = value.image_list;
  if (Array.isArray(imageList)) {
    for (const item of imageList) {
      const url = isRecord(item) ? item.image : undefined;
      if (typeof url === "string" && isValidQwenImageUrl(url)) {
        images.add(url);
      }
    }
  }

  const toolResult = value.tool_result;
  if (Array.isArray(toolResult)) {
    for (const item of toolResult) {
      const url = isRecord(item) ? item.image : undefined;
      if (typeof url === "string" && isValidQwenImageUrl(url)) {
        images.add(url);
      }
    }
  }

  for (const nested of Object.values(value)) {
    collectQwenGeneratedImages(nested, images, depth + 1);
  }
}

function isValidQwenImageUrl(url: string): boolean {
  if (!url || url.startsWith("data:")) {
    return false;
  }
  try {
    const parsed = new URL(url);
    return parsed.hostname.endsWith("qwenlm.ai");
  } catch {
    return false;
  }
}

function safeParseJson(raw: string): unknown {
  try {
    return JSON.parse(raw);
  } catch {
    return undefined;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
