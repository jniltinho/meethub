package meetings

import (
	"context"
	"errors"
	"fmt"

	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/twitchtv/twirp"
)

// LiveKitRooms is the RoomService of a LiveKit server (its Twirp room API, authenticated with the API key).
type LiveKitRooms struct{ client *lksdk.RoomServiceClient }

// NewLiveKitRooms reaches LiveKit's room API at apiURL (http or https).
func NewLiveKitRooms(apiURL, key, secret string) *LiveKitRooms {
	return &LiveKitRooms{client: lksdk.NewRoomServiceClient(apiURL, key, secret)}
}

func notFound(err error) bool {
	var te twirp.Error
	return errors.As(err, &te) && te.Code() == twirp.NotFound
}

// ListRooms returns the names of the rooms LiveKit has open.
func (l *LiveKitRooms) ListRooms(ctx context.Context) ([]string, error) {
	res, err := l.client.ListRooms(ctx, &livekit.ListRoomsRequest{})
	if err != nil {
		return nil, fmt.Errorf("livekit list rooms: %w", err)
	}
	out := make([]string, 0, len(res.GetRooms()))
	for _, r := range res.GetRooms() {
		out = append(out, r.GetName())
	}
	return out, nil
}

// ListParticipants returns who is in a room; none when LiveKit does not know it.
func (l *LiveKitRooms) ListParticipants(ctx context.Context, room string) ([]LiveParticipant, error) {
	res, err := l.client.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: room})
	if notFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("livekit list participants: %w", err)
	}
	out := make([]LiveParticipant, 0, len(res.GetParticipants()))
	for _, p := range res.GetParticipants() {
		joined := p.GetJoinedAtMs()
		if joined == 0 {
			joined = p.GetJoinedAt() * 1000
		}
		out = append(out, LiveParticipant{Identity: p.GetIdentity(), SID: p.GetSid(), JoinedAt: joined})
	}
	return out, nil
}

// RemoveParticipant disconnects identity from room; someone already gone is not an error.
func (l *LiveKitRooms) RemoveParticipant(ctx context.Context, room, identity string) error {
	_, err := l.client.RemoveParticipant(ctx, &livekit.RoomParticipantIdentity{Room: room, Identity: identity})
	if err != nil && !notFound(err) {
		return fmt.Errorf("livekit remove participant: %w", err)
	}
	return nil
}
