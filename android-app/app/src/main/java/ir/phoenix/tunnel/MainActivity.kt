package ir.phoenix.tunnel

import android.Manifest
import android.animation.ObjectAnimator
import android.animation.ValueAnimator
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.view.animation.LinearInterpolator
import android.widget.EditText
import android.widget.ImageButton
import android.widget.ImageView
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import com.google.android.material.bottomsheet.BottomSheetDialog
import com.google.android.material.button.MaterialButton
import com.google.android.material.button.MaterialButtonToggleGroup
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import java.util.Locale

class MainActivity : AppCompatActivity() {

    private lateinit var btnPower: View
    private lateinit var ring: View
    private lateinit var glow: View
    private lateinit var icPower: ImageView
    private lateinit var tvPowerHint: TextView
    private lateinit var tvStatus: TextView
    private lateinit var tvDetail: TextView
    private lateinit var tvDownSpeed: TextView
    private lateinit var tvUpSpeed: TextView
    private lateinit var tvDownTotal: TextView
    private lateinit var tvUpTotal: TextView
    private lateinit var tvServerName: TextView
    private lateinit var tvServerAddr: TextView
    private lateinit var tvServerCount: TextView
    private lateinit var toggleEngine: MaterialButtonToggleGroup

    private val ui = Handler(Looper.getMainLooper())
    private var lastStatus: VpnState.Status? = null
    private var lastUp = 0L
    private var lastDown = 0L
    private var lastTick = 0L
    private var pulse: ObjectAnimator? = null
    private var spin: ObjectAnimator? = null
    private var pendingStart: Server? = null

