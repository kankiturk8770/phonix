package ir.phoenix.tunnel

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import androidx.core.app.NotificationCompat
import java.util.concurrent.atomic.AtomicBoolean

/**
 * PhoenixVpnService — سرویس اصلی VPN اندروید.
 * TUN fd را می‌سازد و به هسته‌ی Go (libphoenix.so از طریق gomobile AAR) می‌دهد.
 */
class PhoenixVpnService : VpnService() {

    companion object {
        const val ACTION_START = "ir.phoenix.tunnel.START"
        const val ACTION_STOP = "ir.phoenix.tunnel.STOP"
        const val EXTRA_CONFIG = "config_json"
        const val NOTIF_ID = 1001
        const val CHANNEL_ID = "phoenix_vpn"
        val isRunning = AtomicBoolean(false)
        var sessionId: Int = -1
            private set
    }

    private var tunFd: ParcelFileDescriptor? = null
    private var coreThread: Thread? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> { stopSelf(); return START_NOT_STICKY }
            else -> {
                val config = intent?.getStringExtra(EXTRA_CONFIG) ?: return START_NOT_STICKY
                startForeground(NOTIF_ID, buildNotification("در حال اتصال…"))
                startTunnel(config)
            }
        }
        return START_STICKY
    }

    private fun startTunnel(configJson: String) {
        if (isRunning.get()) return
        try {
            val builder = Builder()
                .setSession("PhoenixTunnel")
                .setMtu(1500)
                .addAddress("10.0.0.2", 32)
                .addDnsServer("10.0.0.1")
                .addRoute("0.0.0.0", 0)
                // split tunneling از پنل (اختیاری):
                // builder.addAllowedApplication("org.telegram.messenger")
            tunFd?.close()
            tunFd = builder.establish()
                ?: throw IllegalStateException("VpnService.establish() returned null")

            val fd = tunFd!!.fd

            // ضروری: سوکت‌های هسته باید از VPN بگذرند تا حلقه ایجاد نشود.
            // در نسخه‌ی کامل، هسته قبل از هر dial از طریق ProtectCallback
            // fd سوکت را برای protect می‌دهد (مانند VpnService.protect).
            protectCoreSockets()

            coreThread = Thread {
                try {
                    val id = Phoenix.startVPN(fd, configJson)
                    sessionId = id
                    isRunning.set(true)
                    updateNotification("متصل")
                    Phoenix.waitVPN(id)
                } catch (t: Throwable) {
                    updateNotification("خطا: ${t.message}")
                } finally {
                    isRunning.set(false)
                    stopSelf()
                }
            }.apply { start() }
        } catch (t: Throwable) {
            updateNotification("خطا: ${t.message}")
            stopSelf()
        }
    }

    private fun protectCoreSockets() {
        // هسته Go مستقیماً dial می‌کند؛ در اندروید ≥ Q باید قبل از اتصال
        // هر سوکت protect شود. gomobile نسخه‌ی bind اینجا fd را از طریق
        // interface ProtectCallback می‌گیرد — فعلاً با تاخیر ساده:
        Thread.sleep(50)
    }

    override fun onDestroy() {
        if (sessionId >= 0) Phoenix.stopVPN(sessionId)
        isRunning.set(false)
        try { coreThread?.interrupt() } catch (_: Throwable) {}
        try { tunFd?.close() } catch (_: Throwable) {}
        super.onDestroy()
    }

    override fun onRevoke() {
        onDestroy()
        super.onRevoke()
    }

    // ---------- اعلان ----------
    private fun buildNotification(text: String): Notification {
        createChannel()
        val pi = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("PhoenixTunnel")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_lock_lock)
            .setContentIntent(pi)
            .setOngoing(true)
            .build()
    }

    private fun updateNotification(text: String) {
        val nm = getSystemService(NOTIFICATION_SERVICE) as NotificationManager
        nm.notify(NOTIF_ID, buildNotification(text))
    }

    private fun createChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val ch = NotificationChannel(CHANNEL_ID, "VPN", NotificationManager.IMPORTANCE_LOW)
            (getSystemService(NOTIFICATION_SERVICE) as NotificationManager).createNotificationChannel(ch)
        }
    }
}
