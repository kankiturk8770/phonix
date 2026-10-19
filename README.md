# 🐦‍🔥 PhoenixTunnel — هسته سرور + پنل کنترل

پلتفرم ضد-DPI و دور زدن فیلترینگ برای سخت‌ترین شبکه‌های دنیا.
این مخزن شامل **هسته لینوکس سرور**، **ایجنت نود**، و **پنل فوق پیشرفته کنترل** است.
اپلیکیشن اندروید در مخزن جدا (`phoenix-android`) — این اسکلت دقیقاً APIهایی که پنل و هسته اینجا expose می‌کنند را مصرف می‌کند.

## ساختار
```
core/      موتور مشترک: engine registry، رمزنگاری قاب، Chameleon-SNI، HydraMUX، PhantomDNS، fallback
server/    دیمون سرور لینوکس + فایل systemd
panel/     پنل کنترل: API کامل + داشبورد وب فارسی تعبیه‌شده (single-binary)
cmd/       سه باینری: phoenix-server / phoenix-agent / phoenix-panel
```

## سه باینری
| باینری | نقش |
|---|---|
| `phoenix-server` | اجرای مستقیم هسته با پروفایل JSON |
| `phoenix-agent` | نصب روی هر نود؛ پروفایل را از پنل می‌گیرد، هسته را اجرا می‌کند، آمار می‌فرستد، فرمان قبول می‌کند |
| `phoenix-panel` | پنل وب (داشبورد فارسی RTL) + API کامل |

## استقرار سرور
```bash
sudo install -m755 bin/phoenix-server /usr/local/bin/
sudo install -m644 server/systemd/phoenix.service /etc/systemd/system/
sudo mkdir -p /etc/phoenix /var/lib/phoenix
sudo cp config.example.json /etc/phoenix/profile.json   # ویرایش: کلید، دامنه، SNIهای تقلبی
sudo systemctl enable --now phoenix
```

## استقرار پنل
```bash
./bin/phoenix-panel -listen 0.0.0.0:8080 -data /var/lib/phoenix-panel \
    -admin-user admin -admin-pass 'رمز-قوی'
```
سپس در مرورگر: `http://IP:8080` — ورود، ساخت نود، دریافت `agent_token`.

## اتصال ایجنت نود به پنل
```bash
./bin/phoenix-agent -panel https://panel.example.com:8080 \
    -token <agent_token از پنل> -node <node_id>
```

## جریان کنترل
```
پنل → (ساخت نود/اینباند، تعویض موتور، فرمان)
ایجنت → دریافت پروفایل، اجرای هسته، گزارش آمار هر ۱۰ ثانیه، دریافت فرمان سوئیچ موتور
fallback داخل هسته → پروب هر ۳ ثانیه، تشخیص RST/timeout/TLS-alert، سوئیچ خودکار chameleon → hydra → phantom
```

## پروتکل‌های پیاده‌شده در هسته (همه با تست سرتاسری)

| پروتکل | توضیح | تست |
|---|---|---|
| **Chameleon-TLS** | handshake احراز هویت، قاب رمزنگاری‌شده، cover traffic، early-silence | ✅ e2e (داده رد و بدل می‌شود) |
| **Reality-Lite** | پروبر/مرورگر ناشناس → پروکسی خام به سایت استتار؛ کلاینت PHX1 → تونل | ✅ camouflage test |
| **PhoenixUDP** | پروتکل اختصاصی UDP: HELLO/ACCEPT با HMAC، ARQ بازارسال تطبیقی، padding ضد تشخیص | ✅ e2e (اکو برمی‌گردد) |
| **PhantomDNS** | سر DNS کامل (وایرفرمت بدون وابستگی)، DGA چرخان، پاسخ TXT، camouflage با forward به upstream برای دامنه‌های خارجی | ✅ e2e |
| **HydraMUX** | مالتی‌پلکسر round-robin وزن‌دار بین کانال‌ها | ✅ |
| **FEC** | XOR FEC با پوشش کامل هر گم‌شدگی تکی در گروه | ✅ unit |
| **FallbackEngine** | پروب هر ۳ ثانیه، پنجره‌ی لغزان، تشخیص RST/timeout/TLS-alert، سوئیچ خودکار موتور | ✅ |

لایه رمزنگاری مشترک: handshake کوتاه چالش-پاسخ، AES-GCM با nonce صریح، anti-replay، padding حداقلی ۱۲۸ بایت.

## تست‌ها
```bash
go test ./...
# server: TestChameleonEndToEnd, TestRealityCamouflage, TestPhantomDNSQuery,
#         TestPhantomUpstreamCamouflage, TestPhoenixUDPEndToEnd
# core:   TestFECRecoverSingleLoss, TestTargetCodec
```

## بیلد از سورس
```bash
go build -o bin/ ./...
```

## کلاینت و اندروید (client/ + android-app/)

پکیج `client` کلاینت‌های کامل است — همه با تست سرتاسری در برابر سرور واقعی:

| کلاینت | توضیح | تست |
|---|---|---|
| DialChameleon | early-silence + decoy SNI + handshake + تونل | ✅ |
| DialPhoenixUDP | HELLO/ACCEPT + ARQ + مرتب‌سازی seq | ✅ |
| SOCKS5 سرور محلی | هر CONNECT → تونل | ✅ |
| tuntcp پشته TCP/IPv4 | TUN↔TCP کامل (بدون وابستگی) | ✅ |
| VPN glue | TUN → stack → dialer → تونل | ✅ e2e کامل |
| mobile.go | API گو موبایل: StartVPN/StopVPN/WaitVPN | — |

### بیلد AAR برای اندروید
```bash
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
gomobile bind -target=android -javapkg ir.phoenix.core \
    -o android-app/app/libs/phoenix.aar \
    github.com/phoenixtunnel/phoenix/client
```

### اپ اندروید (android-app/)
اپ کامل Kotlin: `PhoenixVpnService` (VpnService + TUN + foreground service)،
`MainActivity` (ورود سرور/کلید + درخواست مجوز VPN)، پل `Phoenix.kt` به AAR.
بیلد در Android Studio یا `./gradlew assembleDebug`.

## نقشه‌ی راه بعدی
1. ProtectCallback واقعی در gomobile (VpnService.protect برای هر سوکت)
2. DNS سمت برگشت PhantomDNS (پاسخ poll از سرور به کلاینت)
3. Hysteria2 + WebRTC کانال چهارم HydraMUX
4. Mimicry Rotation Engine + بانک fingeprint
5. پنل: ساخت کانفیگ کلاینت + QR
