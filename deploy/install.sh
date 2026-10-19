#!/usr/bin/env bash
# PhoenixTunnel — نصب یک‌دستوری روی سرور لینوکس (Ubuntu/Debian/CentOS/Alma/Rocky)
#
#   bash <(curl -fsSL https://github.com/kankiturk8770/phonix/releases/latest/download/install.sh)
#
# گزینه‌ها:
#   --host <IP|domain>   آدرس عمومی سرور (پیش‌فرض: تشخیص خودکار)
#   --port <n>           پورت TCP موتور chameleon (پیش‌فرض 443)
#   --udp-port <n>       پورت موتور phoenix-udp (پیش‌فرض 51820)
#   --panel-port <n>     پورت پنل وب HTTPS (پیش‌فرض 2053)
#   --no-panel           بدون پنل وب
#   --agent --panel <URL> --token <T> --node <ID> [--pin <sha256>]
#                        نصب به‌عنوان نود (ایجنت) که از پنل دیگری کنترل می‌شود
#   --local <file.tar.gz> نصب آفلاین از فایل دانلودشده
#   --uninstall          حذف کامل
set -euo pipefail

REPO="kankiturk8770/phonix"
BIN_DIR="/usr/local/bin"
CONF_DIR="/etc/phoenix"

HOST=""; PORT=443; UDP_PORT=51820; PANEL_PORT=2053; WITH_PANEL=1
AGENT=0; PANEL_URL=""; TOKEN=""; NODE=""; PIN=""; LOCAL_TGZ=""; UNINSTALL=0

red(){ printf '\033[1;31m%s\033[0m\n' "$*"; }
grn(){ printf '\033[1;32m%s\033[0m\n' "$*"; }
ylw(){ printf '\033[1;33m%s\033[0m\n' "$*"; }
org(){ printf '\033[38;5;208m%s\033[0m\n' "$*"; }
die(){ red "✖ $*"; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --host) HOST="$2"; shift 2;;
    --port) PORT="$2"; shift 2;;
    --udp-port) UDP_PORT="$2"; shift 2;;
    --panel-port) PANEL_PORT="$2"; shift 2;;
    --no-panel) WITH_PANEL=0; shift;;
    --agent) AGENT=1; shift;;
    --panel) PANEL_URL="$2"; shift 2;;
    --token) TOKEN="$2"; shift 2;;
    --node) NODE="$2"; shift 2;;
    --pin) PIN="$2"; shift 2;;
    --local) LOCAL_TGZ="$2"; shift 2;;
    --uninstall) UNINSTALL=1; shift;;
    -h|--help) sed -n '2,20p' "$0"; exit 0;;
    *) die "گزینه‌ی ناشناخته: $1";;
  esac
done

[ "$(id -u)" = 0 ] || die "با کاربر root اجرا کنید (sudo -i)"
command -v systemctl >/dev/null || die "systemd لازم است"

org '
   ╔═══════════════════════════════════════╗
   ║   🔥  PhoenixTunnel — نصب سرور        ║
   ╚═══════════════════════════════════════╝'

# ---------------- حذف ----------------
if [ "$UNINSTALL" = 1 ]; then
  for s in phoenix phoenix-panel phoenix-agent; do
    systemctl disable --now "$s" 2>/dev/null || true
    rm -f "/etc/systemd/system/$s.service"
  done
  systemctl daemon-reload
  rm -f "$BIN_DIR"/phoenix-server "$BIN_DIR"/phoenix-panel "$BIN_DIR"/phoenix-agent "$BIN_DIR"/phoenix
  ylw "فایل‌های تنظیمات در $CONF_DIR و /var/lib/phoenix* نگه داشته شدند (برای حذف: rm -rf $CONF_DIR /var/lib/phoenix*)"
  grn "✔ حذف شد"
  exit 0
fi

