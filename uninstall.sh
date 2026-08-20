#!/usr/bin/env bash
# tinyclue 卸载脚本：移除已安装的二进制与用户数据。
# 用法:
#   ./uninstall.sh                  交互式，逐项询问（默认保留）
#   ./uninstall.sh -y               不询问，全量删除（配置先备份为 .config.bak.<时间戳>）
#   ./uninstall.sh --keep-data      只删二进制与缓存，保留配置/会话/任务/计划/工具结果
# 对应 install.sh：PREFIX 与 TINYCLUE_CONFIG_DIR 语义一致。
set -euo pipefail

PREFIX="${PREFIX:-/usr/local}"
BINDIR="$PREFIX/bin"
BIN="$BINDIR/tinyclue"
# 配置根目录：$TINYCLUE_CONFIG_DIR 或 ~/.tinyclue（与 install.sh / 运行时一致）。
CONFIG_HOME="${TINYCLUE_CONFIG_DIR:-$HOME/.tinyclue}"

YES=0
KEEP_DATA=0
case "${1:-}" in
  -y|--yes) YES=1 ;;
  --keep-data) KEEP_DATA=1 ;;
  "") ;;
  *) echo "未知参数: ${1:-}（支持 -y/--yes、--keep-data）"; exit 1 ;;
esac

# 1. 移除已安装二进制。
if [[ -e "$BIN" ]]; then
  echo "移除二进制: $BIN"
  if [[ -w "$BINDIR" ]]; then
    rm -f "$BIN"
  else
    sudo rm -f "$BIN"   # 与 install.sh 一致：/usr/local/bin 需要提权
  fi
else
  echo "二进制不存在（跳过）: $BIN"
fi

# 2. 清理仓库内开发构建产物（make service 产物）。
if [[ -e bin/tinyclue ]]; then
  echo "移除仓库内构建产物: $(pwd)/bin/tinyclue"
  rm -f bin/tinyclue
fi

# 3. 用户数据（$CONFIG_HOME）。
if [[ ! -d "$CONFIG_HOME" ]]; then
  echo "配置目录不存在（跳过）: $CONFIG_HOME"
  exit 0
fi

# config/ 含 auth.json（API Key），删除前先整体备份为 .bak，可随时恢复。
remove_config() {
  [[ -d "$CONFIG_HOME/config" ]] || { echo "配置目录 config/ 不存在（跳过）"; return; }
  local bak="${CONFIG_HOME}.config.bak.$(date +%Y%m%d%H%M%S)"
  mv "$CONFIG_HOME/config" "$bak"
  echo "配置已备份: ${bak}（确认无需后可手动删除）"
}

# caches/ 仅是 model-cache.json，可联网重新生成，直接删。
remove_caches() {
  [[ -d "$CONFIG_HOME/caches" ]] || return
  rm -rf "$CONFIG_HOME/caches"
  echo "已删除缓存: $CONFIG_HOME/caches"
}

# 会话/任务/计划/工具结果：用户运行产生的数据。
# 会话现按项目分目录存于 projects/（旧平铺 sessions/ 一并清理）。
remove_data() {
  for d in sessions projects tasks plans tool-results; do
    if [[ -d "$CONFIG_HOME/$d" ]]; then
      rm -rf "$CONFIG_HOME/$d"
      echo "已删除 $d: $CONFIG_HOME/$d"
    fi
  done
}

if [[ $KEEP_DATA -eq 1 ]]; then
  remove_caches
elif [[ $YES -eq 1 ]]; then
  remove_config
  remove_caches
  remove_data
else
  ans=""
  read -r -p "删除配置（含 auth.json 的 API Key，先备份为 .bak）？[y/N] " ans || true
  case "$ans" in [yY]*) remove_config ;; *) echo "保留配置" ;; esac

  ans=""
  read -r -p "删除缓存 caches/（可重新生成）？[y/N] " ans || true
  case "$ans" in [yY]*) remove_caches ;; *) echo "保留缓存" ;; esac

  ans=""
  read -r -p "删除会话/任务/计划/工具结果（sessions projects tasks plans tool-results）？[y/N] " ans || true
  case "$ans" in [yY]*) remove_data ;; *) echo "保留会话/任务/计划数据" ;; esac
fi

# 配置根目录若已空，顺手移除（rmdir 只删空目录，安全）。
rmdir "$CONFIG_HOME" 2>/dev/null || true

echo "✔ 卸载完成"
