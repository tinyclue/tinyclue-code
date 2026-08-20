#!/usr/bin/env bash
# tinyclue 安装脚本：编译并安装二进制到 PATH，初始化配置文件，并种子 model 缓存到 ~/.tinyclue/caches。
# 默认配置为 opencode 免费模型 deepseek-v4-flash-free（cost 全 0，走 opencode zen 网关；
# auth.json 种子共享匿名 key "public"，装完即用，按 IP 限流，可到登录面板换自己的 key）。
# 已存在的 auth.json/settings.json/slog.json 会逐个询问是否覆盖（覆盖前备份为 .bak）；
# -y 跳过询问直接覆盖。文件不存在则直接初始化，不询问。
# 用法: ./install.sh [-y]   （默认装到 /usr/local/bin/tinyclue；可用 PREFIX=... 覆盖）
set -euo pipefail

PREFIX="${PREFIX:-/usr/local}"
BINDIR="$PREFIX/bin"
# 配置根目录：$TINYCLUE_CONFIG_DIR 或 ~/.tinyclue（与运行时 config.TinyClueDir 语义一致）。
CONFIG_HOME="${TINYCLUE_CONFIG_DIR:-$HOME/.tinyclue}"
CONFIG_DIR="$CONFIG_HOME/config"
cd "$(dirname "$0")"

# -y: 配置文件已存在时不询问，直接覆盖。
YES=0
case "${1:-}" in
  -y|--yes) YES=1 ;;
  "") ;;
  *) echo "未知参数: ${1:-}（支持 -y/--yes）"; exit 1 ;;
esac

# 1. 编译并安装二进制。
if [[ -w "$BINDIR" ]]; then
  go build -o "$BINDIR/tinyclue" ./cmd
else
  go build -o bin/tinyclue ./cmd
  sudo install -m 0755 bin/tinyclue "$BINDIR/tinyclue"
fi

# 2. 初始化配置文件：不存在的直接写入默认值；已存在的询问是否覆盖（覆盖前备份为 .bak）。
mkdir -p "$CONFIG_DIR"

# should_write <file>：返回 0=写入，1=跳过。文件不存在直接写入；存在时询问（-y 自动确认覆盖）。
should_write() {
  local f="$1"
  [[ -e "$CONFIG_DIR/$f" ]] || return 0
  if [[ $YES -eq 1 ]]; then
    return 0
  fi
  local ans=""
  read -r -p "文件已存在，是否覆盖 ${f}（原文件备份为 ${f}.bak）？[y/N] " ans || true
  case "$ans" in
    [yY]*) return 0 ;;
    *) echo "保留现有 $f"; return 1 ;;
  esac
}

if should_write auth.json; then
  [[ -e "$CONFIG_DIR/auth.json" ]] && mv -f "$CONFIG_DIR/auth.json" "$CONFIG_DIR/auth.json.bak"
  cat > "$CONFIG_DIR/auth.json" <<'EOF'
{
  "opencode": {
    "type": "api-key",
    "key": "public"
  }
}
EOF
fi

if should_write settings.json; then
  [[ -e "$CONFIG_DIR/settings.json" ]] && mv -f "$CONFIG_DIR/settings.json" "$CONFIG_DIR/settings.json.bak"
  cat > "$CONFIG_DIR/settings.json" <<'EOF'
{
  "defaultProvider": "opencode",
  "defaultModel": "deepseek-v4-flash-free"
}
EOF
fi

if should_write slog.json; then
  [[ -e "$CONFIG_DIR/slog.json" ]] && mv -f "$CONFIG_DIR/slog.json" "$CONFIG_DIR/slog.json.bak"
  cat > "$CONFIG_DIR/slog.json" <<'EOF'
{
  "enable": true,
  "log_path": "/tmp",
  "file_name": "tinyclue.log",
  "max_size": 104857600,
  "backups": 7,
  "level": "error"
}
EOF
fi
echo "✔ 已初始化配置: ${CONFIG_DIR}"

# 3. 种子 model 缓存（仅缓存不存在时，避免覆盖用户刷新过的版本）。
# 种子源为仓库内 .tinyclue/caches/model-cache.json，目标为配置根目录下 caches/。
CACHE_DIR="$CONFIG_HOME/caches"
if [[ -f .tinyclue/caches/model-cache.json ]] && ! [[ -f "$CACHE_DIR/model-cache.json" ]]; then
  mkdir -p "$CACHE_DIR"
  cp .tinyclue/caches/model-cache.json "$CACHE_DIR/model-cache.json"
  echo "✔ 已种子缓存: $CACHE_DIR/model-cache.json"
fi

echo "✔ 已安装: $BINDIR/tinyclue"
echo "  配置目录: $CONFIG_DIR"