# ---------------- پیش‌نیازها ----------------
need(){ command -v "$1" >/dev/null 2>&1; }
if ! need curl || ! need tar; then
  ylw "• نصب curl/tar ..."
  if need apt-get; then apt-get update -qq && apt-get install -y -qq curl tar >/dev/null
  elif need dnf; then dnf install -y -q curl tar
  elif need yum; then yum install -y -q curl tar
  else die "curl و tar را نصب کنید"; fi
fi

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64;;
  aarch64|arm64) ARCH=arm64;;
  *) die "معماری پشتیبانی نمی‌شود: $(uname -m)";;
esac

# ---------------- دریافت باینری‌ها ----------------
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo /nonexistent)"
if [ -n "$LOCAL_TGZ" ]; then
  tar -xzf "$LOCAL_TGZ" -C "$TMP"
  SRC="$(dirname "$(find "$TMP" -name phoenix-server -type f | head -1)")"
elif [ -x "$SELF_DIR/bin/phoenix-server" ]; then
  SRC="$SELF_DIR/bin"
else
  URL="https://github.com/$REPO/releases/latest/download/phoenix-linux-$ARCH.tar.gz"
  ylw "• دانلود $URL"
  curl -fL --retry 3 -o "$TMP/p.tgz" "$URL" || die "دانلود ناموفق بود. فایل را دستی دانلود و با --local نصب کنید."
  tar -xzf "$TMP/p.tgz" -C "$TMP"
  SRC="$(dirname "$(find "$TMP" -name phoenix-server -type f | head -1)")"
fi
[ -x "$SRC/phoenix-server" ] || die "باینری phoenix-server پیدا نشد"

for s in phoenix phoenix-panel phoenix-agent; do systemctl stop "$s" 2>/dev/null || true; done
install -m 755 "$SRC/phoenix-server" "$SRC/phoenix-panel" "$SRC/phoenix-agent" "$BIN_DIR/"
grn "✔ باینری‌ها نصب شدند ($("$BIN_DIR/phoenix-server" -version))"

# ---------------- بهینه‌سازی شبکه ----------------
cat > /etc/sysctl.d/99-phoenix.conf <<'EOS'
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
net.core.rmem_max=16777216
net.core.wmem_max=16777216
net.ipv4.tcp_fastopen=3
net.ipv4.ip_local_port_range=10000 65000
fs.file-max=1048576
EOS
sysctl -q --system >/dev/null 2>&1 || true

open_port(){ # $1=port $2=proto
  if need ufw && ufw status 2>/dev/null | grep -q "Status: active"; then ufw allow "$1/$2" >/dev/null || true; fi
  if need firewall-cmd && firewall-cmd --state >/dev/null 2>&1; then firewall-cmd -q --permanent --add-port="$1/$2" || true; fi
}
reload_fw(){ if need firewall-cmd && firewall-cmd --state >/dev/null 2>&1; then firewall-cmd -q --reload || true; fi; }

port_busy(){ # $1=port $2=tcp|udp
  local flag="-ltn"; [ "$2" = udp ] && flag="-lun"
  need ss && ss -H $flag "sport = :$1" 2>/dev/null | grep -q .
}

# آزاد کردن پورت 53 از systemd-resolved تا موتور DNS (phantom) بالا بیاید
free_dns_port(){
  if port_busy 53 udp && systemctl is-active --quiet systemd-resolved; then
    ylw "• پورت 53 دست systemd-resolved است — آزادسازی (DNS سیستم کار می‌کند)"
    mkdir -p /etc/systemd/resolved.conf.d
    printf '[Resolve]\nDNSStubListener=no\n' > /etc/systemd/resolved.conf.d/phoenix.conf
    if [ -f /run/systemd/resolve/resolv.conf ]; then
      ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf
    fi
    systemctl restart systemd-resolved || true
    sleep 1
  fi
  if port_busy 53 udp; then
    ylw "⚠ پورت 53 هنوز توسط برنامه‌ی دیگری اشغال است؛ فقط موتور phantom (DNS) غیرفعال می‌ماند. بقیه کار می‌کنند."
  fi
}

