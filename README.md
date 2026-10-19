# 🔥 PhoenixTunnel (ققنوس)

تونل ضد-DPI و ضد فیلتر: **هسته‌ی سرور لینوکس** + **پنل وب** + **اپ اندروید**. هر سه از همین مخزن ساخته می‌شوند و با GitHub Actions بیلد می‌شوند.

## اتصال اپ به سرور چطور کار می‌کند؟

```
گوشی (اپ PhoenixTunnel)  ──Chameleon/TCP:443 یا PhoenixUDP:51820──▶  سرور لینوکس  ──▶  اینترنت
          ▲
          └── لینک phoenix://…  (یا QR)  ← سرور بعد از نصب می‌سازد / پنل نشان می‌دهد
```

۱. اسکریپت نصب را روی سرور اجرا می‌کنید. سرور یک **کلید تصادفی** می‌سازد و یک **لینک `phoenix://` + QR کد** چاپ می‌کند.
۲. در اپ روی «افزودن از کلیپ‌بورد» می‌زنید (یا QR را اسکن می‌کنید، یا روی لینک در تلگرام می‌زنید).
۳. همه‌ی تنظیمات (آدرس، پورت، کلید، SNIهای تقلبی، موتور) خودکار وارد می‌شود. دکمه‌ی بزرگ را می‌زنید و وصل می‌شوید.

لینک فقط یک JSON است که با base64 کد شده:
`{"server":"IP:443","keys":"…","engine":"chameleon","fake_snis":[…],"udp_server":"IP:51820"}`

## نصب سرور (یک دستور)

روی سرور Ubuntu/Debian/CentOS با کاربر root:

```bash
bash <(curl -fsSL https://github.com/kankiturk8770/phonix/releases/latest/download/install.sh)
```

اسکریپت نصب این کارها را انجام می‌دهد:
- باینری‌ها را نصب می‌کند و یک پروفایل با کلید تصادفی و گواهی می‌سازد.
- سرویس‌های systemd را راه می‌اندازد.
- پورت 53 را از systemd-resolved آزاد می‌کند تا موتور DNS بالا بیاید.
- فایروال را باز می‌کند و BBR را فعال می‌کند.
- در پایان **لینک اتصال + QR** و **آدرس و رمز پنل** را چاپ می‌کند.

گزینه‌ها: `--host <IP یا دامنه>`، `--port 443`، `--udp-port 51820`، `--panel-port 2053`، `--no-panel`، `--uninstall`، `--local file.tar.gz` (نصب آفلاین).

دستورات بعد از نصب:

| دستور | کار |
|---|---|
| `phoenix link` | نمایش دوباره‌ی لینک و QR |
| `phoenix status` | وضعیت سرویس‌ها |
| `phoenix logs` | لاگ زنده |
| `phoenix restart` | راه‌اندازی مجدد |
| `phoenix panel` | آدرس و رمز پنل |
| `phoenix passwd <رمز>` | تغییر رمز پنل |

پورت‌ها: `443/tcp` (Chameleon)، `8443/tcp` (Reality)، `51820/udp` (PhoenixUDP)، `53/udp+tcp` (PhantomDNS)، `2053/tcp` (پنل HTTPS).

## پنل وب

`https://IP:2053`. گواهی پنل self-signed است، پس در مرورگر «Advanced → Proceed» را بزنید.
- **این سرور:** وضعیت هسته، اتصال‌های فعال، ترافیک هر موتور، و لینک + QR برای اپ.
- **نودها:** سرورهای دیگری که با `phoenix-agent` به این پنل وصل می‌شوند. برای هر نود لینک، QR و دستور نصب ایجنت (با pin گواهی) نمایش داده می‌شود.
- **تنظیمات:** تغییر رمز و افزودن کاربر.

## اپ اندروید

فایل `PhoenixTunnel.apk` را از [Releases](https://github.com/kankiturk8770/phonix/releases/latest) نصب کنید (اندروید ۷ به بالا).
- افزودن سرور: Paste لینک، اسکن QR، یا باز کردن لینک `phoenix://` از هر اپی.
- انتخاب موتور: **Chameleon (TCP)** که پیش‌فرض و پیشنهادی است، یا **PhoenixUDP** که آزمایشی است.
- همه‌ی ترافیک TCP گوشی و DNS از داخل تونل می‌رود. QUIC (UDP:443) مسدود می‌شود تا مرورگرها به TCP داخل تونل برگردند.
- دکمه‌ی «تست»: یک اتصال واقعی به سرور می‌زند و زمان آن را نشان می‌دهد.
- کاشی Quick Settings برای روشن/خاموش کردن سریع.
- Always-on VPN سیستم پشتیبانی می‌شود.

## ساختار مخزن

```
core/        موتور مشترک: رمزنگاری، قاب‌بندی، Chameleon-SNI، FEC، fallback، لینک phoenix://
server/      موتورهای سرور: chameleon، reality، phoenix-udp، phantom (DNS)
client/      کلاینت‌ها + VPN (پشته‌ی gVisor netstack) + پاسخ‌گوی DNS داخل تونل
mobile/      رابط gomobile برای اندروید (ir.phoenix.core.mobile.Mobile)
panel/       پنل وب (API + داشبورد فارسی تعبیه‌شده)
cmd/         phoenix-server / phoenix-panel / phoenix-agent
deploy/      install.sh
android-app/ اپ Kotlin
```

## بیلد

همه چیز در GitHub Actions بیلد می‌شود (`.github/workflows/build.yml`). خروجی‌ها در Release قرار می‌گیرند:
- `phoenix-linux-amd64.tar.gz` و `phoenix-linux-arm64.tar.gz`
- `PhoenixTunnel.apk`
- `install.sh`

بیلد دستی:

```bash
go test ./...
go build -o bin/ ./cmd/...

# اندروید
gomobile bind -target=android -androidapi 24 -javapkg ir.phoenix.core -o android-app/app/libs/phoenix.aar ./mobile
cd android-app && ./gradlew assembleRelease
```

### امضای ثابت APK (اختیاری)

اگر این Secretها را در GitHub تعریف کنید، APK با کلید شما امضا می‌شود و آپدیت‌ها بدون حذف اپ نصب می‌شوند:
- `ANDROID_KEYSTORE_BASE64`: خروجی `base64 -w0 release.jks`
- `ANDROID_KEYSTORE_PASSWORD`
- `ANDROID_KEY_ALIAS`
- `ANDROID_KEY_PASSWORD`

اگر این Secretها نباشند، APK با کلید debug امضا می‌شود. در این حالت برای نصب نسخه‌ی جدید باید اول نسخه‌ی قبلی را حذف کنید.
