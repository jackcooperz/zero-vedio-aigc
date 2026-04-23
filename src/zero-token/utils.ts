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
