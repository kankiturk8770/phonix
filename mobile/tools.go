//go:build tools

// وابستگی gomobile bind در go.mod نگه داشته شود (go mod tidy حذفش نکند)
package mobile

import _ "golang.org/x/mobile/bind"
