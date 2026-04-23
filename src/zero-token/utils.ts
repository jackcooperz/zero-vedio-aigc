export function interpolateTemplate(value: unknown, vars: Record<string, unknown>): unknown {
  if (typeof value === "string") {
    return value.replace(/\{\{([^}]+)\}\}/g, (_, key: string) => String(vars[key.trim()] ?? ""));
  }
  if (Array.isArray(value)) {
    return value.map((item) => interpolateTemplate(item, vars));
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, interpolateTemplate(item, vars)]),
    );
  }
  return value;
}

export function getPromptFromInput(input: { prompt?: string; messages?: Array<{ content: string }> }): string {
  if (input.prompt?.trim()) {
    return input.prompt;
  }
  const last = [...(input.messages ?? [])].reverse().find((message) => message.content.trim());
  return last?.content ?? "";
}

export async function streamToText(stream: ReadableStream<Uint8Array>): Promise<string> {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let text = "";
  while (true) {
    const { done, value } = await reader.read();
    if (done) {
      return text;
    }
    text += decoder.decode(value, { stream: true });
  }
}

export function extractSseText(raw: string): string {
  const chunks: string[] = [];
  for (const line of raw.split(/\r?\n/)) {
    if (!line.startsWith("data:")) {
      continue;
    }
    const payload = line.slice(5).trim();
    if (!payload || payload === "[DONE]") {
      continue;
    }
    try {
      const json = JSON.parse(payload) as any;
      const text =
        json.text ??
        json.delta ??
        json.choices?.[0]?.delta?.content ??
        json.choices?.[0]?.message?.content ??
        json.content?.text_block?.text;
      if (typeof text === "string") {
        chunks.push(text);
      }
    } catch {
      chunks.push(payload);
    }
  }
  return chunks.join("");
}

export function extractDoubaoSseText(raw: string): string {
  const chunks: string[] = [];
  for (const event of parseSseEvents(raw)) {
    if (event.event !== "CHUNK_DELTA") {
      continue;
    }
    let data: unknown;
    try {
      data = JSON.parse(event.data);
    } catch {
      continue;
    }
    if (!isRecord(data) || typeof data.text !== "string") {
      continue;
    }
    const text = repairMojibakeText(data.text);
    chunks.push(text);
  }
  return chunks.join("");
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

function looksGarbledText(text: string): boolean {
  if (!text.trim()) {
    return false;
  }
  if (text.includes("�")) {
    return true;
  }
  const hasHan = /\p{Script=Han}/u.test(text);
  const mojibakeScore = (text.match(/[ÃÂâãåäæçèéï]/g) || []).length;
  return !hasHan && text.length >= 4 && mojibakeScore / text.length > 0.25;
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

export type DoubaoGeneratedImage = {
  url: string;
  key?: string;
  width?: number;
  height?: number;
};

export function extractDoubaoGeneratedImagesFromSse(raw: string): DoubaoGeneratedImage[] {
  const images = new Map<string, DoubaoGeneratedImage>();
  for (const payload of parseSseDataPayloads(raw)) {
    collectDoubaoGeneratedImages(payload, images);
  }
  return [...images.values()];
}

export function extractDoubaoGeneratedImagesFromValue(value: unknown): DoubaoGeneratedImage[] {
  const images = new Map<string, DoubaoGeneratedImage>();
  collectDoubaoGeneratedImages(value, images);
  return [...images.values()];
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

export function extractImageUrlsDeep(value: unknown): string[] {
  const urls = new Set<string>();
  const addIfImageUrl = (raw: string) => {
    const normalized = raw
      .replace(/\\\//g, "/")
      .replace(/\\u0026/gi, "&")
      .replace(/&amp;/g, "&")
      .replace(/[),.;\]}]+$/g, "");
    try {
      const url = new URL(normalized);
      const searchable = `${url.hostname}${url.pathname}${url.search}`.toLowerCase();
      if (
        /\.(png|jpe?g|webp|gif|heic|avif)(\?|$)/i.test(normalized) ||
        /(?:^|[?&])(format|mime_type|image_format)=(png|jpe?g|webp|gif|heic|avif)/i.test(url.search) ||
        /(image|img|byteimg|tos-|doubao|seedream|volc)/i.test(searchable)
      ) {
        urls.add(normalized);
      }
    } catch {
      // Ignore non-URL fragments from loose extraction.
    }
  };

  const visit = (node: unknown) => {
    if (!node) {
      return;
    }
    if (typeof node === "string") {
      addIfImageUrl(node);
      const normalized = node
        .replace(/\\\//g, "/")
        .replace(/\\u0026/gi, "&");
      for (const match of normalized.matchAll(/https?:\/\/[^\s"'<>\\]+/g)) {
        addIfImageUrl(match[0]);
      }
      if (/^[\[{]/.test(node.trim())) {
        try {
          visit(JSON.parse(node));
        } catch {
          // The string may be an SSE fragment or partial JSON.
        }
      }
      return;
    }
    if (Array.isArray(node)) {
      for (const item of node) {
        visit(item);
      }
      return;
    }
    if (typeof node === "object") {
      for (const item of Object.values(node)) {
        visit(item);
      }
    }
  };
  visit(value);
  return [...urls];
}