    private val vpnPermission = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) {
        val s = pendingStart
        pendingStart = null
        if (it.resultCode == RESULT_OK && s != null) PhoenixVpnService.start(this, s)
        else {
            VpnState.status = VpnState.Status.IDLE
            toast("مجوز VPN داده نشد")
        }
    }

    private val scanQr = registerForActivityResult(ScanContract()) { r ->
        r.contents?.let { importLink(it) }
    }

    private val notifPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) {}

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        btnPower = findViewById(R.id.btnPower)
        ring = findViewById(R.id.ring)
        glow = findViewById(R.id.glow)
        icPower = findViewById(R.id.icPower)
        tvPowerHint = findViewById(R.id.tvPowerHint)
        tvStatus = findViewById(R.id.tvStatus)
        tvDetail = findViewById(R.id.tvDetail)
        tvDownSpeed = findViewById(R.id.tvDownSpeed)
        tvUpSpeed = findViewById(R.id.tvUpSpeed)
        tvDownTotal = findViewById(R.id.tvDownTotal)
        tvUpTotal = findViewById(R.id.tvUpTotal)
        tvServerName = findViewById(R.id.tvServerName)
        tvServerAddr = findViewById(R.id.tvServerAddr)
        tvServerCount = findViewById(R.id.tvServerCount)
        toggleEngine = findViewById(R.id.toggleEngine)

        btnPower.setOnClickListener { onPower() }
        findViewById<View>(R.id.cardServer).setOnClickListener { showServers() }
        findViewById<MaterialButton>(R.id.btnPaste).setOnClickListener { pasteFromClipboard() }
        findViewById<MaterialButton>(R.id.btnScan).setOnClickListener { startScan() }
        findViewById<MaterialButton>(R.id.btnPing).setOnClickListener { testServer() }
        toggleEngine.addOnButtonCheckedListener { _, id, checked ->
            if (!checked) return@addOnButtonCheckedListener
            val s = ServerStore.selected(this) ?: return@addOnButtonCheckedListener
            val engine = if (id == R.id.btnUdp) "phoenix-udp" else "chameleon"
            if (s.engine == engine) return@addOnButtonCheckedListener
            if (engine == "phoenix-udp" && !s.hasUdp) {
                toast("این سرور پورت UDP ندارد")
                toggleEngine.check(R.id.btnChameleon)
                return@addOnButtonCheckedListener
            }
            ServerStore.update(this, s.withEngine(engine))
            renderServer()
            if (VpnState.isActive) toast("برای اعمال، دوباره وصل شوید")
        }
        try {
            findViewById<TextView>(R.id.tvVersion).text = "نسخه ${BuildConfig.VERSION_NAME} · هسته ${Core.version()}"
        } catch (_: Throwable) {
        }

        if (Build.VERSION.SDK_INT >= 33 &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) notifPermission.launch(Manifest.permission.POST_NOTIFICATIONS)

        handleIntent(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handleIntent(intent)
    }

    private fun handleIntent(i: Intent?) {
        val text = when (i?.action) {
            Intent.ACTION_VIEW -> i.dataString
            Intent.ACTION_SEND -> i.getStringExtra(Intent.EXTRA_TEXT)
            else -> null
        } ?: return
        extractLink(text)?.let { importLink(it) } ?: toast("لینک phoenix:// پیدا نشد")
    }

    override fun onResume() {
        super.onResume()
        renderServer()
        ui.post(ticker)
    }

    override fun onPause() {
        super.onPause()
        ui.removeCallbacks(ticker)
    }

    // ---------- اتصال ----------
    private fun onPower() {
        if (VpnState.isActive) {
            PhoenixVpnService.stop(this)
            return
        }
        val s = ServerStore.selected(this)
        if (s == null) {
            toast("اول یک سرور اضافه کنید")
            showServers()
            return
        }
        val prep = VpnService.prepare(this)
        if (prep != null) {
            pendingStart = s
            vpnPermission.launch(prep)
        } else {
            PhoenixVpnService.start(this, s)
        }
        VpnState.status = VpnState.Status.CONNECTING
        VpnState.error = null
        render()
    }

    private fun testServer() {
        val s = ServerStore.selected(this) ?: return toast("سروری انتخاب نشده")
        tvDetail.text = "در حال تست ${s.name}…"
        Thread {
            val msg = try {
                "✓ سرور سالم است · ${Core.ping(s.config)} ms"
            } catch (t: Throwable) {
                "✗ ${t.message}"
            }
            runOnUiThread { toast(msg); render(force = true) }
        }.start()
    }

    // ---------- افزودن سرور ----------
    private fun extractLink(text: String): String? {
        val t = text.trim()
        if (t.startsWith("{")) return t
        return Regex("phoenix://[A-Za-z0-9_\\-+/=]+(#[^\\s]*)?", RegexOption.IGNORE_CASE).find(t)?.value
    }

    private fun importLink(raw: String) {
        val link = extractLink(raw) ?: return toast("لینک phoenix:// معتبر نیست")
        try {
            val json = Core.parseLink(link)
            val s = ServerStore.add(this, json)
            toast("سرور «${s.name}» اضافه شد ✓")
            renderServer()
        } catch (t: Throwable) {
            toast("خطا در لینک: ${t.message}")
        }
    }

    private fun pasteFromClipboard() {
        val cm = getSystemService(CLIPBOARD_SERVICE) as ClipboardManager
        val text = cm.primaryClip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.coerceToText(this)?.toString()
        if (text.isNullOrBlank()) return toast("کلیپ‌بورد خالی است")
        importLink(text)
    }

    private fun startScan() {
        scanQr.launch(ScanOptions().apply {
            setDesiredBarcodeFormats(ScanOptions.QR_CODE)
            setPrompt("QR لینک PhoenixTunnel را اسکن کنید")
            setBeepEnabled(false)
            setOrientationLocked(false)
        })
    }

    private fun manualDialog() {
        val input = EditText(this).apply {
            hint = "phoenix://..."
            textDirection = View.TEXT_DIRECTION_LTR
            setPadding(48, 32, 48, 32)
        }
        MaterialAlertDialogBuilder(this)
            .setTitle("وارد کردن لینک")
            .setView(input)
            .setPositiveButton("افزودن") { _, _ -> importLink(input.text.toString()) }
            .setNegativeButton("انصراف", null)
            .show()
    }

    // ---------- لیست سرورها ----------
    private fun showServers() {
        val dlg = BottomSheetDialog(this)
        val v = layoutInflater.inflate(R.layout.sheet_servers, null)
        val list = v.findViewById<RecyclerView>(R.id.list)
        val empty = v.findViewById<TextView>(R.id.empty)
        list.layoutManager = LinearLayoutManager(this)
        fun reload() {
            val items = ServerStore.all(this)
            empty.visibility = if (items.isEmpty()) View.VISIBLE else View.GONE
            list.adapter = ServerAdapter(items, ServerStore.selected(this)?.id,
                onPick = {
                    ServerStore.select(this, it.id)
                    renderServer()
                    dlg.dismiss()
                    if (VpnState.isActive) toast("برای اعمال، دوباره وصل شوید")
                },
                onShare = { shareServer(it) },
                onDelete = { s ->
                    MaterialAlertDialogBuilder(this)
                        .setTitle("حذف «${s.name}»؟")
                        .setPositiveButton("حذف") { _, _ -> ServerStore.remove(this, s.id); reload(); renderServer() }
                        .setNegativeButton("انصراف", null).show()
                })
        }
        reload()
        v.findViewById<MaterialButton>(R.id.btnManual).setOnClickListener { dlg.dismiss(); manualDialog() }
        dlg.setContentView(v)
        dlg.show()
    }

    private fun shareServer(s: Server) {
        try {
            val link = Core.buildLink(s.config)
            val cm = getSystemService(CLIPBOARD_SERVICE) as ClipboardManager
            cm.setPrimaryClip(ClipData.newPlainText("phoenix", link))
            startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).setType("text/plain")
                .putExtra(Intent.EXTRA_TEXT, link), "اشتراک سرور"))
        } catch (t: Throwable) {
            toast(t.message ?: "خطا")
        }
    }

    private fun renderServer() {
        val all = ServerStore.all(this)
        val s = ServerStore.selected(this)
        tvServerCount.text = all.size.toString()
        if (s == null) {
            tvServerName.text = "سروری انتخاب نشده"
            tvServerAddr.text = "لینک phoenix:// را اضافه کنید"
            return
        }
        tvServerName.text = s.name
        tvServerAddr.text = s.address
        toggleEngine.check(if (s.engine == "phoenix-udp") R.id.btnUdp else R.id.btnChameleon)
        render(force = true)
    }

    // ---------- نمایش وضعیت ----------
    private val ticker = object : Runnable {
        override fun run() {
            render()
            ui.postDelayed(this, 1000)
        }
    }

    private fun render(force: Boolean = false) {
        val st = VpnState.status
        if (force || st != lastStatus) {
            lastStatus = st
            when (st) {
                VpnState.Status.CONNECTED -> {
                    tvStatus.text = "متصل"
                    tvStatus.setTextColor(getColor(R.color.ok))
                    tvPowerHint.text = "قطع"
                    icPower.setColorFilter(getColor(R.color.gold))
                    ring.setBackgroundResource(R.drawable.power_ring_on)
                    startPulse(fast = false)
                }
                VpnState.Status.CONNECTING -> {
                    tvStatus.text = "در حال اتصال…"
                    tvStatus.setTextColor(getColor(R.color.gold))
                    tvPowerHint.text = "لغو"
                    icPower.setColorFilter(getColor(R.color.fire))
                    ring.setBackgroundResource(R.drawable.power_ring_on)
                    startPulse(fast = true)
                }
                VpnState.Status.ERROR -> {
                    tvStatus.text = "خطا"
                    tvStatus.setTextColor(getColor(R.color.bad))
                    tvPowerHint.text = "اتصال"
                    icPower.setColorFilter(getColor(R.color.dim))
                    ring.setBackgroundResource(R.drawable.power_ring_off)
                    stopPulse()
                }
                VpnState.Status.IDLE -> {
                    tvStatus.text = "قطع"
                    tvStatus.setTextColor(getColor(R.color.txt))
                    tvPowerHint.text = "اتصال"
                    icPower.setColorFilter(getColor(R.color.dim))
                    ring.setBackgroundResource(R.drawable.power_ring_off)
                    stopPulse()
                    tvDownSpeed.text = "0 B/s"; tvUpSpeed.text = "0 B/s"
                }
            }
        }
        when (st) {
            VpnState.Status.CONNECTED -> {
                val id = VpnState.sessionId
                if (id >= 0) {
                    val (up, down) = try { Core.traffic(id) } catch (_: Throwable) { 0L to 0L }
                    val now = System.currentTimeMillis()
                    if (lastTick > 0 && now > lastTick) {
                        val dt = (now - lastTick) / 1000.0
                        tvDownSpeed.text = "${fmt(((down - lastDown).coerceAtLeast(0) / dt).toLong())}/s"
                        tvUpSpeed.text = "${fmt(((up - lastUp).coerceAtLeast(0) / dt).toLong())}/s"
                    }
                    lastUp = up; lastDown = down; lastTick = now
                    tvDownTotal.text = "دانلود · ${fmt(down)}"
                    tvUpTotal.text = "آپلود · ${fmt(up)}"
                }
                val secs = (System.currentTimeMillis() - VpnState.connectedAt) / 1000
                val ping = if (VpnState.pingMs >= 0) " · ${VpnState.pingMs} ms" else ""
                tvDetail.text = "${VpnState.serverName} · ${String.format(Locale.US, "%02d:%02d:%02d", secs / 3600, secs / 60 % 60, secs % 60)}$ping"
            }
            VpnState.Status.CONNECTING -> tvDetail.text = "تست سرور و برقراری تونل امن…"
            VpnState.Status.ERROR -> tvDetail.text = VpnState.error ?: "خطای ناشناخته"
            VpnState.Status.IDLE -> {
                lastTick = 0
                tvDetail.text = ServerStore.selected(this)?.let { "آماده‌ی اتصال به ${it.name}" }
                    ?: "برای شروع یک سرور اضافه کنید"
            }
        }
    }

    private fun startPulse(fast: Boolean) {
        stopPulse()
        glow.alpha = 0.4f
        pulse = ObjectAnimator.ofFloat(glow, View.ALPHA, 0.35f, 1f).apply {
            duration = if (fast) 600 else 1800
            repeatMode = ValueAnimator.REVERSE
            repeatCount = ValueAnimator.INFINITE
            start()
        }
        spin = ObjectAnimator.ofFloat(ring, View.ROTATION, 0f, 360f).apply {
            duration = if (fast) 1200 else 6000
            interpolator = LinearInterpolator()
            repeatCount = ValueAnimator.INFINITE
            start()
        }
    }

    private fun stopPulse() {
        pulse?.cancel(); spin?.cancel()
        pulse = null; spin = null
        glow.animate().alpha(0f).setDuration(300).start()
        ring.rotation = 0f
    }

    private fun fmt(b: Long): String {
        val u = arrayOf("B", "KB", "MB", "GB", "TB")
        var v = b.toDouble()
        var i = 0
        while (v >= 1024 && i < u.size - 1) { v /= 1024; i++ }
        return if (i == 0) "${b} B" else String.format(Locale.US, "%.1f %s", v, u[i])
    }

    private fun toast(m: String) = Toast.makeText(this, m, Toast.LENGTH_SHORT).show()
}

