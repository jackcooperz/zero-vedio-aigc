import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { extname, join, normalize } from "node:path";
import { fileURLToPath } from "node:url";
import {
  ZeroTokenRuntime,
  buildDefaultBrowserProfiles,
  buildDefaultZeroTokenProviders,
  type AuthProfile,
} from "../zero-token/index.js";

const rootDir = fileURLToPath(new URL("../../", import.meta.url));
const publicDir = join(rootDir, "src", "web-demo", "public");

const providers = buildDefaultZeroTokenProviders();
const browserProfiles = buildDefaultBrowserProfiles();

function readAuthProfilesFromEnv(): AuthProfile[] {
  const profiles: AuthProfile[] = [];
  for (const provider of providers) {
    if (!provider.authProfileId) {
      continue;
    }
    const envPrefix = provider.providerId.toUpperCase().replace(/[^A-Z0-9]/g, "_");
    const cookie = process.env[`${envPrefix}_COOKIE`];
    const userAgent = process.env[`${envPrefix}_USER_AGENT`];
    if (cookie || userAgent) {
      profiles.push({
        authProfileId: provider.authProfileId,
        providerId: provider.providerId,
        authType: "cookie",
        cookie,
        userAgent,
      });
    }
  }
  return profiles;
}

function createRuntime() {
  return new ZeroTokenRuntime({
    providers,
    browserProfiles,
    authProfiles: readAuthProfilesFromEnv(),
  });
}

function sendJson(res: any, status: number, value: unknown) {
  const body = JSON.stringify(value, createJsonReplacer(), 2);
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Content-Length": Buffer.byteLength(body),
  });
  res.end(body);
}

function createJsonReplacer() {
  const seen = new WeakSet<object>();
  return (_key: string, value: unknown) => {
    if (value instanceof Error) {
      const base = {
        name: value.name,
        message: value.message,
        stack: value.stack,
      } as Record<string, unknown>;
      const errorRecord = value as unknown as Record<string, unknown>;
      for (const key of Object.getOwnPropertyNames(value)) {
        if (!(key in base)) {
          base[key] = errorRecord[key];
        }
      }
      return {
        ...base,
        ...(value instanceof AggregateError && value.errors ? { errors: value.errors } : {}),
      };
    }
    if (typeof value === "object" && value !== null) {
      if (seen.has(value)) {
        return "[Circular]";
      }
      seen.add(value);
    }
    return value;
  };
}

function readBody(req: any): Promise<unknown> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk: Buffer) => chunks.push(chunk));
    req.on("end", () => {
      const raw = Buffer.concat(chunks).toString("utf8");
      if (!raw.trim()) {
        resolve({});
        return;
      }
      try {
        resolve(JSON.parse(raw));
      } catch (error) {
        reject(error);
      }
    });
    req.on("error", reject);
  });
}

function summarizeProviders() {
  return providers.map((provider) => ({
    providerId: provider.providerId,
    label: provider.label,
    aliases: provider.aliases ?? [],
    transportStrategy: provider.transportStrategy,
    models: provider.models,
  }));
}

async function handleApi(req: any, res: any, url: URL) {
  if (req.method === "GET" && url.pathname === "/api/providers") {
    sendJson(res, 200, {
      providers: summarizeProviders(),
      browserProfiles,
      authProfilesFromEnv: readAuthProfilesFromEnv().map((profile) => ({
        authProfileId: profile.authProfileId,
        providerId: profile.providerId,
        hasCookie: Boolean(profile.cookie),
        hasUserAgent: Boolean(profile.userAgent),
      })),
    });
    return;
  }

  if (req.method === "POST" && url.pathname === "/api/generate") {
    const body = (await readBody(req)) as {
      providerRef?: string;
      prompt?: string;
      count?: number;
      transportPreference?: string[];
    };
    if (!body.providerRef || !body.prompt) {
      sendJson(res, 400, {
        error: "providerRef and prompt are required",
      });
      return;
    }
    const runtime = createRuntime();
    try {
      const result = await runtime.generate({
        requestId: `web_${Date.now()}`,
        providerRef: body.providerRef,
        capability: "text_generation",
        transportPreference: body.transportPreference as any,
        input: {
          prompt: body.prompt,
          count: body.count ?? 1,
        },
        runtimeOptions: {
          browserProfileId: "chrome_main",
          timeoutMs: 180000,
          retryLimit: 1,
          saveDebugArtifacts: false,
        },
      });
      sendJson(res, 200, result);
    } catch (error) {
      sendJson(res, 500, {
        error: error instanceof Error ? error.message : String(error),
        name: error instanceof Error ? error.name : "Error",
        details: error,
      });
    }
    return;
  }

  sendJson(res, 404, { error: "API route not found" });
}

async function serveStatic(res: any, pathname: string) {
  const requested = pathname === "/" ? "/index.html" : pathname;
  const safePath = normalize(requested).replace(/^(\.\.[/\\])+/, "");
  const filePath = join(publicDir, safePath);
  const contentType =
    extname(filePath) === ".html"
      ? "text/html; charset=utf-8"
      : extname(filePath) === ".css"
        ? "text/css; charset=utf-8"
        : extname(filePath) === ".js"
          ? "text/javascript; charset=utf-8"
          : "application/octet-stream";
  try {
    const body = await readFile(filePath);
    res.writeHead(200, {
      "Content-Type": contentType,
      "Content-Length": body.length,
    });
    res.end(body);
  } catch {
    res.writeHead(404, { "Content-Type": "text/plain; charset=utf-8" });
    res.end("Not found");
  }
}

const port = Number(process.env.PORT ?? 4317);

const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url ?? "/", `http://${req.headers.host ?? "localhost"}`);
    if (url.pathname.startsWith("/api/")) {
      await handleApi(req, res, url);
      return;
    }
    await serveStatic(res, url.pathname);
  } catch (error) {
    sendJson(res, 500, {
      error: error instanceof Error ? error.message : String(error),
    });
  }
});

server.on("error", (error: NodeJS.ErrnoException) => {
  console.error(`ZeroToken web demo failed to start on 127.0.0.1:${port}: ${error.message}`);
  process.exitCode = 1;
});

server.listen(port, "127.0.0.1", () => {
  console.log(`ZeroToken web demo: http://127.0.0.1:${port}`);
});
