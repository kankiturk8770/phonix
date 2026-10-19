package ir.phoenix.tunnel

import android.content.Intent
import android.net.VpnService
import android.os.Bundle
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import org.json.JSONObject

class MainActivity : AppCompatActivity() {

    private lateinit var status: TextView
    private lateinit var serverInput: EditText
    private lateinit var keyInput: EditText

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        status = findViewById(R.id.tvStatus)
        serverInput = findViewById(R.id.etServer)
        keyInput = findViewById(R.id.etKey)

        findViewById<Button>(R.id.btnConnect).setOnClickListener { connect() }
        findViewById<Button>(R.id.btnDisconnect).setOnClickListener { disconnect() }
    }

    private fun connect() {
        val server = serverInput.text.toString().trim()
        val key = keyInput.text.toString().trim()
        if (server.isEmpty() || key.isEmpty()) {
            Toast.makeText(this, "سرور و کلید را وارد کنید", Toast.LENGTH_SHORT).show()
            return
        }
        val cfg = JSONObject()
            .put("server", server)
            .put("keys", key)
            .put("engine", "chameleon")
            .put("fake_snis", listOf("www.google.com", "www.microsoft.com"))
            .toString()

        // درخواست مجوز VPN از کاربر
        val intent = VpnService.prepare(this)
        if (intent != null) {
            startActivityForResult(intent, REQ_VPN)
        } else {
            startVpnService(cfg)
        }
        pendingConfig = cfg
    }

    private fun startVpnService(cfg: String) {
        val i = Intent(this, PhoenixVpnService::class.java)
            .setAction(PhoenixVpnService.ACTION_START)
            .putExtra(PhoenixVpnService.EXTRA_CONFIG, cfg)
        startService(i)
        status.text = "در حال اتصال…"
    }

    private fun disconnect() {
        startService(Intent(this, PhoenixVpnService::class.java)
            .setAction(PhoenixVpnService.ACTION_STOP))
        status.text = "قطع"
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == REQ_VPN && resultCode == RESULT_OK) {
            pendingConfig?.let { startVpnService(it) }
        }
    }

    override fun onResume() {
        super.onResume()
        status.text = if (PhoenixVpnService.isRunning.get()) "متصل" else "قطع"
    }

    companion object {
        const val REQ_VPN = 42
        var pendingConfig: String? = null
    }
}
