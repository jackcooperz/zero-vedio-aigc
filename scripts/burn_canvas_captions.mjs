import fs from "node:fs/promises";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { pathToFileURL } from "node:url";

const SKIA_CANDIDATES = [
  "skia-canvas",
  "/Users/mac/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/@oai/artifact-tool/node_modules/skia-canvas/lib/index.js",
];
const FONT = "/System/Library/Fonts/STHeiti Medium.ttc";

async function loadSkiaCanvas() {
  let lastError;
  for (const candidate of SKIA_CANDIDATES) {
    try {
      if (candidate.startsWith("/")) {
        return await import(pathToFileURL(candidate).href);
      }
      return await import(candidate);
    } catch (error) {
      lastError = error;
    }
  }
  throw lastError ?? new Error("skia-canvas not found");
}

function parseArgs(argv) {
  const args = {};
  for (let i = 0; i < argv.length; i += 1) {
    const item = argv[i];
    if (item.startsWith("--")) {
      args[item.slice(2)] = argv[i + 1];
      i += 1;
    }
  }
  return args;
}

function run(cmd, args) {
  const result = spawnSync(cmd, args, { stdio: "inherit", encoding: "utf8" });
  if (result.status !== 0) {
    throw new Error(`${cmd} failed: ${args.join(" ")}`);
  }
}

function parseTime(value) {
  const match = String(value ?? "").match(/(\d+):(\d+):(\d+),(\d+)/);
  if (!match) return 0;
  const [, h, m, s, ms] = match;
  return Number(h) * 3600 + Number(m) * 60 + Number(s) + Number(ms) / 1000;
}

function parseSrt(text) {
  return text
    .split(/\n\s*\n/)
    .map((block) => block.trim())
    .filter(Boolean)
    .map((block) => {
      const lines = block.split(/\n/);
      const timing = lines[1] ?? "";
      const [startRaw, endRaw] = timing.split("-->").map((item) => item.trim());
      return {
        start: parseTime(startRaw),
        end: parseTime(endRaw),
        text: lines.slice(2).join("\n").trim(),
      };
    })
    .filter((item) => item.text && item.end > item.start);
}

function wrapText(ctx, text, maxWidth, font, maxLines = 2) {
  ctx.font = font;
  const sourceLines = [String(text).replace(/\s*\n\s*/g, "")].filter(Boolean);
  const lines = [];
  for (const source of sourceLines) {
    let line = "";
    for (const char of Array.from(source)) {
      const next = line + char;
      if (ctx.measureText(next).width > maxWidth && line) {
        lines.push(line);
        line = char;
      } else {
        line = next;
      }
    }
    if (line) lines.push(line);
  }
  return rebalanceShortLastLine(lines.slice(0, maxLines));
}

function rebalanceShortLastLine(lines) {
  if (lines.length !== 2) return lines;
  const first = Array.from(lines[0]);
  const second = Array.from(lines[1]);
  while (second.length < 4 && first.length > second.length + 5) {
    second.unshift(first.pop());
  }
  return [first.join(""), second.join("")].filter(Boolean);
}

function activeCaption(captions, seconds) {
  return captions.find((item) => seconds >= item.start && seconds < item.end);
}

function visibleChars(text) {
  return Array.from(String(text)).filter((char) => !/\s/.test(char));
}

function activeCharIndex(caption, seconds) {
  const chars = visibleChars(caption.text);
  if (!chars.length) return -1;
  const duration = Math.max(0.1, caption.end - caption.start);
  const progress = Math.max(0, Math.min(0.999, (seconds - caption.start) / duration));
  return Math.min(chars.length - 1, Math.floor(progress * chars.length));
}

function clamp(value, min, max) {
  return Math.max(min, Math.min(max, value));
}

