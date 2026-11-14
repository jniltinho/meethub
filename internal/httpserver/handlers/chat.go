package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/httpserver"
	"github.com/jniltinho/meethub/internal/presence"
)

// StatusStore reads and sets presence statuses (presence.Tracker).
type StatusStore interface {
	Get(ctx context.Context, userIDs []string) ([]presence.Status, error)
	SetDND(ctx context.Context, userID string, until time.Time) error
	ClearDND(ctx context.Context, userID string) error
	Activity(ctx context.Context, userID string) error
}

// registerChat adds the chat routes (design D10) to the authenticated group.
func (a *API) registerChat(in *echo.Group) {
	in.GET("/users/me/teams", a.myTeams)
	in.GET("/teams", a.joinableTeams)
	in.POST("/teams", a.createTeam)
	in.GET("/teams/:id", a.getTeam)
	in.PATCH("/teams/:id", a.updateTeam)
	in.POST("/teams/:id/archive", a.archiveTeam(true))
	in.POST("/teams/:id/restore", a.archiveTeam(false))
	in.GET("/teams/:id/members", a.teamMembers)
	in.POST("/teams/:id/members", a.addTeamMember)
	in.DELETE("/teams/:id/members/:user", a.removeTeamMember)
	in.PUT("/teams/:id/members/:user/role", a.setTeamRole)

	in.GET("/users/me/teams/:id/channels", a.myChannels)
	in.GET("/teams/:id/channels", a.joinableChannels)
	in.POST("/channels", a.createChannel)
	in.POST("/channels/direct", a.openDirect)
	in.POST("/channels/group", a.openGroup)
	in.GET("/channels/:id", a.getChannel)
	in.PATCH("/channels/:id", a.updateChannel)
	in.POST("/channels/:id/archive", a.archiveChannel(true))
	in.POST("/channels/:id/restore", a.archiveChannel(false))
	in.GET("/channels/:id/members", a.channelMembers)
	in.POST("/channels/:id/members", a.addChannelMember)
	in.DELETE("/channels/:id/members/:user", a.removeChannelMember)
	in.PUT("/channels/:id/members/:user/role", a.setChannelRole)
	in.POST("/channels/:id/view", a.viewChannel)
	in.GET("/users/me/conversations", a.myConversations)
	in.PUT("/users/me/conversations/:id/visibility", a.conversationVisibility)
	in.PUT("/users/me/channels/:id/favorite", a.channelFavorite)

	in.GET("/channels/:id/posts", a.channelPosts)
	in.POST("/posts", a.createPost)
	in.PATCH("/posts/:id", a.editPost)
	in.DELETE("/posts/:id", a.deletePost)
	in.GET("/posts/:id/thread", a.thread)
	in.POST("/posts/:id/pin", a.pinPost)
	in.POST("/posts/:id/unpin", a.unpinPost)
	in.GET("/channels/:id/pinned", a.pinnedPosts)
	in.POST("/posts/search", a.searchPosts)
	in.POST("/reactions", a.addReaction)
	in.DELETE("/users/me/posts/:id/reactions/:emoji", a.removeReaction)

	in.GET("/users", a.directory)
	in.POST("/users/ids", a.usersByIDs)
	in.POST("/users/status/ids", a.statusesByIDs)
	in.PUT("/users/me/status", a.setMyStatus)
}

func subject(c *echo.Context) chat.Subject {
	p := principal(c)
	return chat.Subject{UserID: p.User.ID, OrgRole: p.Membership.Role}
}

func (a *API) chatOrUnavailable() error {
	if a.Chat == nil {
		return ErrUnavailable
	}
	return nil
}

// page is a cursor-paginated list.
type page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func paged[T any](c *echo.Context, items []T, next string, err error) error {
	if err != nil {
		return err
	}
	if items == nil {
		items = []T{}
	}
	return c.JSON(http.StatusOK, page[T]{Items: items, NextCursor: next})
}

func perPage(c *echo.Context) int { return queryInt(c, "per_page") }

type userRef struct {
	UserID string `json:"user_id"`
}

type roleBody struct {
	Role string `json:"role"`
}

// ── Teams ────────────────────────────────────────────────────────────────────

func (a *API) myTeams(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	teams, err := a.Chat.MyTeams(c.Request().Context(), subject(c))
	return paged(c, teams, "", err)
}

