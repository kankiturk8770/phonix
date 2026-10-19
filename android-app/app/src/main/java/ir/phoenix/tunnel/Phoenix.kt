package ir.phoenix.tunnel

/**
 * Phoenix — پل Kotlin به هسته‌ی Go (AAR تولیدشده توسط gomobile bind).
 *
 * بیلد هسته:
 *   gomobile bind -target=android -javapkg ir.phoenix.core \
 *       -o android-app/app/libs/phoenix.aar \
 *       github.com/phoenixtunnel/phoenix/client
 *
 * متدهای Client در Java: startVPN(long fd, String config) throws Exception
 */
object Phoenix {

    init {
        System.loadLibrary("gojni")   // همراه AAR
    }

    @JvmStatic
    fun startVPN(fd: Int, configJson: String): Int {
        return ir.phoenix.core.Client.startVPN(fd.toLong(), configJson).toInt()
    }

    @JvmStatic
    fun stopVPN(id: Int) {
        ir.phoenix.core.Client.stopVPN(id.toLong())
    }

    @JvmStatic
    fun waitVPN(id: Int) {
        ir.phoenix.core.Client.waitVPN(id.toLong())
    }
}
