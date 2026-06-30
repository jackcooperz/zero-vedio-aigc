import { stdin as input, stdout as output, stderr as errorOutput } from "node:process";
import { loadProjectEnv } from "./env.js";
import {
  ZeroTokenRuntime,
  buildDefaultBrowserProfiles,
  buildDefaultZeroTokenProviders,
  type AuthProfile,
  type ZeroTokenCapability,
  type ZeroTokenRequest,
} from "./index.js";

loadProjectEnv();

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

async function readStdin(): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of input) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }
  return Buffer.concat(chunks).toString("utf8");
}

function writeJson(value: unknown) {
  output.write(`${JSON.stringify(value, null, 2)}\n`);
}

function writeErrorAndExit(error: unknown): never {
  const payload = {
    ok: false,
    error: error instanceof Error ? error.message : String(error),
    name: error instanceof Error ? error.name : "Error",
  };
  errorOutput.write(`${JSON.stringify(payload, null, 2)}\n`);
  process.exit(1);
}

async function main() {
  const command = process.argv[2];
  if (command !== "generate") {
    writeErrorAndExit(new Error("Unsupported command. Use: generate"));
  }

  const raw = await readStdin();
  if (!raw.trim()) {
    writeErrorAndExit(new Error("Missing stdin JSON payload"));
  }

  const payload = JSON.parse(raw) as BridgeGenerateInput;
  if (!payload.providerRef) {
    writeErrorAndExit(new Error("providerRef is required"));
  }

  const runtime = createRuntime();
  const result = await runtime.generate({
    requestId: payload.requestId ?? `bridge_${Date.now()}`,
    providerRef: payload.providerRef,
    capability: payload.capability,
    input: payload.input,
    runtimeOptions: payload.runtimeOptions,
  });

  writeJson({
    ok: true,
    result,
  });
}

main().catch(writeErrorAndExit);
