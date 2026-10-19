# هسته‌ی Go (gomobile): کلاس‌ها از JNI صدا زده می‌شوند
-keep class go.** { *; }
-keep class ir.phoenix.core.** { *; }
-keepclasseswithmembernames class * { native <methods>; }
# zxing
-keep class com.journeyapps.barcodescanner.** { *; }
-dontwarn com.google.zxing.**
