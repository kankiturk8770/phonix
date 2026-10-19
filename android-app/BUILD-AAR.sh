#!/bin/bash
# ساخت AAR هسته برای اندروید
set -e
cd "$(dirname "$0")"
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
gomobile bind -target=android -javapkg ir.phoenix.core \
    -o android-app/app/libs/phoenix.aar \
    ./gomobile-src/client
echo "AAR ساخته شد: android-app/app/libs/phoenix.aar"
echo "حالا پروژه android-app را در Android Studio باز کنید و assembleDebug بزنید."
