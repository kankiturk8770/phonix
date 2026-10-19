package ir.phoenix.tunnel

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID

/** یک سرور ذخیره‌شده: config همان JSON کانفیگ هسته است */
data class Server(val id: String, val config: String) {
    private val json get() = JSONObject(config)
    val name: String get() = json.optString("name").ifBlank { address }
    val engine: String get() = json.optString("engine", "chameleon")
    val address: String
        get() = if (engine == "phoenix-udp")
            json.optString("udp_server").ifBlank { json.optString("server") }
        else json.optString("server")
    val hasUdp: Boolean get() = json.optString("udp_server").isNotBlank()

    fun withEngine(e: String): Server = copy(config = JSONObject(config).put("engine", e).toString())
}

/** ذخیره‌ی سرورها در SharedPreferences */
object ServerStore {
    private const val PREFS = "phoenix"
    private const val KEY_SERVERS = "servers"
    private const val KEY_SELECTED = "selected"

    private fun prefs(ctx: Context) = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    fun all(ctx: Context): List<Server> {
        val arr = JSONArray(prefs(ctx).getString(KEY_SERVERS, "[]"))
        return (0 until arr.length()).mapNotNull {
            val o = arr.optJSONObject(it) ?: return@mapNotNull null
            Server(o.optString("id"), o.optString("config"))
        }
    }

    private fun save(ctx: Context, list: List<Server>) {
        val arr = JSONArray()
        list.forEach { arr.put(JSONObject().put("id", it.id).put("config", it.config)) }
        prefs(ctx).edit().putString(KEY_SERVERS, arr.toString()).apply()
    }

    /** افزودن از JSON معتبر (خروجی Core.parseLink)؛ تکراری‌ها جایگزین می‌شوند */
    fun add(ctx: Context, configJson: String): Server {
        val list = all(ctx).toMutableList()
        val o = JSONObject(configJson)
        val dupe = list.indexOfFirst {
            val j = JSONObject(it.config)
            j.optString("server") == o.optString("server") && j.optString("keys") == o.optString("keys")
        }
        val s = if (dupe >= 0) Server(list[dupe].id, configJson).also { list[dupe] = it }
        else Server(UUID.randomUUID().toString(), configJson).also { list.add(it) }
        save(ctx, list)
        select(ctx, s.id)
        return s
    }

    fun update(ctx: Context, s: Server) {
        save(ctx, all(ctx).map { if (it.id == s.id) s else it })
    }

    fun remove(ctx: Context, id: String) {
        val list = all(ctx).filter { it.id != id }
        save(ctx, list)
        if (selectedId(ctx) == id) select(ctx, list.firstOrNull()?.id)
    }

    fun selectedId(ctx: Context): String? = prefs(ctx).getString(KEY_SELECTED, null)

    fun select(ctx: Context, id: String?) {
        prefs(ctx).edit().putString(KEY_SELECTED, id).apply()
    }

    fun selected(ctx: Context): Server? {
        val list = all(ctx)
        return list.firstOrNull { it.id == selectedId(ctx) } ?: list.firstOrNull()
    }
}