# ---------------- حالت ایجنت ----------------
if [ "$AGENT" = 1 ]; then
  [ -n "$PANEL_URL" ] && [ -n "$TOKEN" ] && [ -n "$NODE" ] || die "--panel و --token و --node لازم‌اند"
  mkdir -p "$CONF_DIR" /var/lib/phoenix-agent
  umask 077
  cat > "$CONF_DIR/agent.env" <<EOF
PANEL_URL=$PANEL_URL
AGENT_TOKEN=$TOKEN
NODE_ID=$NODE
AGENT_PIN=$PIN
EOF
  cat > /etc/systemd/system/phoenix-agent.service <<'EOF'
[Unit]
Description=PhoenixTunnel Node Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/phoenix/agent.env
ExecStart=/usr/local/bin/phoenix-agent -panel ${PANEL_URL} -token ${AGENT_TOKEN} -node ${NODE_ID} -pin "${AGENT_PIN}" -workdir /var/lib/phoenix-agent
Restart=always
RestartSec=5
LimitNOFILE=1048576
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/phoenix-agent
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
  free_dns_port
  for p in 443 8443; do open_port $p tcp; done
  open_port 51820 udp; open_port 53 udp; reload_fw
  systemctl daemon-reload
  systemctl enable --now phoenix-agent >/dev/null
  sleep 3
  if systemctl is-active --quiet phoenix-agent; then
    grn "✔ ایجنت اجرا شد و به پنل وصل است. لینک اتصال اپ را از پنل بگیرید."
  else
    red "✖ ایجنت اجرا نشد:"; journalctl -u phoenix-agent -n 20 --no-pager
  fi
  exit 0
fi

# ---------------- حالت سرور (پیش‌فرض) ----------------
if [ -z "$HOST" ]; then
  HOST="$(curl -4 -fsS --max-time 6 https://api.ipify.org 2>/dev/null || curl -4 -fsS --max-time 6 https://ifconfig.me 2>/dev/null || true)"
  [ -n "$HOST" ] || HOST="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}')"
  [ -n "$HOST" ] || die "IP عمومی پیدا نشد؛ با --host <IP> اجرا کنید"
fi
grn "✔ آدرس عمومی سرور: $HOST"

if [ ! -f "$CONF_DIR/profile.json" ] && port_busy "$PORT" tcp; then
  die "پورت $PORT/tcp اشغال است (مثلاً nginx). با --port <پورت دیگر> اجرا کنید."
fi

mkdir -p "$CONF_DIR" /var/lib/phoenix
chmod 700 "$CONF_DIR"
"$BIN_DIR/phoenix-server" -init -config "$CONF_DIR/profile.json" -host "$HOST" \
  -name "Phoenix-$(hostname -s 2>/dev/null || echo server)" -port "$PORT" -udp-port "$UDP_PORT" >/dev/null
grn "✔ پروفایل و کلید: $CONF_DIR/profile.json"

free_dns_port

cat > /etc/systemd/system/phoenix.service <<'EOF'
[Unit]
Description=PhoenixTunnel Core Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/etc/phoenix
ExecStart=/usr/local/bin/phoenix-server -config /etc/phoenix/profile.json -stats 127.0.0.1:9090
Restart=always
RestartSec=3
LimitNOFILE=1048576
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/etc/phoenix /var/lib/phoenix
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

CPORT="$(grep -o '"0.0.0.0:[0-9]*"' "$CONF_DIR/profile.json" | head -1 | tr -dc 0-9)"
open_port "${CPORT:-$PORT}" tcp; open_port 8443 tcp
open_port "$UDP_PORT" udp; open_port 53 udp; open_port 53 tcp

