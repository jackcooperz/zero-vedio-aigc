import http from "node:http";
import {
  ZeroTokenRuntime,
  buildDefaultBrowserProfiles,
  buildDefaultZeroTokenProviders,
  type AuthProfile,
  type ZeroTokenCapability,
  type ZeroTokenRequest,
} from "./index.js";

type BridgeGenerateInput = {
  requestId?: string;
  providerRef: string;
  capability?: ZeroTokenCapability;
  input: ZeroTokenRequest["input"];
  runtimeOptions?: ZeroTokenRequest["runtimeOptions"];
};

function readAuthProfilesFromEnv(): AuthProfile[] {
  const profiles: AuthProfile[] = [];
  for (const provider of buildDefaultZeroTokenProviders()) {
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
    providers: buildDefaultZeroTokenProviders(),
    browserProfiles: buildDefaultBrowserProfiles(),
    authProfiles: readAuthProfilesFromEnv(),
  });
}

function writeJson(res: http.ServerResponse, status: number, value: unknown) {
  res.writeHead(status, { "content-type": "application/json; charset=utf-8" });
  res.end(`${JSON.stringify(value, null, 2)}\n`);
}

async function readRequestBody(req: http.IncomingMessage): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }
  return Buffer.concat(chunks).toString("utf8");
}

async function main() {
  const runtime = createRuntime();
  const port = Number.parseInt(process.env.ZERO_TOKEN_BRIDGE_PORT ?? "4390", 10) || 4390;
  const startedAt = new Date().toISOString();

  const server = http.createServer(async (req, res) => {
    try {
      if (req.method === "GET" && req.url === "/healthz") {
        writeJson(res, 200, { ok: true, startedAt });
        return;
      }

      if (req.method === "POST" && req.url === "/generate") {
        const raw = await readRequestBody(req);
        if (!raw.trim()) {
          writeJson(res, 400, { ok: false, error: "Missing JSON body", name: "Error" });
          return;
        }
        const payload = JSON.parse(raw) as BridgeGenerateInput;
        if (!payload.providerRef) {
          writeJson(res, 400, { ok: false, error: "providerRef is required", name: "Error" });
          return;
        }

        const result = await runtime.generate({
          requestId: payload.requestId ?? `bridge_${Date.now()}`,
          providerRef: payload.providerRef,
          capability: payload.capability,
          input: payload.input,
          runtimeOptions: payload.runtimeOptions,
        });

        writeJson(res, 200, { ok: true, result });
        return;
      }

      writeJson(res, 404, { ok: false, error: "Not found", name: "Error" });
    } catch (error) {
      writeJson(res, 500, {
        ok: false,
        error: error instanceof Error ? error.message : String(error),
        name: error instanceof Error ? error.name : "Error",
      });
    }
  });

  const shutdown = async () => {
    server.close();
    await runtime.shutdown();
  };

  process.on("SIGINT", () => {
    shutdown().finally(() => process.exit(0));
  });
  process.on("SIGTERM", () => {
    shutdown().finally(() => process.exit(0));
  });

  server.listen(port, "127.0.0.1", () => {
    process.stdout.write(`ZeroToken bridge server listening on http://127.0.0.1:${port}\n`);
  });
}

main().catch((error) => {
  process.stderr.write(`${error instanceof Error ? error.stack ?? error.message : String(error)}\n`);
  process.exit(1);
});