func (a *API) joinableTeams(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	teams, next, err := a.Chat.JoinableTeams(c.Request().Context(), subject(c), c.QueryParam("cursor"), perPage(c))
	return paged(c, teams, next, err)
}

type teamBody struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

func (a *API) createTeam(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b teamBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	t, err := a.Chat.CreateTeam(c.Request().Context(), subject(c), chat.NewTeam(b))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, t)
}

func (a *API) getTeam(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	t, err := a.Chat.GetTeam(c.Request().Context(), subject(c), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, t)
}

type teamPatchBody struct {
	DisplayName *string `json:"display_name"`
	Description *string `json:"description"`
	Type        *string `json:"type"`
}

func (a *API) updateTeam(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b teamPatchBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	t, err := a.Chat.UpdateTeam(c.Request().Context(), subject(c), c.Param("id"), chat.TeamPatch(b))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, t)
}

func (a *API) archiveTeam(archived bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if err := a.chatOrUnavailable(); err != nil {
			return err
		}
		if err := a.Chat.SetTeamArchived(c.Request().Context(), subject(c), c.Param("id"), archived); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func (a *API) teamMembers(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	m, next, err := a.Chat.TeamMembers(c.Request().Context(), subject(c), c.Param("id"), c.QueryParam("cursor"), perPage(c))
	return paged(c, m, next, err)
}

// addTeamMember joins an open team (user_id = the caller) or adds someone (team admins).
func (a *API) addTeamMember(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b userRef
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	sub, ctx := subject(c), c.Request().Context()
	var err error
	if b.UserID == "" || b.UserID == sub.UserID {
		err = a.Chat.JoinTeam(ctx, sub, c.Param("id"))
	} else {
		err = a.Chat.AddTeamMember(ctx, sub, c.Param("id"), b.UserID)
	}
	if err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) removeTeamMember(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	if err := a.Chat.RemoveTeamMember(c.Request().Context(), subject(c), c.Param("id"), c.Param("user")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) setTeamRole(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b roleBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	if err := a.Chat.SetTeamRole(c.Request().Context(), subject(c), c.Param("id"), c.Param("user"), b.Role); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// ── Channels ─────────────────────────────────────────────────────────────────

func (a *API) myChannels(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	ch, next, err := a.Chat.MyChannels(c.Request().Context(), subject(c), c.Param("id"), c.QueryParam("cursor"), perPage(c))
	return paged(c, ch, next, err)
}

func (a *API) joinableChannels(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	ch, next, err := a.Chat.JoinableChannels(c.Request().Context(), subject(c), c.Param("id"), c.QueryParam("cursor"), perPage(c))
	return paged(c, ch, next, err)
}

type channelBody struct {
	TeamID      string `json:"team_id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Purpose     string `json:"purpose"`
	Header      string `json:"header"`
	Type        string `json:"type"`
}

func (a *API) createChannel(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b channelBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	ch, err := a.Chat.CreateChannel(c.Request().Context(), subject(c), b.TeamID, chat.NewChannel{
		Name: b.Name, DisplayName: b.DisplayName, Purpose: b.Purpose, Header: b.Header, Type: b.Type,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, ch)
}

func (a *API) openDirect(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b userRef
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	if b.UserID == "" {
		return httpserver.ErrInvalidInput
	}
	ch, err := a.Chat.OpenDirect(c.Request().Context(), subject(c), b.UserID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, ch)
}

type groupBody struct {
	UserIDs []string `json:"user_ids"`
}

func (a *API) openGroup(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b groupBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	ch, err := a.Chat.OpenGroup(c.Request().Context(), subject(c), b.UserIDs)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, ch)
}

func (a *API) getChannel(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	ch, err := a.Chat.GetChannel(c.Request().Context(), subject(c), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, ch)
}

type channelPatchBody struct {
	Name        *string `json:"name"`
	DisplayName *string `json:"display_name"`
	Purpose     *string `json:"purpose"`
	Header      *string `json:"header"`
}

func (a *API) updateChannel(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b channelPatchBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	ch, err := a.Chat.UpdateChannel(c.Request().Context(), subject(c), c.Param("id"), chat.ChannelPatch(b))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, ch)
}

func (a *API) archiveChannel(archived bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if err := a.chatOrUnavailable(); err != nil {
			return err
		}
		if err := a.Chat.SetChannelArchived(c.Request().Context(), subject(c), c.Param("id"), archived); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func (a *API) channelMembers(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	m, next, err := a.Chat.Members(c.Request().Context(), subject(c), c.Param("id"), c.QueryParam("cursor"), perPage(c))
	return paged(c, m, next, err)
}

// addChannelMember joins a public channel (user_id = the caller) or adds someone (channel admins).
func (a *API) addChannelMember(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b userRef
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	sub, ctx := subject(c), c.Request().Context()
	var err error
	if b.UserID == "" || b.UserID == sub.UserID {
		err = a.Chat.JoinChannel(ctx, sub, c.Param("id"))
	} else {
		err = a.Chat.AddChannelMember(ctx, sub, c.Param("id"), b.UserID)
	}
	if err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) removeChannelMember(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	if err := a.Chat.RemoveChannelMember(c.Request().Context(), subject(c), c.Param("id"), c.Param("user")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) setChannelRole(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b roleBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	if err := a.Chat.SetChannelRole(c.Request().Context(), subject(c), c.Param("id"), c.Param("user"), b.Role); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) viewChannel(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	if err := a.Chat.ViewChannel(c.Request().Context(), subject(c), c.Param("id")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) myConversations(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	list, next, err := a.Chat.MyConversations(c.Request().Context(), subject(c), c.QueryParam("cursor"), perPage(c))
	return paged(c, list, next, err)
}

type visibilityBody struct {
	Visible bool `json:"visible"`
}

func (a *API) conversationVisibility(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b visibilityBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	if err := a.Chat.SetConversationVisible(c.Request().Context(), subject(c), c.Param("id"), b.Visible); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

type favoriteBody struct {
	Favorite bool `json:"favorite"`
}

func (a *API) channelFavorite(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b favoriteBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	if err := a.Chat.SetFavorite(c.Request().Context(), subject(c), c.Param("id"), b.Favorite); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// ── Posts and reactions ──────────────────────────────────────────────────────

// channelPosts serves history (cursor), catch-up (since_revision), a window
// around a message (around) or the messages after a cursor (after).
func (a *API) channelPosts(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	ctx, sub := c.Request().Context(), subject(c)
	if s := c.QueryParam("since_revision"); s != "" {
		since, err := strconv.ParseInt(s, 10, 64)
		if err != nil || since < 0 {
			return httpserver.ErrInvalidInput
		}
		changes, err := a.Chat.ChangesSince(ctx, sub, c.Param("id"), since, perPage(c))
		if err != nil {
			return err
		}
		if changes.Posts == nil {
			changes.Posts = []chat.PostView{}
		}
		return c.JSON(http.StatusOK, changes)
	}
	if id := c.QueryParam("around"); id != "" {
		window, err := a.Chat.HistoryAround(ctx, sub, c.Param("id"), id, perPage(c))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, window)
	}
	if cursor := c.QueryParam("after"); cursor != "" {
		posts, next, err := a.Chat.HistoryAfter(ctx, sub, c.Param("id"), cursor, perPage(c))
		return paged(c, posts, next, err)
	}
	posts, next, err := a.Chat.History(ctx, sub, c.Param("id"), c.QueryParam("cursor"), perPage(c))
	return paged(c, posts, next, err)
}

type postBody struct {
	ChannelID     string   `json:"channel_id"`
	RootID        string   `json:"root_id"`
	Message       string   `json:"message"`
	PendingPostID string   `json:"pending_post_id"`
	FileIDs       []string `json:"file_ids"`
}

func (a *API) createPost(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b postBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	ctx, sub := c.Request().Context(), subject(c)
	p, err := a.Chat.CreatePost(ctx, sub, chat.NewPost(b))
	if err != nil {
		return err
	}
	if a.Presence != nil { // a post is activity
		_ = a.Presence.Activity(ctx, sub.UserID)
	}
	return c.JSON(http.StatusCreated, p)
}

type editBody struct {
	Message string `json:"message"`
}

func (a *API) editPost(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b editBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	p, err := a.Chat.EditPost(c.Request().Context(), subject(c), c.Param("id"), b.Message)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, p)
}

func (a *API) deletePost(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	if err := a.Chat.DeletePost(c.Request().Context(), subject(c), c.Param("id")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) thread(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	posts, next, err := a.Chat.Thread(c.Request().Context(), subject(c), c.Param("id"), c.QueryParam("cursor"), perPage(c))
	return paged(c, posts, next, err)
}

func (a *API) pinPost(c *echo.Context) error   { return a.setPinned(c, true) }
func (a *API) unpinPost(c *echo.Context) error { return a.setPinned(c, false) }

func (a *API) setPinned(c *echo.Context, pin bool) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	fn := a.Chat.UnpinPost
	if pin {
		fn = a.Chat.PinPost
	}
	p, err := fn(c.Request().Context(), subject(c), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, p)
}

func (a *API) pinnedPosts(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	posts, next, err := a.Chat.PinnedPosts(c.Request().Context(), subject(c), c.Param("id"), c.QueryParam("cursor"), perPage(c))
	return paged(c, posts, next, err)
}

type searchBody struct {
	Terms   string `json:"terms"`
	Page    int    `json:"page"`
	PerPage int    `json:"per_page"`
}

func (a *API) searchPosts(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b searchBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	res, err := a.Chat.Search(c.Request().Context(), subject(c), b.Terms, b.Page, b.PerPage)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

type reactionBody struct {
	PostID    string `json:"post_id"`
	EmojiName string `json:"emoji_name"`
}

func (a *API) addReaction(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var b reactionBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	if err := a.Chat.AddReaction(c.Request().Context(), subject(c), b.PostID, b.EmojiName); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) removeReaction(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	if err := a.Chat.RemoveReaction(c.Request().Context(), subject(c), c.Param("id"), c.Param("emoji")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// ── Users and statuses ───────────────────────────────────────────────────────

func (a *API) directory(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	users, next, err := a.Chat.Users(c.Request().Context(), subject(c), chat.DirectoryQuery{
		Query: c.QueryParam("q"), InTeam: c.QueryParam("in_team"), InChannel: c.QueryParam("in_channel"),
		Cursor: c.QueryParam("cursor"), Limit: perPage(c),
	})
	return paged(c, users, next, err)
}

// usersByIDs answers POST /users/ids (a JSON array of up to 200 ids) with public profiles.
func (a *API) usersByIDs(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	var ids []string
	if err := bindJSON(c, &ids); err != nil {
		return err
	}
	users, err := a.Chat.UsersByIDs(c.Request().Context(), subject(c), ids)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, users)
}

// statusesByIDs answers POST /users/status/ids (a JSON array of up to 200
// ids) with the statuses the caller may see; others are omitted.
func (a *API) statusesByIDs(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	if a.Presence == nil {
		return ErrUnavailable
	}
	var ids []string
	if err := bindJSON(c, &ids); err != nil {
		return err
	}
	if len(ids) > 200 {
		return httpserver.ErrInvalidInput
	}
	ctx := c.Request().Context()
	visible, err := a.Chat.VisibleStatuses(ctx, subject(c).UserID, ids)
	if err != nil {
		return err
	}
	sts, err := a.Presence.Get(ctx, visible)
	if err != nil {
		return err
	}
	if sts == nil {
		sts = []presence.Status{}
	}
	return c.JSON(http.StatusOK, sts)
}

type statusBody struct {
	Status     string `json:"status"`
	DNDEndTime int64  `json:"dnd_end_time"` // ms; 0 = until changed
}

// setMyStatus sets do not disturb or returns to automatic status (any other value).
func (a *API) setMyStatus(c *echo.Context) error {
	if a.Presence == nil {
		return ErrUnavailable
	}
	var b statusBody
	if err := bindJSON(c, &b); err != nil {
		return err
	}
	ctx, uid := c.Request().Context(), subject(c).UserID
	var err error
	switch b.Status {
	case presence.DND:
		until := time.Time{}
		if b.DNDEndTime > 0 {
			until = time.UnixMilli(b.DNDEndTime)
		}
		err = a.Presence.SetDND(ctx, uid, until)
	case presence.Online, presence.Away, presence.Offline:
		err = a.Presence.ClearDND(ctx, uid)
	default:
		return httpserver.ErrInvalidInput
	}
	if err != nil {
		return err
	}
	sts, err := a.Presence.Get(ctx, []string{uid})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, sts[0])
}
