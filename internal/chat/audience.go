package chat

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jniltinho/meethub/internal/org"
)

// The status visibility predicate (one rule for status pushes, batch lookups
// and the get_statuses action): a viewer sees a user's status when they are the
// same person, share a channel or conversation, or share a team and neither of
// them is a guest.

// sharedChannelSQL pairs subject a with viewer b through a common channel.
const sharedChannelSQL = `SELECT DISTINCT a.user_id AS subject, b.user_id AS viewer
FROM channel_members a JOIN channel_members b ON b.channel_id = a.channel_id
WHERE a.user_id IN ? %s`

// sharedTeamSQL pairs subject a with viewer b through a common team, guests excluded on both sides.
const sharedTeamSQL = `SELECT DISTINCT a.user_id AS subject, b.user_id AS viewer
FROM team_members a JOIN team_members b ON b.team_id = a.team_id
WHERE a.user_id IN ? %s
AND a.user_id NOT IN (SELECT user_id FROM memberships WHERE role = ?)
AND b.user_id NOT IN (SELECT user_id FROM memberships WHERE role = ?)`

type statusPair struct {
	Subject string
	Viewer  string
}

func (s *Service) statusPairs(ctx context.Context, subjects []string, viewer string) ([]statusPair, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	extra, args := "", []any{subjects}
	if viewer != "" {
		extra, args = "AND b.user_id = ?", append(args, viewer)
	}
	var pairs []statusPair
	q := fmt.Sprintf(sharedChannelSQL, extra) + " UNION " + fmt.Sprintf(sharedTeamSQL, extra)
	all := append(append(slices.Clone(args), args...), org.RoleGuest, org.RoleGuest)
	if err := s.DB.Gorm.WithContext(ctx).Raw(q, all...).Scan(&pairs).Error; err != nil {
		return nil, fmt.Errorf("status audience: %w", err)
	}
	for i := range pairs {
		pairs[i].Subject = strings.TrimSpace(pairs[i].Subject)
		pairs[i].Viewer = strings.TrimSpace(pairs[i].Viewer)
	}
	return pairs, nil
}

// StatusWatchers returns, for each user, who may see that user's status
// (always including the user), in one query per batch.
func (s *Service) StatusWatchers(ctx context.Context, userIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(userIDs))
	for _, id := range userIDs {
		out[id] = []string{id}
	}
	for chunk := range slices.Chunk(userIDs, 500) {
		pairs, err := s.statusPairs(ctx, chunk, "")
		if err != nil {
			return nil, err
		}
		for _, p := range pairs {
			if p.Viewer != p.Subject {
				out[p.Subject] = append(out[p.Subject], p.Viewer)
			}
		}
	}
	return out, nil
}

// VisibleStatuses filters userIDs to those whose status viewerID may see;
// unknown and invisible users are omitted.
func (s *Service) VisibleStatuses(ctx context.Context, viewerID string, userIDs []string) ([]string, error) {
	pairs, err := s.statusPairs(ctx, userIDs, viewerID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if slices.Contains(userIDs, viewerID) {
		add(viewerID)
	}
	for _, p := range pairs {
		add(p.Subject)
	}
	return out, nil
}