/** آداپتر لیست سرورها */
class ServerAdapter(
    private val items: List<Server>,
    private val selectedId: String?,
    private val onPick: (Server) -> Unit,
    private val onShare: (Server) -> Unit,
    private val onDelete: (Server) -> Unit,
) : RecyclerView.Adapter<ServerAdapter.VH>() {

    class VH(v: View) : RecyclerView.ViewHolder(v) {
        val dot: View = v.findViewById(R.id.dot)
        val name: TextView = v.findViewById(R.id.name)
        val addr: TextView = v.findViewById(R.id.addr)
        val share: ImageButton = v.findViewById(R.id.share)
        val delete: ImageButton = v.findViewById(R.id.delete)
    }

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int) =
        VH(LayoutInflater.from(parent.context).inflate(R.layout.item_server, parent, false))

    override fun getItemCount() = items.size

    override fun onBindViewHolder(h: VH, pos: Int) {
        val s = items[pos]
        h.name.text = s.name
        h.addr.text = "${s.address} · ${if (s.engine == "phoenix-udp") "UDP" else "TCP"}"
        h.dot.setBackgroundResource(if (s.id == selectedId) R.drawable.dot_on else R.drawable.dot_off)
        h.itemView.setOnClickListener { onPick(s) }
        h.itemView.setOnLongClickListener { onShare(s); true }
        h.share.setOnClickListener { onShare(s) }
        h.delete.setOnClickListener { onDelete(s) }
    }
}
