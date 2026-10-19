package server

import (
	"net"

	"github.com/phoenixtunnel/phoenix/core"

)

type net_Conn = net.Conn
type net_PacketConn = net.PacketConn

// متدهای صادرشده فقط برای تست‌های پکیج client (ساخت اسپکرهای embeddable)

// HandleForTest جایگزین handle خصوصی در تست‌ها
func (c *ChameleonEngine) HandleForTest(conn net_Conn) { c.handle(conn) }

// ExportForTest مقداردهی فیلدهای خصوصی برای تست UDP
func (e *PhoenixUDPEngine) ExportForTest(pc net_PacketConn) {
	e.pc = pc
	e.sessions = map[uint32]*udpSession{}
}

// ServeForTest حلقه‌ی dispatch برای تست
func (e *PhoenixUDPEngine) ServeForTest() {
	buf := make([]byte, 4096)
	for {
		n, peer, err := e.pc.ReadFrom(buf)
		if err != nil {
			return
		}
		e.dispatch(peer, buf[:n])
	}
}

// NewChameleonEngineForTest ساخت موتور برای تست‌های client
func NewChameleonEngineForTest(keys []byte) *ChameleonEngine {
	return &ChameleonEngine{conf: core.ChameleonConf{}, keys: keys}
}

// NewPhoenixUDPEngineForTest ساخت موتور UDP برای تست‌های client
func NewPhoenixUDPEngineForTest(keys []byte) *PhoenixUDPEngine {
	return &PhoenixUDPEngine{conf: core.HydraConf{}, keys: keys}
}