function regionScore(ctx, rect, width, height) {
  const x = clamp(Math.round(rect.x), 0, width - 1);
  const y = clamp(Math.round(rect.y), 0, height - 1);
  const w = clamp(Math.round(rect.w), 1, width - x);
  const h = clamp(Math.round(rect.h), 1, height - y);
  const image = ctx.getImageData(x, y, w, h).data;
  const step = Math.max(3, Math.floor(Math.sqrt((w * h) / 700)));
  let count = 0;
  let sum = 0;
  let sumSq = 0;
  let edgeSum = 0;
  let edgeCount = 0;
  for (let yy = 0; yy < h; yy += step) {
    let prevLum = null;
    for (let xx = 0; xx < w; xx += step) {
      const offset = (yy * w + xx) * 4;
      const lum = image[offset] * 0.299 + image[offset + 1] * 0.587 + image[offset + 2] * 0.114;
      sum += lum;
      sumSq += lum * lum;
      count += 1;
      if (prevLum != null) {
        edgeSum += Math.abs(lum - prevLum);
        edgeCount += 1;
      }
      prevLum = lum;
    }
  }
  const avg = count ? sum / count : 0;
  const variance = count ? Math.max(0, sumSq / count - avg * avg) : 0;
  const edge = edgeCount ? edgeSum / edgeCount : 0;
  const centerOverlap =
    rect.x < width * 0.74 &&
    rect.x + rect.w > width * 0.26 &&
    rect.y < height * 0.82 &&
    rect.y + rect.h > height * 0.24;
  return Math.sqrt(variance) * 0.9 + edge * 0.7 + (centerOverlap ? 78 : 0);
}

function measureLines(ctx, lines) {
  return Math.max(...lines.map((line) => ctx.measureText(line).width), 1);
}

function buildCaptionLayouts(ctx, caption, width, height, font, fontSize, lineHeight) {
  const candidates = [
    { name: "bottom-center", x: width * 0.5, y: height * 0.865, maxWidth: width * 0.76, align: "center", bias: 0 },
  ];
  const margin = Math.round(fontSize * 0.8);
  return candidates
    .map((candidate) => {
      const lines = wrapText(ctx, caption.text, candidate.maxWidth, font, 3);
      if (!lines.length) return null;
      const textWidth = measureLines(ctx, lines);
      const textHeight = lineHeight * lines.length;
      const left = candidate.align === "center" ? candidate.x - textWidth / 2 : candidate.x;
      const top = candidate.y - textHeight / 2;
      const rect = {
        x: clamp(left - margin, 0, width - 1),
        y: clamp(top - margin, 0, height - 1),
        w: Math.min(width - 1, textWidth + margin * 2),
        h: Math.min(height - 1, textHeight + margin * 2),
      };
      return {
        ...candidate,
        lines,
        textWidth,
        textHeight,
        score: candidate.bias,
      };
    })
    .filter(Boolean)
    .sort((a, b) => a.score - b.score);
}

function drawLineByChars(ctx, line, x, y, activeIndex, visibleOffset, normalStyle, activeStyle) {
  let cursor = x;
  let visibleCursor = visibleOffset;
  for (const char of Array.from(line)) {
    const charWidth = ctx.measureText(char).width;
    const isVisible = !/\s/.test(char);
    const isActive = isVisible && visibleCursor === activeIndex;
    const style = isActive ? activeStyle : normalStyle;

    ctx.save();
    ctx.lineWidth = style.lineWidth;
    ctx.strokeStyle = style.strokeStyle;
    ctx.fillStyle = style.fillStyle;
    ctx.shadowColor = style.shadowColor;
    ctx.shadowBlur = style.shadowBlur;
    ctx.shadowOffsetY = style.shadowOffsetY;
    ctx.strokeText(char, cursor, y);
    ctx.fillText(char, cursor, y);
    ctx.restore();

    cursor += charWidth;
    if (isVisible) visibleCursor += 1;
  }
  return visibleCursor;
}

