package ir.phoenix.tunnel

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat

/**
 * PhoenixVpnService — سرویس اصلی VPN اندروید.
 * TUN را می‌سازد و fd آن را (با detachFd) به هسته‌ی Go می‌دهد.
 *
 * جلوگیری از حلقه: خود اپ با addDisallowedApplication از VPN بیرون است،
 * پس اتصال‌های هسته به سرور مستقیم از اینترنت واقعی می‌روند.
 * DNS: سیستم به 10.0.0.1 می‌فرستد و هسته از داخل تونل پاسخ می‌دهد.
 */
class PhoenixVpnService : VpnService() {

    companion object {
        const val ACTION_START = "ir.phoenix.tunnel.START"
        const val ACTION_STOP = "ir.phoenix.tunnel.STOP"
        const val EXTRA_CONFIG = "config_json"
        const val EXTRA_NAME = "server_name"
        private const val NOTIF_ID = 1001
        private const val CHANNEL_ID = "phoenix_vpn"

        fun start(ctx: Context, server: Server) {
            val i = Intent(ctx, PhoenixVpnService::class.java)
                .setAction(ACTION_START)
                .putExtra(EXTRA_CONFIG, server.config)
                .putExtra(EXTRA_NAME, server.name)
            ContextCompat.startForegroundService(ctx, i)
        }

        fun stop(ctx: Context) {
            if (!VpnState.isActive) return
            ctx.startService(Intent(ctx, PhoenixVpnService::class.java).setAction(ACTION_STOP))
        }
    }

    @Volatile private var userStopped = false
    private var coreThread: Thread? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            userStopped = true
            shutdown()
            return START_NOT_STICKY
        }
        // ACTION_START از اپ، یا android.net.VpnService از Always-on VPN سیستم
        val config = intent?.getStringExtra(EXTRA_CONFIG) ?: ServerStore.selected(this)?.config
        val name = intent?.getStringExtra(EXTRA_NAME) ?: ServerStore.selected(this)?.name ?: ""
        startAsForeground("در حال اتصال به $name…")
        if (config == null) {
            fail("سروری انتخاب نشده")
            stopForegroundCompat()
            stopSelf()
            return START_NOT_STICKY
        }
        if (VpnState.isActive) return START_STICKY
        userStopped = false
        VpnState.serverName = name
        VpnState.error = null
        VpnState.status = VpnState.Status.CONNECTING
        coreThread = Thread({ runTunnel(config, name) }, "phoenix-core").apply { start() }
        return START_STICKY
    }

    private fun runTunnel(config: String, name: String) {
        try {
            // ۱) تست واقعی سرور قبل از برقراری TUN (اینترنت گوشی قطع نمی‌شود اگر سرور خراب است)
            VpnState.pingMs = Core.ping(config)
            if (userStopped) return

            // ۲) ساخت رابط VPN
            val builder = Builder()
                .setSession("PhoenixTunnel · $name")
                .setMtu(1500)
                .addAddress("10.0.0.2", 32)
                .addRoute("0.0.0.0", 0)
                .addAddress("fd66:7068:6e78::2", 128)
                .addRoute("::", 0)
                .addDnsServer("10.0.0.1")
                .addDisallowedApplication(packageName) // سوکت‌های هسته از VPN رد نشوند
                .setConfigureIntent(mainIntent())
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) builder.setMetered(false)
            val pfd = builder.establish() ?: throw IllegalStateException("مجوز VPN داده نشده است")

            // ۳) مالکیت fd به Go؛ از این به بعد فقط هسته آن را می‌بندد
            val fd = pfd.detachFd()
            val id = Core.start(fd, config)
            if (userStopped) {
                Core.stop(id)
                return
            }
            VpnState.sessionId = id
            VpnState.connectedAt = System.currentTimeMillis()
            VpnState.status = VpnState.Status.CONNECTED
            updateNotification("متصل به $name")

            Core.await(id)
            if (!userStopped) fail("اتصال قطع شد")
        } catch (t: Throwable) {
            if (!userStopped) fail(friendlyError(t))
        } finally {
            VpnState.sessionId = -1
            if (VpnState.status != VpnState.Status.ERROR) VpnState.status = VpnState.Status.IDLE
            stopForegroundCompat()
            stopSelf()
        }
    }

    private fun friendlyError(t: Throwable): String {
        val m = t.message ?: t.javaClass.simpleName
        return when {
            m.contains("bad handshake", true) -> "کلید اشتباه است یا سرور پاسخ نمی‌دهد"
            m.contains("no ACCEPT", true) -> "سرور UDP پاسخ نمی‌دهد (احتمالاً UDP مسدود است)"
            m.contains("timeout", true) || m.contains("deadline", true) -> "سرور در دسترس نیست (timeout)"
            m.contains("refused", true) -> "پورت سرور بسته است"
            m.contains("no such host", true) -> "آدرس سرور پیدا نشد"
            else -> m
        }
    }

    private fun fail(msg: String) {
        VpnState.error = msg
        VpnState.status = VpnState.Status.ERROR
    }

    private fun shutdown() {
        val id = VpnState.sessionId
        if (id >= 0) Thread { Core.stop(id) }.start()
        VpnState.status = VpnState.Status.IDLE
        stopForegroundCompat()
        stopSelf()
    }

    override fun onRevoke() {
        // کاربر VPN دیگری را روشن کرد یا مجوز را برداشت
        userStopped = true
        shutdown()
    }

    override fun onDestroy() {
        val id = VpnState.sessionId
        if (id >= 0) Core.stop(id)
        super.onDestroy()
    }

    // ---------- اعلان ----------
    private fun mainIntent(): PendingIntent = PendingIntent.getActivity(
        this, 0, Intent(this, MainActivity::class.java),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
    )

    private fun buildNotification(text: String): Notification {
        createChannel()
        val stop = PendingIntent.getService(
            this, 1, Intent(this, PhoenixVpnService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("PhoenixTunnel")
            .setContentText(text)
            .setSmallIcon(R.drawable.ic_flame)
            .setColor(0xFFFF6A1A.toInt())
            .setContentIntent(mainIntent())
            .addAction(R.drawable.ic_power, "قطع", stop)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .build()
    }

    private fun startAsForeground(text: String) {
        val type = if (Build.VERSION.SDK_INT >= 34) ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE else 0
        ServiceCompat.startForeground(this, NOTIF_ID, buildNotification(text), type)
    }

    private fun stopForegroundCompat() {
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
    }

    private fun updateNotification(text: String) {
        val nm = getSystemService(NOTIFICATION_SERVICE) as NotificationManager
        try {
            nm.notify(NOTIF_ID, buildNotification(text))
        } catch (_: SecurityException) {
        }
    }

    private fun createChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val ch = NotificationChannel(CHANNEL_ID, "VPN", NotificationManager.IMPORTANCE_LOW)
            (getSystemService(NOTIFICATION_SERVICE) as NotificationManager).createNotificationChannel(ch)
        }
    }
}
