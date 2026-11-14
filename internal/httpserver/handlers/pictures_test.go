package handlers_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/identity"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.SetRGBA(0, 0, color.RGBA{A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// upload posts a multipart form with the file as the "image" field.
func (c *client) upload(path, filename string, data []byte) *httptest.ResponseRecorder {
	c.e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("image", filename)
	_, _ = part.Write(data)
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/v1"+path, &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Origin", origin)
	r.AddCookie(c.cookie)
	r.Header.Set("X-CSRF-Token", c.csrf)
	w := httptest.NewRecorder()
	c.e.srv.Handler().ServeHTTP(w, r)
	return w
}

// TestProfilePictures covers the profile-images spec through the API.
func TestProfilePictures(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		e := newEnv(t, db, false)
		svc := &chat.Service{DB: db}
		e.api.Chat = svc
		ana := e.setupOwner()
		anaID := ana.do("GET", "/me", nil).body["user"].(map[string]any)["id"].(string)
		biaID := ana.createUser("bia@acme.io", "member")
		ana.createUser("gus@acme.io", "guest")
		bia, gus := e.login("bia@acme.io", "a long password!"), e.login("gus@acme.io", "a long password!")
		team := ana.do("POST", "/teams", map[string]string{"name": "eng", "display_name": "Engineering"}).body["id"].(string)
		if r := ana.do("POST", "/teams/"+team+"/members", map[string]string{"user_id": biaID}); r.Code != 204 {
			t.Fatalf("add bia: %d", r.Code)
		}

		get := func(c *client, id string, mutate ...func(*http.Request)) resp {
			return c.do("GET", "/users/"+id+"/image", nil, mutate...)
		}
		if r := get(bia, anaID); r.Code != 404 {
			t.Fatalf("picture before any upload: %d", r.Code)
		}
		// rejected uploads change nothing
		if w := ana.upload("/users/me/image", "photo.png", []byte("<!doctype html><script>alert(1)</script>")); w.Code != 400 || !bytes.Contains(w.Body.Bytes(), []byte("invalid_image")) {
			t.Fatalf("html as png: %d %s", w.Code, w.Body.String())
		}
		if w := ana.upload("/users/me/image", "big.png", make([]byte, identity.PictureMaxBytes+1)); w.Code != 413 || !bytes.Contains(w.Body.Bytes(), []byte("image_too_large")) {
			t.Fatalf("over 5 MiB: %d %s", w.Code, w.Body.String())
		}
		// a JSON body on the upload route is not a form
		if r := ana.do("POST", "/users/me/image", map[string]string{"image": "x"}); r.Code != 415 && r.Code != 400 {
			t.Fatalf("json upload: %d", r.Code)
		}

		w := ana.upload("/users/me/image", "photo.png", pngOf(t, 600, 400))
		if w.Code != 200 {
			t.Fatalf("upload: %d %s", w.Code, w.Body.String())
		}
		r := get(bia, anaID)
		img, format, err := image.Decode(bytes.NewReader(r.Body.Bytes()))
		if r.Code != 200 || err != nil || format != "png" || img.Bounds().Dx() != identity.PictureSide ||
			r.Header().Get("Content-Type") != "image/png" || r.Header().Get("X-Content-Type-Options") != "nosniff" ||
			r.Header().Get("Cache-Control") != "private, max-age=86400" || r.Header().Get("ETag") == "" {
			t.Fatalf("served picture: %d %s %v %v", r.Code, format, err, r.Header())
		}
		etag := r.Header().Get("ETag")
		if r := get(bia, anaID, func(q *http.Request) { q.Header.Set("If-None-Match", etag) }); r.Code != 304 {
			t.Fatalf("revalidation: %d", r.Code)
		}
		if r := get(ana, "me"); r.Code != 200 {
			t.Fatalf("own picture: %d", r.Code)
		}
		// the public profile carries the version; a guest without a shared channel sees neither
		var profiles []identity.PublicUser
		pr := bia.do("POST", "/users/ids", []string{anaID, biaID})
		if err := json.Unmarshal(pr.Body.Bytes(), &profiles); err != nil || len(profiles) != 2 {
			t.Fatalf("profiles: %v %s", err, pr.Body.String())
		}
		for _, p := range profiles {
			if (p.ID == anaID) != (p.LastPictureUpdate != 0) {
				t.Fatalf("last_picture_update: %+v", profiles)
			}
		}
		if r := get(gus, anaID); r.Code != 404 {
			t.Fatalf("a guest sees a stranger's picture: %d", r.Code)
		}
		if r := e.client().do("GET", "/users/"+anaID+"/image", nil); r.Code != 401 {
			t.Fatalf("logged out: %d", r.Code)
		}

		// own removal, then moderation with an audit event
		if r := ana.do("DELETE", "/users/me/image", nil); r.Code != 204 {
			t.Fatalf("remove own: %d %s", r.Code, r.Body.String())
		}
		if r := get(bia, anaID); r.Code != 404 {
			t.Fatalf("picture after removal: %d", r.Code)
		}
		if w := bia.upload("/users/me/image", "me.png", pngOf(t, 64, 64)); w.Code != 200 {
			t.Fatalf("bia upload: %d", w.Code)
		}
		if r := bia.do("DELETE", "/admin/users/"+anaID+"/image", nil); r.Code != 403 {
			t.Fatalf("a member moderating: %d", r.Code)
		}
		if r := ana.do("DELETE", "/admin/users/"+biaID+"/image", nil); r.Code != 204 {
			t.Fatalf("moderation: %d %s", r.Code, r.Body.String())
		}
		if r := get(ana, biaID); r.Code != 404 {
			t.Fatalf("picture after moderation: %d", r.Code)
		}
		ev, err := audit.List(t.Context(), db.Gorm, audit.Filter{Action: audit.ActionPictureRemoved})
		if err != nil || len(ev.Events) != 1 || ev.Events[0].ActorID == nil || *ev.Events[0].ActorID != anaID || ev.Events[0].TargetID != biaID {
			t.Fatalf("audit of the moderation: %+v %v", ev, err)
		}
	})
}