function drawCaption(ctx, caption, width, height, seconds) {
  if (!caption) return;
  const fontSize = Math.max(34, Math.round(height * 0.052));
  const lineHeight = Math.round(fontSize * 1.28);
  const font = `bold ${fontSize}px STHeiti`;
  ctx.font = font;
  const layout = buildCaptionLayouts(ctx, caption, width, height, font, fontSize, lineHeight)[0];
  if (!layout) return;

  ctx.save();
  ctx.font = font;
  ctx.textAlign = "left";
  ctx.textBaseline = "middle";
  ctx.lineJoin = "round";
  const normalStyle = {
    lineWidth: Math.max(5, Math.round(fontSize * 0.13)),
    strokeStyle: "rgba(35, 26, 18, 0.82)",
    fillStyle: "#fffaf0",
    shadowColor: "rgba(0, 0, 0, 0.46)",
    shadowBlur: Math.round(fontSize * 0.12),
    shadowOffsetY: Math.max(1, Math.round(fontSize * 0.035)),
  };
  const activeStyle = {
    lineWidth: Math.max(6, Math.round(fontSize * 0.15)),
    strokeStyle: "rgba(78, 50, 0, 0.92)",
    fillStyle: "#ffd84f",
    shadowColor: "rgba(255, 232, 133, 0.72)",
    shadowBlur: Math.round(fontSize * 0.28),
    shadowOffsetY: Math.max(1, Math.round(fontSize * 0.025)),
  };

  const firstY = layout.y - ((layout.lines.length - 1) * lineHeight) / 2;
  const activeIndex = activeCharIndex(caption, seconds);
  let visibleOffset = 0;
  layout.lines.forEach((line, idx) => {
    const yy = firstY + idx * lineHeight;
    const lineWidth = ctx.measureText(line).width;
    const x = layout.align === "center" ? layout.x - lineWidth / 2 : layout.x;
    visibleOffset = drawLineByChars(ctx, line, x, yy, activeIndex, visibleOffset, normalStyle, activeStyle);
  });
  ctx.restore();
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const framesDir = path.resolve(args["frames-dir"]);
  const srt = path.resolve(args.srt);
  const outFramesDir = path.resolve(args["out-frames-dir"]);
  const audioVideo = path.resolve(args["audio-video"]);
  const output = path.resolve(args.output);
  const fps = Number(args.fps ?? 24);
  const ffmpeg = args.ffmpeg ?? "ffmpeg";

  const { Canvas, loadImage, FontLibrary } = await loadSkiaCanvas();
  FontLibrary.use("STHeiti", FONT);

  await fs.rm(outFramesDir, { recursive: true, force: true });
  await fs.mkdir(outFramesDir, { recursive: true });
  await fs.mkdir(path.dirname(output), { recursive: true });

  const captions = parseSrt(await fs.readFile(srt, "utf8"));
  const files = (await fs.readdir(framesDir)).filter((file) => file.endsWith(".png")).sort();
  for (let i = 0; i < files.length; i += 1) {
    const file = files[i];
    const seconds = i / fps;
    const img = await loadImage(path.join(framesDir, file));
    const canvas = new Canvas(img.width, img.height);
    const ctx = canvas.getContext("2d");
    ctx.drawImage(img, 0, 0);
    drawCaption(ctx, activeCaption(captions, seconds), img.width, img.height, seconds);
    await fs.writeFile(path.join(outFramesDir, file), await canvas.toBuffer("png"));
  }

  run(ffmpeg, [
    "-y",
    "-framerate", String(fps),
    "-i", path.join(outFramesDir, "%06d.png"),
    "-i", audioVideo,
    "-map", "0:v:0",
    "-map", "1:a:0",
    "-c:v", "libx264",
    "-pix_fmt", "yuv420p",
    "-crf", "18",
    "-preset", "medium",
    "-c:a", "copy",
    "-movflags", "+faststart",
    output,
  ]);

  console.log(JSON.stringify({ output, frames: files.length, fps }, null, 2));
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
