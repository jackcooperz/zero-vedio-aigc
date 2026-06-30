#!/usr/bin/env bash
set -euo pipefail

# Start Chrome / Chromium with Chrome DevTools Protocol enabled.
# Used by ZeroToken Web Runtime to connect through Playwright connectOverCDP.
# Compatible with macOS, Linux, WSL, and Git Bash on Windows.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

trim() {
  local value="$*"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

parse_env_value() {
  local value
  value="$(trim "$1")"
  if [[ "$value" == \"*\" && "$value" == *\" ]]; then
    value="${value:1:${#value}-2}"
  elif [[ "$value" == \'*\' && "$value" == *\' ]]; then
    value="${value:1:${#value}-2}"
  else
    value="$(trim "${value%% #*}")"
  fi
  printf '%s' "$value"
}

load_project_env() {
  local env_file="$ROOT_DIR/.env"
  [[ -f "$env_file" ]] || return 0
  local line key value
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="$(trim "$line")"
    [[ -z "$line" || "$line" == \#* ]] && continue
    [[ "$line" == export\ * ]] && line="$(trim "${line#export }")"
    [[ "$line" == *=* ]] || continue
    key="$(trim "${line%%=*}")"
    [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue
    if [[ -z "${!key+x}" ]]; then
      value="$(parse_env_value "${line#*=}")"
      export "$key=$value"
    fi
  done < "$env_file"
}

load_project_env

PORT="${ZERO_TOKEN_CHROME_DEBUG_PORT:-9222}"
OPEN_LOGIN_PAGES="${ZERO_TOKEN_OPEN_LOGIN_PAGES:-1}"
RESTART_CHROME="${ZERO_TOKEN_RESTART_CHROME:-0}"

echo "=========================================="
echo "  ZeroToken Chrome Debug Launcher"
echo "=========================================="
echo ""

detect_os() {
  case "$(uname -s)" in
    Darwin*) echo "mac" ;;
    MINGW*|MSYS*|CYGWIN*) echo "win" ;;
    *)
      if grep -qi microsoft /proc/version 2>/dev/null; then
        echo "wsl"
      else
        echo "linux"
      fi
      ;;
  esac
}

detect_chrome() {
  local linux_paths=(
    "/opt/google/chrome/google-chrome"
    "/usr/bin/google-chrome"
    "/usr/bin/google-chrome-stable"
    "/usr/bin/chromium"
    "/usr/bin/chromium-browser"
    "/snap/bin/chromium"
  )
  local mac_paths=(
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
    "/Applications/Chromium.app/Contents/MacOS/Chromium"
  )
  local win_paths=(
    "${PROGRAMFILES:-}/Google/Chrome/Application/chrome.exe"
    "${PROGRAMFILES:-} (x86)/Google/Chrome/Application/chrome.exe"
    "${LOCALAPPDATA:-}/Google/Chrome/Application/chrome.exe"
    "${PROGRAMFILES:-}/Chromium/Application/chrome.exe"
  )
  local wsl_win_paths=(
    "/mnt/c/Program Files/Google/Chrome/Application/chrome.exe"
    "/mnt/c/Program Files (x86)/Google/Chrome/Application/chrome.exe"
    "/mnt/c/Users/$USER/AppData/Local/Google/Chrome/Application/chrome.exe"
    "/mnt/c/Program Files/Chromium/Application/chrome.exe"
  )

  if [[ -n "${CHROME_PATH:-}" && -f "$CHROME_PATH" ]]; then
    echo "$CHROME_PATH"
    return
  fi

  case "$OS" in
    mac)
      for path in "${mac_paths[@]}"; do
        [[ -f "$path" ]] && echo "$path" && return
      done
      ;;
    win)
      for path in "${win_paths[@]}"; do
        [[ -f "$path" ]] && echo "$path" && return
      done
      ;;
    wsl|linux)
      for path in "${linux_paths[@]}"; do
        [[ -f "$path" ]] && echo "$path" && return
      done
      for cmd in google-chrome google-chrome-stable chromium chromium-browser; do
        command -v "$cmd" >/dev/null 2>&1 && echo "$cmd" && return
      done
      if [[ "$OS" == "wsl" ]]; then
        for path in "${wsl_win_paths[@]}"; do
          [[ -f "$path" ]] && echo "$path" && return
        done
      fi
      ;;
  esac
  echo ""
}

detect_user_data_dir() {
  if [[ -n "${ZERO_TOKEN_CHROME_USER_DATA_DIR:-}" ]]; then
    echo "$ZERO_TOKEN_CHROME_USER_DATA_DIR"
    return
  fi

  case "$OS" in
    mac)
      echo "$HOME/Library/Application Support/ZeroToken-Chrome-Debug"
      ;;
    win)
      echo "${LOCALAPPDATA:-$HOME}/ZeroToken-Chrome-Debug"
      ;;
    wsl)
      local linux_dir="$HOME/.config/zero-token/chrome-debug"
      mkdir -p "$linux_dir"
      if [[ "$CHROME_PATH" == /mnt/*/*.exe ]] && command -v wslpath >/dev/null 2>&1; then
        wslpath -w "$linux_dir"
      else
        echo "$linux_dir"
      fi
      ;;
    *)
      echo "$HOME/.config/zero-token/chrome-debug"
      ;;
  esac
}

is_debug_chrome_running() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsS "http://127.0.0.1:$PORT/json/version" >/dev/null 2>&1
  else
    return 1
  fi
}

kill_existing_debug_chrome() {
  if pgrep -f "chrome.*remote-debugging-port=$PORT" >/dev/null 2>&1; then
    echo "Detected existing debug Chrome on port $PORT, stopping it..."
    pkill -f "chrome.*remote-debugging-port=$PORT" 2>/dev/null || true
    sleep 2
  fi
}

OS="$(detect_os)"
CHROME_PATH="$(detect_chrome)"

echo "OS: $OS"

if [[ -z "$CHROME_PATH" ]]; then
  echo "Chrome / Chromium not found."
  echo ""
  echo "Set CHROME_PATH manually, for example:"
  echo "  CHROME_PATH=\"/path/to/chrome\" ./scripts/start-chrome-debug.sh"
  exit 1
fi

USER_DATA_DIR="$(detect_user_data_dir)"
TMP_LOG="${TMPDIR:-/tmp}/zero-token-chrome-debug.log"

echo "Chrome: $CHROME_PATH"
echo "CDP port: $PORT"
echo "User data dir: $USER_DATA_DIR"
echo "Restart existing Chrome: $RESTART_CHROME"
echo "Open login pages: $OPEN_LOGIN_PAGES"
echo "Log: $TMP_LOG"
echo ""

if is_debug_chrome_running && [[ "$RESTART_CHROME" != "1" ]]; then
  echo "Existing Debug Chrome is already available."
  echo "Reusing: http://127.0.0.1:$PORT"
  echo ""
  echo "To force restart:"
  echo "  ZERO_TOKEN_RESTART_CHROME=1 npm run chrome:debug"
  echo ""
  if command -v curl >/dev/null 2>&1; then
    echo "CDP version:"
    curl -fsS "http://127.0.0.1:$PORT/json/version" | sed 's/,/,\n/g' | head -n 8 || true
    echo ""
  fi
  echo "Next for zero-video-factory:"
  echo "1. Log in to the provider tabs you want to use, such as Doubao, Qwen, Gemini, or GLM."
  echo "2. Keep this Chrome window running. ZeroToken connects to it through CDP."
  echo "3. Start the demo with: npm run dev:web"
  echo "4. Open: http://127.0.0.1:4317"
  echo ""
  echo "No OpenClaw onboard step is required in this project."
  echo "=========================================="
  exit 0
fi

if [[ "$RESTART_CHROME" == "1" ]]; then
  kill_existing_debug_chrome
elif pgrep -f "chrome.*remote-debugging-port=$PORT" >/dev/null 2>&1; then
  echo "A Chrome process with remote-debugging-port=$PORT was found, but CDP is not responding."
  echo "Not killing it by default."
  echo ""
  echo "To force restart:"
  echo "  ZERO_TOKEN_RESTART_CHROME=1 npm run chrome:debug"
  echo ""
  echo "Or inspect manually:"
  echo "  curl http://127.0.0.1:$PORT/json/version"
  exit 1
fi

echo "Starting Chrome debug mode..."
"$CHROME_PATH" \
  --remote-debugging-port="$PORT" \
  --user-data-dir="$USER_DATA_DIR" \
  --no-first-run \
  --no-default-browser-check \
  --disable-background-networking \
  --disable-sync \
  --disable-translate \
  --disable-features=TranslateUI \
  --remote-allow-origins=* \
  > "$TMP_LOG" 2>&1 &

CHROME_PID=$!

echo "Waiting for CDP endpoint..."
for _ in $(seq 1 20); do
  if is_debug_chrome_running; then
    break
  fi
  printf "."
  sleep 1
done
echo ""
echo ""

if ! is_debug_chrome_running; then
  echo "Chrome debug mode failed to start."
  echo "Check log: $TMP_LOG"
  exit 1
fi

echo "Chrome debug mode is ready."
echo "PID: $CHROME_PID"
echo "CDP: http://127.0.0.1:$PORT"
echo ""

if command -v curl >/dev/null 2>&1; then
  echo "CDP version:"
  curl -fsS "http://127.0.0.1:$PORT/json/version" | sed 's/,/,\n/g' | head -n 8 || true
  echo ""
fi

if [[ "$OPEN_LOGIN_PAGES" == "1" ]]; then
  echo "Opening Web provider login pages..."
  WEB_URLS=(
    "https://www.doubao.com/chat/"
    "https://chat.qwen.ai/"
    "https://chat2.qianwen.com/"
    "https://gemini.google.com/app"
    "https://chatglm.cn/"
    "https://chat.z.ai/"
    "https://chatgpt.com/"
    "https://claude.ai/new"
    "https://chat.deepseek.com/"
    "https://www.kimi.com/"
    "https://grok.com/"
    "https://www.perplexity.ai/"
  )
  for url in "${WEB_URLS[@]}"; do
    "$CHROME_PATH" \
      --remote-debugging-port="$PORT" \
      --user-data-dir="$USER_DATA_DIR" \
      "$url" \
      >/dev/null 2>&1 &
    sleep 0.3
  done
  echo "Login pages opened."
else
  echo "Skipping Web provider login pages."
fi

echo ""
echo "Next for zero-video-factory:"
echo "1. Log in to the provider tabs you want to use, such as Doubao, Qwen, Gemini, or GLM."
echo "2. Keep this Chrome window running. ZeroToken connects to it through CDP."
echo "3. Start the demo with: npm run dev:web"
echo "4. Open: http://127.0.0.1:4317"
echo ""
echo "No OpenClaw onboard step is required in this project."
echo ""
echo "Stop debug Chrome:"
echo "  pkill -f 'chrome.*remote-debugging-port=$PORT'"
echo "=========================================="
