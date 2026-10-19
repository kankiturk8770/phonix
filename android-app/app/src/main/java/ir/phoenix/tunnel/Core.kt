package ir.phoenix.tunnel

import ir.phoenix.core.mobile.Mobile

/**
 * Core — پل Kotlin به هسته‌ی Go (AAR تولیدشده با gomobile bind از پکیج mobile/).
 * کلاس تولیدی: ir.phoenix.core.mobile.Mobile — int گو در Java به long تبدیل می‌شود.
 */
object Core {
    /** شروع VPN روی TUN fd؛ مالکیت fd به هسته منتقل می‌شود (detachFd). */
    fun start(fd: Int, configJson: String): Long = Mobile.startVPN(fd.toLong(), configJson)

    fun stop(id: Long) = Mobile.stopVPN(id)

    /** مسدود تا پایان نشست */
    fun await(id: Long) = Mobile.waitVPN(id)

    /** (آپلود، دانلود) به بایت */
    fun traffic(id: Long): Pair<Long, Long> {
        val p = Mobile.traffic(id).split(",")
        return Pair(p.getOrNull(0)?.toLongOrNull() ?: 0L, p.getOrNull(1)?.toLongOrNull() ?: 0L)
    }

    /** لینک phoenix:// (یا JSON) → JSON کانفیگ معتبر؛ در صورت خطا Exception */
    fun parseLink(link: String): String = Mobile.parseLink(link)

    fun buildLink(configJson: String): String = Mobile.buildLink(configJson)

    /** اتصال آزمایشی کامل؛ زمان به میلی‌ثانیه */
    fun ping(configJson: String): Long = Mobile.ping(configJson)

    fun version(): String = Mobile.getVersion()
}