PANEL_INFO=""
if [ "$WITH_PANEL" = 1 ]; then
  mkdir -p /var/lib/phoenix-panel
  CRED="$CONF_DIR/panel-credentials.txt"
  if [ ! -f "$CRED" ]; then
    PASS="$(head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 16)"
    "$BIN_DIR/phoenix-panel" -data /var/lib/phoenix-panel -reset-admin -admin-user admin -admin-pass "$PASS" >/dev/null 2>&1
    umask 077; printf 'user=admin\npassword=%s\n' "$PASS" > "$CRED"
  fi
  PASS="$(sed -n 's/^password=//p' "$CRED")"
  echo "PANEL_LISTEN=0.0.0.0:$PANEL_PORT" > "$CONF_DIR/panel.env"
  cat > /etc/systemd/system/phoenix-panel.service <<'EOF'
[Unit]
Description=PhoenixTunnel Control Panel
After=network-online.target phoenix.service
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/phoenix/panel.env
ExecStart=/usr/local/bin/phoenix-panel -listen ${PANEL_LISTEN} -data /var/lib/phoenix-panel -local-profile /etc/phoenix/profile.json -local-stats http://127.0.0.1:9090/stats -tls-cert /etc/phoenix/certs/server.crt -tls-key /etc/phoenix/certs/server.key
Restart=always
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadOnlyPaths=/etc/phoenix
ReadWritePaths=/var/lib/phoenix-panel
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
  open_port "$PANEL_PORT" tcp
  PANEL_INFO="https://$HOST:$PANEL_PORT   (user: admin   pass: $PASS)"
fi
reload_fw

# ابزار مدیریت کوتاه
cat > "$BIN_DIR/phoenix" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  link)    phoenix-server -link -config /etc/phoenix/profile.json ;;
  status)  systemctl --no-pager status phoenix phoenix-panel 2>/dev/null | grep -E '●|Active' ;;
  restart) systemctl restart phoenix; systemctl restart phoenix-panel 2>/dev/null; echo ok ;;
  logs)    journalctl -u phoenix -n 100 -f ;;
  panel)   cat /etc/phoenix/panel-credentials.txt 2>/dev/null; grep PANEL_LISTEN /etc/phoenix/panel.env 2>/dev/null ;;
  passwd)  [ -n "$2" ] || { echo "phoenix passwd <new-password>"; exit 1; }
           systemctl stop phoenix-panel; phoenix-panel -data /var/lib/phoenix-panel -reset-admin -admin-user admin -admin-pass "$2" &&
           sed -i "s/^password=.*/password=$2/" /etc/phoenix/panel-credentials.txt; systemctl start phoenix-panel ;;
  uninstall) bash <(curl -fsSL https://github.com/kankiturk8770/phonix/releases/latest/download/install.sh) --uninstall ;;
  *) echo "phoenix link|status|restart|logs|panel|passwd <pw>|uninstall" ;;
esac
EOF
chmod 755 "$BIN_DIR/phoenix"

systemctl daemon-reload
systemctl enable --now phoenix >/dev/null
[ "$WITH_PANEL" = 1 ] && systemctl enable --now phoenix-panel >/dev/null
sleep 2
systemctl restart phoenix
[ "$WITH_PANEL" = 1 ] && systemctl restart phoenix-panel
sleep 2

if ! systemctl is-active --quiet phoenix; then
  red "✖ سرویس phoenix اجرا نشد:"; journalctl -u phoenix -n 30 --no-pager; exit 1
fi
journalctl -u phoenix -n 20 --no-pager | grep -E 'ENGINE' | sed 's/^.*\[ENGINE\]/   •/' || true

echo
org "══════════════════ ✅ نصب کامل شد ══════════════════"
"$BIN_DIR/phoenix-server" -link -config "$CONF_DIR/profile.json"
if [ -n "$PANEL_INFO" ]; then
  grn "🌐 پنل مدیریت: $PANEL_INFO"
  ylw "   (گواهی پنل self-signed است؛ در مرورگر «Advanced → Proceed» را بزنید)"
fi
echo
grn "دستورات: phoenix link | phoenix status | phoenix logs | phoenix restart | phoenix panel"
