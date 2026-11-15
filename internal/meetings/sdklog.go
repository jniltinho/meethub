package meetings

import (
	lklogger "github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

// The LiveKit SDK logs through one process-wide logger. Its default prints every debug line of the WebRTC stack and,
// to build them, formats a connection's live state while another goroutine changes it: pion's "got new track" line
// is a data race under load, which the race detector reports. With this logger only warnings and errors are
// written, and a debug line is never formatted.
func init() {
	l, err := lklogger.NewZapLogger(&lklogger.Config{Level: "warn", JSON: true})
	if err != nil {
		return // the SDK's default logger stays
	}
	lksdk.SetLogger(l)
}
