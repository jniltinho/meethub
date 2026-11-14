package meetings

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/turn/v5"
	"github.com/twitchtv/twirp"
	"google.golang.org/protobuf/proto"
)

// checkStepTimeout bounds each network step of Check.
const checkStepTimeout = 10 * time.Second

// CheckResult is one probe of Check. Target and Detail never hold the API key, the secret or a token.
type CheckResult struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// Check probes LiveKit as meetings use it: its room API with the configured key, then a signalling join (as a
// throwaway participant of a throwaway room, deleted afterwards) for the ICE servers LiveKit hands browsers, then a
// TURN allocation on each TURN server among them. A TURN server on a loopback address (LiveKit's --node-ip
// 127.0.0.1) is tried at apiURL's host, the machine LiveKit runs on.
func Check(ctx context.Context, apiURL, key, secret string) []CheckResult {
	rooms := NewLiveKitRooms(apiURL, key, secret)
	api := CheckResult{Name: "LiveKit API", Target: apiURL}
	sctx, cancel := context.WithTimeout(ctx, checkStepTimeout)
	_, err := rooms.ListRooms(sctx)
	cancel()
	if err != nil {
		api.Detail = apiError(err)
		return []CheckResult{api}
	}
	api.OK, api.Detail = true, "the key is accepted"

	room := fmt.Sprintf("meethub-check-%d", time.Now().UnixNano())
	defer func() {
		dctx, dcancel := context.WithTimeout(context.WithoutCancel(ctx), checkStepTimeout)
		_, _ = rooms.client.DeleteRoom(dctx, &livekit.DeleteRoomRequest{Room: room})
		dcancel()
	}()
	conn, servers, err := joinICEServers(ctx, apiURL, key, secret, room)
	if err != nil {
		return []CheckResult{api, {Name: "Signalling", Target: wsURL(apiURL), Detail: err.Error()}}
	}
	// LiveKit's TURN serves only participants still connected: leave after the allocations
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	out := []CheckResult{api}
	for _, s := range servers {
		for _, u := range s.GetUrls() {
			if !strings.HasPrefix(u, "turn:") && !strings.HasPrefix(u, "turns:") {
				continue
			}
			r := CheckResult{Name: "TURN", Target: u}
			note, err := allocate(ctx, u, apiURL, s.GetUsername(), s.GetCredential())
			r.OK, r.Detail = err == nil, "allocation succeeded"
			if err != nil {
				r.Detail = err.Error()
			}
			if note != "" {
				r.Detail = note + ": " + r.Detail
			}
			out = append(out, r)
		}
	}
	if len(out) == 1 {
		out = append(out, CheckResult{Name: "TURN", Target: "-",
			Detail: "LiveKit offers no TURN server: enable turn in its configuration"})
	}
	return out
}

func apiError(err error) string {
	var te twirp.Error
	if errors.As(err, &te) && (te.Code() == twirp.Unauthenticated || te.Code() == twirp.PermissionDenied) {
		return "the key was refused: livekit.api_key and livekit.api_secret are not LiveKit's"
	}
	return "unreachable: " + err.Error()
}

// wsURL is the signalling endpoint of an http(s) LiveKit URL.
func wsURL(apiURL string) string {
	return "ws" + strings.TrimPrefix(strings.TrimSuffix(apiURL, "/"), "http") + "/rtc"
}

// joinICEServers joins room over LiveKit's signalling WebSocket and returns the ICE servers of its join response.
// The token goes in the Authorization header, so no error message (which may carry the URL) holds it. The caller
// closes the connection, which leaves the room.
func joinICEServers(ctx context.Context, apiURL, key, secret, room string) (*websocket.Conn, []*livekit.ICEServer, error) {
	token, err := auth.NewAccessToken(key, secret).
		SetVideoGrant(&auth.VideoGrant{RoomJoin: true, Room: room}).
		SetIdentity("meethub-check").SetValidFor(time.Minute).ToJWT()
	if err != nil {
		return nil, nil, fmt.Errorf("token: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, checkStepTimeout)
	defer cancel()
	q := url.Values{"protocol": {fmt.Sprint(lksdk.PROTOCOL)}, "sdk": {"go"}, "version": {lksdk.Version}, "auto_subscribe": {"0"}}
	conn, resp, err := websocket.Dial(ctx, wsURL(apiURL)+"?"+q.Encode(), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, nil, fmt.Errorf("signalling join: %w", err)
	}
	conn.SetReadLimit(1 << 20)
	for range 5 {
		_, b, err := conn.Read(ctx)
		var res livekit.SignalResponse
		if err == nil {
			err = proto.Unmarshal(b, &res)
		}
		if err != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return nil, nil, fmt.Errorf("signalling join: %w", err)
		}
		if j := res.GetJoin(); j != nil {
			return conn, j.GetIceServers(), nil
		}
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
	return nil, nil, errors.New("signalling join: no join response")
}

// turnAddr is the host:port of a turn:/turns: URL, with a loopback host replaced by apiURL's host.
func turnAddr(u, apiURL string) (addr string, moved bool, err error) {
	_, rest, _ := strings.Cut(u, ":")
	rest, _, _ = strings.Cut(rest, "?")
	host, port, err := net.SplitHostPort(rest)
	if err != nil {
		return "", false, fmt.Errorf("bad TURN URL: %w", err)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if a, err := url.Parse(apiURL); err == nil && a.Hostname() != "" && a.Hostname() != host {
			return net.JoinHostPort(a.Hostname(), port), true, nil
		}
	}
	return rest, false, nil
}

// allocate requests a TURN relay on u with the credentials LiveKit issued, over UDP, TCP or TLS as u says.
// The note says where a loopback URL was tried.
func allocate(ctx context.Context, u, apiURL, user, pass string) (note string, err error) {
	addr, moved, err := turnAddr(u, apiURL)
	if err != nil {
		return "", err
	}
	if moved {
		note = "loopback address, tried at " + addr
	}
	ctx, cancel := context.WithTimeout(ctx, checkStepTimeout)
	defer cancel()
	var pc net.PacketConn
	switch {
	case strings.HasPrefix(u, "turns:"):
		host, _, _ := net.SplitHostPort(addr)
		c, err := (&tls.Dialer{Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return note, fmt.Errorf("TLS: %w", err)
		}
		pc = turn.NewSTUNConn(c)
	case strings.Contains(u, "transport=tcp"):
		c, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return note, fmt.Errorf("TCP: %w", err)
		}
		pc = turn.NewSTUNConn(c)
	default:
		pc, err = (&net.ListenConfig{}).ListenPacket(ctx, "udp", ":0")
		if err != nil {
			return note, fmt.Errorf("UDP: %w", err)
		}
	}
	defer func() { _ = pc.Close() }()
	client, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr: addr, TURNServerAddr: addr, Username: user, Password: pass, Conn: pc,
		// LiveKit relays over IPv4 (its node IP); a dual-stack socket would otherwise ask for IPv6
		RequestedAddressFamily: turn.RequestedAddressFamilyIPv4,
	})
	if err != nil {
		return note, fmt.Errorf("TURN client: %w", err)
	}
	defer client.Close()
	if err := client.Listen(); err != nil {
		return note, fmt.Errorf("TURN client: %w", err)
	}
	relay, err := client.AllocateWithContext(ctx)
	if err != nil {
		return note, fmt.Errorf("allocation: %w", err)
	}
	return note, relay.Close()
}
