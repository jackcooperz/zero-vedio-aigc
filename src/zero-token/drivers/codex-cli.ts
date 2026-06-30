import { spawn } from "node:child_process";
import { mkdir, mkdtemp, readdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { loadProjectEnv } from "../env.js";
import { ZeroTokenError } from "../errors.js";
import type { GeneratedImage, ZeroTokenResult } from "../types.js";
import { getPromptFromInput } from "../utils.js";
import type { TransportDriverContext } from "./registry.js";

const IMAGE_EXT_RE = /\.(png|jpe?g|webp)$/i;
const STDERR_LIMIT = 64 * 1024;
const DEFAULT_TIMEOUT_MS = 1_200_000;
const DEFAULT_CODEX_BIN_ENV_VARS = ["ZERO_TOKEN_CODEX_PATH", "ZERO_TOKEN_CODEX_BIN"];
const DEFAULT_CODEX_HOME_ENV_VAR = "CODEX_HOME";
const DEFAULT_CODEX_BIN = "codex";

type CodexJsonEvent = {
  type?: string;
  thread_id?: string;
  item?: {
    id?: string;
    type?: string;
    status?: string;
    revised_prompt?: string;
    result?: string;
    saved_path?: string;
  };
  message?: string;
  error?: string;
  usage?: unknown;
};

function readFirstEnv(envVars: string[]) {
  for (const envVar of envVars) {
    const value = process.env[envVar]?.trim();
    if (value) {
      return { value, source: envVar };
    }
  }
  return undefined;
}

function resolveCodexCli(context: TransportDriverContext) {
  const cli = context.provider.type === "codex" ? context.provider.cli : undefined;
  const binEnvVars = cli?.binEnvVars?.length ? cli.binEnvVars : DEFAULT_CODEX_BIN_ENV_VARS;
  const configuredBin = readFirstEnv(binEnvVars);
  const bin = configuredBin?.value || cli?.defaultBin || DEFAULT_CODEX_BIN;
  const binSource = configuredBin?.source || "provider.defaultBin";
  const homeEnvVar = cli?.homeEnvVar || DEFAULT_CODEX_HOME_ENV_VAR;
  const home = process.env[homeEnvVar]?.trim() || path.join(os.homedir(), ".codex");
  return { bin, binSource, binEnvVars, home, homeEnvVar };
}

function codexGeneratedImagesDir(home: string, threadId: string) {
  return path.join(home, "generated_images", threadId);
}

async function snapshotImageDir(home: string, threadId?: string): Promise<Set<string>> {
  if (!threadId) {
    return new Set();
  }
  try {
    return new Set(await readdir(codexGeneratedImagesDir(home, threadId)));
  } catch {
    return new Set();
  }
}

async function discoverNewImages(home: string, threadId: string | undefined, knownFiles: Set<string>): Promise<string[]> {
  if (!threadId) {
    return [];
  }
  const dir = codexGeneratedImagesDir(home, threadId);
  try {
    const entries = await readdir(dir);
    return entries
      .filter((file) => !knownFiles.has(file) && IMAGE_EXT_RE.test(file))
      .map((file) => path.join(dir, file));
  } catch {
    return [];
  }
}

function buildCodexImagePrompt(prompt: string, aspectRatio?: unknown, count?: unknown) {
  const imageCount = typeof count === "number" && count > 0 ? Math.min(Math.floor(count), 4) : 1;
  const ratio = typeof aspectRatio === "string" && aspectRatio.trim() ? aspectRatio.trim() : "";
  return [
    `Use the image generation tool to create exactly ${imageCount} image${imageCount === 1 ? "" : "s"}.`,
    ratio ? `Required aspect ratio: ${ratio}.` : "",
    "Do not edit repository files or create any other artifact manually.",
    "Image prompt:",
    prompt,
  ].filter(Boolean).join("\n");
}

function parseCodexJsonLine(line: string): CodexJsonEvent | null {
  if (!line.trim()) {
    return null;
  }
  try {
    return JSON.parse(line) as CodexJsonEvent;
  } catch {
    return null;
  }
}

function imageFromEvent(item: NonNullable<CodexJsonEvent["item"]>): GeneratedImage | null {
  if (item.type !== "image_generation") {
    return null;
  }
  if (item.saved_path) {
    return {
      localPath: item.saved_path,
      mimeType: "image/png",
      metadata: {
        source: "codex_cli",
        status: item.status,
        revised_prompt: item.revised_prompt,
      },
    };
  }
  return null;
}

async function writeBase64Image(params: {
  requestId: string;
  threadId?: string;
  itemId?: string;
  result: string;
  revisedPrompt?: string;
  status?: string;
}): Promise<GeneratedImage> {
  const dir = path.join(os.tmpdir(), "zero-token-codex-images", params.threadId || params.requestId);
  await mkdir(dir, { recursive: true });
  const safeId = (params.itemId || `image_${Date.now()}`).replace(/[^a-z0-9_.-]/gi, "_");
  const localPath = path.join(dir, `${safeId}.png`);
  await writeFile(localPath, Buffer.from(params.result, "base64"));
  return {
    localPath,
    mimeType: "image/png",
    metadata: {
      source: "codex_cli_base64",
      status: params.status,
      revised_prompt: params.revisedPrompt,
    },
  };
}

export async function runCodexCliDriver(context: TransportDriverContext): Promise<ZeroTokenResult> {
  const prompt = getPromptFromInput(context.request.input).trim();
  if (!prompt) {
    throw new ZeroTokenError("DOM_SEND_FAILED", "Codex image prompt is empty", { retryable: false });
  }

  const timeoutMs = context.request.runtimeOptions?.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  loadProjectEnv();
  const codexCli = resolveCodexCli(context);
  const workDir = await mkdtemp(path.join(os.tmpdir(), "zero-token-codex-"));
  const stderrChunks: Buffer[] = [];
  const images: GeneratedImage[] = [];
  const errors: string[] = [];
  let stderrSize = 0;
  let stdoutBuffer = "";
  let text = "";
  let threadId = "";
  let usage: unknown;
  const base64ImageItems: NonNullable<CodexJsonEvent["item"]>[] = [];
  const env = { ...process.env };
  delete env.CODEX_CLI;
  delete env.CLAUDECODE;
  delete env.CLAUDE_CODE_ENTRYPOINT;

  const child = spawn(
    codexCli.bin,
    [
      "exec",
      "--json",
      "--dangerously-bypass-approvals-and-sandbox",
      "--skip-git-repo-check",
      buildCodexImagePrompt(prompt, context.request.input.aspectRatio, context.request.input.count),
    ],
    {
      cwd: workDir,
      env,
      stdio: ["ignore", "pipe", "pipe"],
    },
  );

  const timeout = setTimeout(() => {
    child.kill("SIGTERM");
  }, timeoutMs);

  child.stdout.on("data", (chunk: Buffer) => {
    stdoutBuffer += chunk.toString("utf8");
    let newline = stdoutBuffer.indexOf("\n");
    while (newline >= 0) {
      const line = stdoutBuffer.slice(0, newline);
      stdoutBuffer = stdoutBuffer.slice(newline + 1);
      const event = parseCodexJsonLine(line);
      if (event?.type === "thread.started" && event.thread_id) {
        threadId = event.thread_id;
      }
      if (event?.type === "item.completed" && event.item?.type === "agent_message") {
        text += (event.item as any).text ?? "";
      }
      if (event?.type === "item.completed" && event.item?.type === "image_generation") {
        const image = imageFromEvent(event.item);
        if (image) {
          images.push(image);
        } else if (event.item.result) {
          base64ImageItems.push(event.item);
        }
      }
      if (event?.type === "turn.completed") {
        usage = event.usage;
      }
      if (event?.type === "error") {
        errors.push(event.message || event.error || JSON.stringify(event));
      }
      newline = stdoutBuffer.indexOf("\n");
    }
  });

  child.stderr.on("data", (chunk: Buffer) => {
    if (stderrSize < STDERR_LIMIT) {
      stderrChunks.push(chunk);
      stderrSize += chunk.length;
      while (stderrSize > STDERR_LIMIT && stderrChunks.length > 1) {
        const dropped = stderrChunks.shift();
        if (dropped) {
          stderrSize -= dropped.length;
        }
      }
    }
  });

  let exitCode: number;
  try {
    exitCode = await new Promise<number>((resolve, reject) => {
      child.on("error", reject);
      child.on("exit", (code) => resolve(code ?? 0));
    });
  } catch (error: unknown) {
    clearTimeout(timeout);
    await rm(workDir, { recursive: true, force: true }).catch(() => {});
    const message = error instanceof Error ? error.message : String(error);
    throw new ZeroTokenError("CODEX_CLI_UNAVAILABLE", `Failed to start Codex CLI from ${codexCli.binSource}=${codexCli.bin}: ${message}`, {
      retryable: false,
      details: {
        codexBin: codexCli.bin,
        codexBinSource: codexCli.binSource,
        codexBinEnvVars: codexCli.binEnvVars,
      },
    });
  }
  clearTimeout(timeout);

  if (stdoutBuffer.trim()) {
    const event = parseCodexJsonLine(stdoutBuffer);
    if (event?.type === "thread.started" && event.thread_id) {
      threadId = event.thread_id;
    }
    if (event?.type === "item.completed" && event.item?.type === "image_generation" && event.item.result) {
      base64ImageItems.push(event.item);
    }
  }

  for (const item of base64ImageItems) {
    images.push(await writeBase64Image({
      requestId: context.request.requestId,
      threadId,
      itemId: item.id,
      result: item.result || "",
      revisedPrompt: item.revised_prompt,
      status: item.status,
    }));
  }

  const discovered = await discoverNewImages(codexCli.home, threadId, await snapshotImageDir(codexCli.home, undefined));
  for (const localPath of discovered) {
    if (!images.some((image) => image.localPath === localPath)) {
      images.push({
        localPath,
        mimeType: "image/png",
        metadata: { source: "codex_cli_generated_images_scan", thread_id: threadId },
      });
    }
  }

  await rm(workDir, { recursive: true, force: true }).catch(() => {});

  if (exitCode !== 0) {
    const stderr = Buffer.concat(stderrChunks).toString("utf8").trim().slice(-1000);
    throw new ZeroTokenError("NETWORK_RESPONSE_TIMEOUT", `Codex CLI exited ${exitCode}: ${errors.join("; ") || stderr || "unknown error"}`, {
      retryable: true,
      details: {
        codexBin: codexCli.bin,
        codexBinSource: codexCli.binSource,
        codexHome: codexCli.home,
        codexHomeEnvVar: codexCli.homeEnvVar,
        threadId,
      },
    });
  }
  if (images.length === 0) {
    throw new ZeroTokenError("NETWORK_RESPONSE_TIMEOUT", "Codex CLI completed without generated images", {
      retryable: true,
      details: {
        codexBin: codexCli.bin,
        codexBinSource: codexCli.binSource,
        codexHome: codexCli.home,
        codexHomeEnvVar: codexCli.homeEnvVar,
        threadId,
        text: text.slice(0, 1000),
        usage,
      },
    });
  }

  return {
    requestId: context.request.requestId,
    providerRef: context.request.providerRef,
    transportUsed: "codex_cli",
    status: "success",
    output: {
      text,
      images,
    },
    debug: {
      threadId,
      codexBin: codexCli.bin,
      codexBinSource: codexCli.binSource,
      codexHome: codexCli.home,
    },
    usage: { billingMode: "web", apiTokens: 0, estimatedCost: 0 },
  };
}
