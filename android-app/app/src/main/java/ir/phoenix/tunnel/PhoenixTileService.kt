package ir.phoenix.tunnel

import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import android.app.PendingIntent

/** کاشی تنظیمات سریع: روشن/خاموش کردن VPN از پنل اعلان‌ها */
class PhoenixTileService : TileService() {

    override fun onStartListening() {
        super.onStartListening()
        update()
    }

    override fun onClick() {
        super.onClick()
        if (VpnState.isActive) {
            PhoenixVpnService.stop(this)
        } else {
            val s = ServerStore.selected(this)
            if (s == null || VpnService.prepare(this) != null) {
                openApp()
                return
            }
            PhoenixVpnService.start(this, s)
            VpnState.status = VpnState.Status.CONNECTING
        }
        update()
    }

    private fun openApp() {
        val i = Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        if (Build.VERSION.SDK_INT >= 34) {
            startActivityAndCollapse(PendingIntent.getActivity(this, 0, i, PendingIntent.FLAG_IMMUTABLE))
        } else {
            @Suppress("DEPRECATION")
            startActivityAndCollapse(i)
        }
    }

    private fun update() {
        val t = qsTile ?: return
        t.state = if (VpnState.isActive) Tile.STATE_ACTIVE else Tile.STATE_INACTIVE
        if (Build.VERSION.SDK_INT >= 29) t.subtitle = ServerStore.selected(this)?.name ?: ""
        t.updateTile()
    }
}
