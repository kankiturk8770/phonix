package ir.phoenix.tunnel

/** وضعیت سراسری VPN — سرویس می‌نویسد، Activity و Tile می‌خوانند */
object VpnState {
    enum class Status { IDLE, CONNECTING, CONNECTED, ERROR }

    @Volatile var status: Status = Status.IDLE
    @Volatile var error: String? = null
    @Volatile var sessionId: Long = -1
    @Volatile var connectedAt: Long = 0
    @Volatile var serverName: String = ""
    @Volatile var pingMs: Long = -1

    val isActive get() = status == Status.CONNECTED || status == Status.CONNECTING
}
