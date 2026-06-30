import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const defaultProjectRoot = fileURLToPath(new URL("../../", import.meta.url));

export function loadProjectEnv(projectRoot = defaultProjectRoot): Record<string, string> {
  const envPath = join(projectRoot, ".env");
  let content = "";
  try {
    content = readFileSync(envPath, "utf8");
  } catch {
    return {};
  }

  const values = parseEnvContent(content);
  for (const [key, value] of Object.entries(values)) {
    if (!process.env[key]) {
      process.env[key] = value;
    }
  }
  return values;
}

function parseEnvContent(content: string): Record<string, string> {
  const values: Record<string, string> = {};
  for (const rawLine of content.split(/\r?\n/)) {
    let line = rawLine.trim();
    if (!line || line.startsWith("#")) {
      continue;
    }
    if (line.startsWith("export ")) {
      line = line.slice("export ".length).trim();
    }
    const index = line.indexOf("=");
    if (index <= 0) {
      continue;
    }
    const key = line.slice(0, index).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) {
      continue;
    }
    values[key] = parseEnvValue(line.slice(index + 1));
  }
  return values;
}

function parseEnvValue(raw: string): string {
  const value = raw.trim();
  if (value.startsWith('"')) {
    const end = value.lastIndexOf('"');
    if (end > 0) {
      try {
        return JSON.parse(value.slice(0, end + 1)) as string;
      } catch {
        return value.slice(1, end);
      }
    }
  }
  if (value.startsWith("'")) {
    const end = value.lastIndexOf("'");
    if (end > 0) {
      return value.slice(1, end);
    }
  }
  const comment = value.search(/\s#/);
  if (comment >= 0) {
    return value.slice(0, comment).trim();
  }
  return value;
}
