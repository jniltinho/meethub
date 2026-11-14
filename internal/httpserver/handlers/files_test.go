package handlers_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/jpeg"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/files"
	"github.com/jniltinho/meethub/internal/testenv"
)

type upload struct {
	name string
	data []byte
	typ  string // declared content type ("" lets the form writer choose)
}

// uploadFiles posts the multipart form of POST /files: channel_id, then the files.
func (c *client) uploadFiles(channelID string, ups ...upload) resp {
	c.e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if channelID != "" {
		_ = mw.WriteField("channel_id", channelID)
	}
	for _, u := range ups {
		h := make(map[string][]string)
		h["Content-Disposition"] = []string{multipart.FileContentDisposition("files", u.name)}
		if u.typ != "" {
			h["Content-Type"] = []string{u.typ}
		}
		part, _ := mw.CreatePart(h)
		_, _ = part.Write(u.data)
	}
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/files", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Origin", origin)
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	r.Header.Set("X-CSRF-Token", c.csrf)
	w := httptest.NewRecorder()
	c.e.srv.Handler().ServeHTTP(w, r)
	out := resp{ResponseRecorder: w}
	_ = json.Unmarshal(w.Body.Bytes(), &out.body)
	return out
}

func infos(t *testing.T, r resp) []map[string]any {
	t.Helper()
	var p struct {
		Infos []map[string]any `json:"file_infos"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil || r.Code != 201 {
		t.Fatalf("upload: %d %s", r.Code, r.Body.String())
	}
	return p.Infos
}

// photo is a JPEG carrying an EXIF segment: how a viewer must turn it (1: as
// stored, 6: a quarter turn clockwise) and a recognizable location.
func photo(t *testing.T, w, h int, orientation byte) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	exif := []byte("Exif\x00\x00MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00")
	exif = append(append(exif, orientation, 0, 0, 0, 0, 0, 0), "GPS-SECRET-LOCATION -23.55,-46.63"...)
	seg := append([]byte{0xff, 0xe1, 0, 0}, exif...)
	binary.BigEndian.PutUint16(seg[2:], uint16(len(exif)+2)) //nolint:gosec // a short test segment
	return append(append(append([]byte{}, b.Bytes()[:2]...), seg...), b.Bytes()[2:]...)
}

// bombPNG is a tiny PNG whose header declares side × side pixels.
func bombPNG(side uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], side)
	binary.BigEndian.PutUint32(ihdr[4:], side)
	ihdr[8], ihdr[9] = 8, 6 // 8 bits, RGBA
	chunk := func(typ string, data []byte) []byte {
		out := binary.BigEndian.AppendUint32(nil, uint32(len(data))) //nolint:gosec // tiny test chunks
		out = append(append(out, typ...), data...)
		return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(out[4:]))
	}
	return append(append([]byte("\x89PNG\r\n\x1a\n"), chunk("IHDR", ihdr)...), chunk("IEND", nil)...)
}

// events records what the chat publishes.
type events struct {
	mu  sync.Mutex
	all []chat.Event
}

func (e *events) Publish(_ context.Context, evs ...chat.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.all = append(e.all, evs...)
}

func (e *events) last(name string) *chat.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.all) - 1; i >= 0; i-- {
		if e.all[i].Name == name {
			return &e.all[i]
		}
	}
	return nil
}

// flakyStorage fails where the test tells it to.
type flakyStorage struct {
	files.Storage
	failDelete string // a key whose Delete fails
	down       bool   // everything fails
}

func (f *flakyStorage) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if f.down {
		return files.ErrUnavailable
	}
	return f.Storage.Put(ctx, key, r, size)
}

func (f *flakyStorage) Open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	if f.down {
		return nil, files.ErrUnavailable
	}
	return f.Storage.Open(ctx, key)
}

func (f *flakyStorage) Delete(ctx context.Context, key string) error {
	if f.down || (f.failDelete != "" && key == f.failDelete) {
		return files.ErrUnavailable
	}
	return f.Storage.Delete(ctx, key)
}

type fileWorld struct {
	e                      *env
	svc                    *files.Service
	st                     *flakyStorage
	ev                     *events
	now                    time.Time
	ana, bia, carl         *client
	biaID                  string
	team, proj, priv, side string // #projects (ana, bia), private #finance (ana), #side (ana, bia)
}

func (w *fileWorld) call(c *client, method, path string, body any, want int, code ...string) resp {
	w.e.t.Helper()
	r := c.do(method, path, body)
	if r.Code != want || (len(code) > 0 && r.code() != code[0]) {
		w.e.t.Fatalf("%s %s: %d %s, want %d %v", method, path, r.Code, r.Body.String(), want, code)
	}
	return r
}

func newFileWorld(t *testing.T, db *database.DB, st files.Storage) *fileWorld {
	t.Helper()
	w := &fileWorld{e: newEnv(t, db, false), st: &flakyStorage{Storage: st}, ev: &events{}, now: time.Now()}
	chatSvc := &chat.Service{DB: db, Publisher: w.ev, MaxFilesPerPost: 3}
	w.svc = files.New(db, chatSvc, w.st, 1<<20, 3, nil)
	w.svc.Now = func() time.Time { w.now = w.now.Add(time.Millisecond); return w.now }
	w.e.api.Chat, w.e.api.Files = chatSvc, w.svc
	w.ana = w.e.setupOwner()
	w.biaID = w.ana.createUser("bia@acme.io", "member")
	carlID := w.ana.createUser("carl@acme.io", "member")
	w.bia, w.carl = w.e.login("bia@acme.io", "a long password!"), w.e.login("carl@acme.io", "a long password!")
	w.team = w.call(w.ana, "POST", "/teams", map[string]string{"name": "eng", "display_name": "Engineering"}, 201).body["id"].(string)
	for _, id := range []string{w.biaID, carlID} {
		w.call(w.ana, "POST", "/teams/"+w.team+"/members", map[string]string{"user_id": id}, 204)
	}
	mk := func(display, typ string) string {
		return w.call(w.ana, "POST", "/channels", map[string]string{"team_id": w.team, "display_name": display, "type": typ}, 201).body["id"].(string)
	}
	w.proj, w.priv, w.side = mk("Projects", "O"), mk("Finance", "P"), mk("Side", "O")
	for _, ch := range []string{w.proj, w.side} {
		w.call(w.bia, "POST", "/channels/"+ch+"/members", map[string]string{}, 204)
	}
	return w
}

func (w *fileWorld) post(c *client, channelID, text, key string, fileIDs ...string) resp {
	return c.do("POST", "/posts", map[string]any{"channel_id": channelID, "message": text, "pending_post_id": key, "file_ids": fileIDs})
}

// get fetches a binary route (the JSON helper would parse the body).
func (c *client) get(path string, header ...string) *httptest.ResponseRecorder {
	return c.do("GET", path, nil, func(r *http.Request) {
		for i := 0; i+1 < len(header); i += 2 {
			r.Header.Set(header[i], header[i+1])
		}
	}).ResponseRecorder
}

func storedFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// TestFilesAPI covers the file-attachments, file-previews and messages specs
// through the API on every database, with the local storage.
func TestFilesAPI(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		dir := t.TempDir()
		local, err := files.NewLocal(dir)
		if err != nil {
			t.Fatal(err)
		}
		w := newFileWorld(t, db, local)
		ana, bia, carl := w.ana, w.bia, w.carl

		// refused uploads store nothing
		for name, tc := range map[string]struct {
			r    resp
			want int
			code string
		}{
			"not a member":        {carl.uploadFiles(w.priv, upload{name: "a.txt", data: []byte("x")}), 404, "not_found"},
			"unknown channel":     {ana.uploadFiles("01J0000000000000000000NONE", upload{name: "a.txt", data: []byte("x")}), 404, "not_found"},
			"too large":           {ana.uploadFiles(w.proj, upload{name: "big.bin", data: make([]byte, 1<<20+1)}), 413, "file_too_large"},
			"empty file":          {ana.uploadFiles(w.proj, upload{name: "empty.txt"}), 400, "invalid_input"},
			"no file":             {ana.uploadFiles(w.proj), 400, "invalid_input"},
			"file before channel": {ana.uploadFiles("", upload{name: "a.txt", data: []byte("x")}), 400, "invalid_input"},
			"logged out":          {w.e.client().uploadFiles(w.proj, upload{name: "a.txt", data: []byte("x")}), 401, "unauthenticated"},
		} {
			if tc.r.Code != tc.want || tc.r.code() != tc.code {
				t.Fatalf("%s: %d %s", name, tc.r.Code, tc.r.Body.String())
			}
		}
		if n := storedFiles(t, dir); n != 0 {
			t.Fatalf("%d objects left by refused uploads", n)
		}
		if r := ana.uploadFiles(w.proj, upload{name: "1"}, upload{name: "2", data: []byte("x")}); r.Code != 400 {
			t.Fatalf("an empty file among others: %d", r.Code)
		}
		four := []upload{{name: "1", data: []byte("1")}, {name: "2", data: []byte("2")}, {name: "3", data: []byte("3")}, {name: "4", data: []byte("4")}}
		if r := ana.uploadFiles(w.proj, four...); r.Code != 400 || r.code() != "too_many_files" {
			t.Fatalf("more files than a message carries: %d %s", r.Code, r.Body.String())
		}

		// upload: the name is metadata, the type comes from the bytes
		html := []byte("<!doctype html><html><script>alert(1)</script></html>")
		up := infos(t, bia.uploadFiles(w.proj,
			upload{name: "plan.xlsx", data: []byte("PK\x03\x04 spreadsheet")},
			upload{name: "../../etc/passwd", data: html, typ: "image/png"},
			upload{name: "report.pdf", data: []byte("%PDF-1.4\n%%EOF")}))
		if len(up) != 3 || up[0]["name"] != "plan.xlsx" || up[0]["size"] != float64(16) || up[0]["extension"] != "xlsx" {
			t.Fatalf("upload: %v", up)
		}
		if up[1]["name"] != "passwd" || up[1]["mime_type"] != "text/html" || up[1]["has_preview_image"] != false {
			t.Fatalf("hostile name and declared type: %v", up[1])
		}
		plan, page, pdf := up[0]["id"].(string), up[1]["id"].(string), up[2]["id"].(string)

		// unattached: only the uploader
		if r := ana.get("/files/" + plan); r.Code != 404 {
			t.Fatalf("another member reads an unattached upload: %d", r.Code)
		}
		if r := bia.get("/files/" + plan); r.Code != 200 || r.Body.String() != "PK\x03\x04 spreadsheet" {
			t.Fatalf("uploader reads an unattached upload: %d", r.Code)
		}

		// messages with files
		if r := w.post(ana, w.proj, "mine?", "a-1", plan); r.Code != 400 || r.code() != "invalid_input" {
			t.Fatalf("someone else's upload: %d %s", r.Code, r.Body.String())
		}
		if r := w.post(bia, w.side, "wrong channel", "b-0", plan); r.Code != 400 {
			t.Fatalf("a file of another channel: %d", r.Code)
		}
		if r := w.post(bia, w.proj, "", "b-00"); r.Code != 400 {
			t.Fatalf("empty message without files: %d", r.Code)
		}
		if r := w.post(bia, w.proj, "", "b-01", plan, plan); r.Code != 400 {
			t.Fatalf("the same file twice: %d", r.Code)
		}
		if r := bia.get("/files/" + plan + "/info"); r.Code != 200 || strings.Contains(r.Body.String(), "post_id") {
			t.Fatalf("file attached by a refused post: %s", r.Body.String())
		}
		posted := w.post(bia, w.proj, "", "b-1", plan, page)
		if posted.Code != 201 {
			t.Fatalf("post with files and no text: %d %s", posted.Code, posted.Body.String())
		}
		ids, _ := posted.body["file_ids"].([]any)
		meta, _ := posted.body["metadata"].(map[string]any)
		if len(ids) != 2 || ids[0] != plan || ids[1] != page || len(meta["files"].([]any)) != 2 {
			t.Fatalf("projection: %s", posted.Body.String())
		}
		ev := w.ev.last("posted")
		if v, ok := ev.Data["post"].(chat.PostView); !ok || len(v.FileIDs) != 2 || v.Metadata.Files[0].Name != "plan.xlsx" {
			t.Fatalf("posted event without the files: %+v", ev.Data["post"])
		}
		// a retry returns the stored message and claims nothing again
		again := w.post(bia, w.proj, "", "b-1", plan, page)
		if again.Code != 201 || again.body["id"] != posted.body["id"] || len(again.body["file_ids"].([]any)) != 2 {
			t.Fatalf("retry: %d %s", again.Code, again.Body.String())
		}
		// the same key for other files, or for the same in another order, is a conflict like another text
		for name, ids := range map[string][]string{"fewer": {plan}, "reordered": {page, plan}} {
			if r := w.post(bia, w.proj, "", "b-1", ids...); r.Code != 409 || r.code() != "pending_post_conflict" {
				t.Fatalf("retry with %s files: %d %s", name, r.Code, r.Body.String())
			}
		}
		if r := w.post(bia, w.proj, "again", "b-2", plan); r.Code != 400 {
			t.Fatalf("a file attached twice: %d", r.Code)
		}
		hist := items(t, w.call(ana, "GET", "/channels/"+w.proj+"/posts", nil, 200))
		if got, _ := hist[0]["file_ids"].([]any); len(got) != 2 {
			t.Fatalf("history without files: %v", hist[0])
		}
		// the text of a message with files may be removed, never of one without
		pid := posted.body["id"].(string)
		w.call(bia, "PATCH", "/posts/"+pid, map[string]string{"message": "the plan"}, 200)
		if r := w.call(bia, "PATCH", "/posts/"+pid, map[string]string{"message": ""}, 200); len(r.body["file_ids"].([]any)) != 2 {
			t.Fatalf("edit lost the files: %s", r.Body.String())
		}

		// downloads: members only, always an attachment in a sandbox
		r := ana.get("/files/" + page)
		h := r.Header()
		if r.Code != 200 || !bytes.Equal(r.Body.Bytes(), html) || h.Get("Content-Disposition") != `attachment; filename=passwd` ||
			h.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(h.Get("Content-Security-Policy"), "sandbox") ||
			h.Get("Content-Type") != "text/html" {
			t.Fatalf("html download: %d %v", r.Code, h)
		}
		if r := ana.get("/files/"+plan, "Range", "bytes=2-5"); r.Code != 206 || r.Body.String() != "\x03\x04 s" {
			t.Fatalf("range request: %d %q", r.Code, r.Body.String())
		}
		for name, c := range map[string]*client{"non-member": carl, "logged out": w.e.client()} {
			if r := c.get("/files/" + plan); r.Code != 404 && r.Code != 401 {
				t.Fatalf("%s downloads: %d", name, r.Code)
			}
			if r := c.get("/files/" + plan + "/info"); r.Code != 404 && r.Code != 401 {
				t.Fatalf("%s reads the info: %d", name, r.Code)
			}
		}
		if r := ana.get("/files/01J0000000000000000000NONE"); r.Code != 404 {
			t.Fatalf("unknown file: %d", r.Code)
		}

		// derived images
		img := infos(t, ana.uploadFiles(w.proj,
			upload{name: "photo.jpg", data: photo(t, 2000, 500, 1)}, upload{name: "bomb.png", data: bombPNG(30000)}))
		pic, bomb := img[0], img[1]
		if pic["has_preview_image"] != true || pic["width"] != float64(2000) || pic["height"] != float64(500) || pic["mime_type"] != "image/jpeg" {
			t.Fatalf("photo info: %v", pic)
		}
		if bomb["has_preview_image"] != false || bomb["mime_type"] != "image/png" {
			t.Fatalf("decompression bomb: %v", bomb)
		}
		picID := pic["id"].(string)
		if r := w.post(ana, w.proj, "photos", "a-2", picID, bomb["id"].(string), pdf); r.Code != 400 {
			t.Fatalf("bia's pdf in ana's post: %d", r.Code)
		}
		if r := w.post(ana, w.proj, "photos", "a-2", picID, bomb["id"].(string)); r.Code != 201 {
			t.Fatalf("post photos: %d %s", r.Code, r.Body.String())
		}
		for variant, want := range map[string]image.Point{"thumbnail": {X: 960, Y: 240}, "preview": {X: 1920, Y: 480}} {
			r := bia.get("/files/" + picID + "/" + variant)
			cfg, format, err := image.DecodeConfig(bytes.NewReader(r.Body.Bytes()))
			if r.Code != 200 || err != nil || format != "jpeg" || cfg.Width != want.X || cfg.Height != want.Y {
				t.Fatalf("%s: %d %v %s %dx%d", variant, r.Code, err, format, cfg.Width, cfg.Height)
			}
			if bytes.Contains(r.Body.Bytes(), []byte("GPS-SECRET")) || r.Header().Get("Content-Disposition") != "" ||
				r.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(r.Header().Get("Cache-Control"), "private") {
				t.Fatalf("%s leaks metadata or is not an inline private image: %v", variant, r.Header())
			}
			if r := carl.get("/files/" + picID + "/" + variant); r.Code != 404 {
				t.Fatalf("%s outside the channel: %d", variant, r.Code)
			}
		}
		if r := ana.get("/files/" + picID); !bytes.Contains(r.Body.Bytes(), []byte("GPS-SECRET")) {
			t.Fatal("the original is not the uploaded bytes")
		}
		// a photo the camera stored turned is shown upright: its size and derived images are the turned ones
		turned := infos(t, ana.uploadFiles(w.proj, upload{name: "portrait.jpg", data: photo(t, 1000, 500, 6)}))[0]
		if turned["width"] != float64(500) || turned["height"] != float64(1000) {
			t.Fatalf("turned photo info: %v", turned)
		}
		if cfg, _, err := image.DecodeConfig(ana.get("/files/" + turned["id"].(string) + "/thumbnail").Body); err != nil || cfg.Width != 120 || cfg.Height != 240 {
			t.Fatalf("turned thumbnail: %dx%d %v", cfg.Width, cfg.Height, err)
		}
		for _, id := range []string{plan, bomb["id"].(string)} {
			if r := ana.get("/files/" + id + "/thumbnail"); r.Code != 404 {
				t.Fatalf("thumbnail of a file without one: %d", r.Code)
			}
		}

		// lists: attached files only, newest first, each with its message
		list := items(t, w.call(bia, "GET", "/channels/"+w.proj+"/files", nil, 200))
		if len(list) != 4 || list[0]["name"] != "bomb.png" || list[3]["name"] != "plan.xlsx" || list[3]["post_id"] != pid || list[3]["user_id"] != w.biaID {
			t.Fatalf("channel files: %v", list)
		}
		first := w.call(bia, "GET", "/channels/"+w.proj+"/files?per_page=3", nil, 200)
		rest := items(t, w.call(bia, "GET", "/channels/"+w.proj+"/files?per_page=3&cursor="+first.body["next_cursor"].(string), nil, 200))
		if len(items(t, first)) != 3 || len(rest) != 1 || rest[0]["name"] != "plan.xlsx" {
			t.Fatalf("paged channel files: %v", rest)
		}
		w.call(carl, "GET", "/channels/"+w.priv+"/files", nil, 404)
		secret := infos(t, ana.uploadFiles(w.priv, upload{name: "Plan-ORÇAMENTO.txt", data: []byte("secret")}))[0]["id"].(string)
		if r := w.post(ana, w.priv, "", "a-3", secret); r.Code != 201 {
			t.Fatalf("post in private: %d", r.Code)
		}
		if mine := items(t, w.call(bia, "GET", "/users/me/files?q=PLAN", nil, 200)); len(mine) != 1 || mine[0]["name"] != "plan.xlsx" {
			t.Fatalf("bia's files named plan: %v", mine)
		}
		if mine := items(t, w.call(ana, "GET", "/users/me/files?q=PLAN", nil, 200)); len(mine) != 2 {
			t.Fatalf("ana's files named plan: %v", mine)
		}
		// case is ignored beyond ASCII, on every database
		if mine := items(t, w.call(ana, "GET", "/users/me/files?q=or%C3%A7amento", nil, 200)); len(mine) != 1 || mine[0]["name"] != "Plan-ORÇAMENTO.txt" {
			t.Fatalf("accented name in another case: %v", mine)
		}
		if mine := items(t, w.call(ana, "GET", "/users/me/files?q=100%25", nil, 200)); len(mine) != 0 {
			t.Fatalf("a filter with a wildcard character: %v", mine)
		}
		if mine := items(t, w.call(carl, "GET", "/users/me/files", nil, 200)); len(mine) != 0 {
			t.Fatalf("files of channels carl is not in: %v", mine)
		}

		// after leaving the channel its files are gone for that user
		w.call(bia, "DELETE", "/channels/"+w.proj+"/members/"+w.biaID, nil, 204)
		if r := bia.get("/files/" + plan); r.Code != 404 {
			t.Fatalf("download after leaving: %d", r.Code)
		}
		if mine := items(t, w.call(bia, "GET", "/users/me/files", nil, 200)); len(mine) != 0 {
			t.Fatalf("files after leaving: %v", mine)
		}

		// deleting a message deletes its files; a thread's root takes the replies' files along
		root := w.post(ana, w.side, "root", "a-4").body["id"].(string)
		inReply := infos(t, bia.uploadFiles(w.side, upload{name: "reply.txt", data: []byte("r")}))[0]["id"].(string)
		if r := bia.do("POST", "/posts", map[string]any{"channel_id": w.side, "root_id": root, "message": "see", "pending_post_id": "b-3", "file_ids": []string{inReply}}); r.Code != 201 {
			t.Fatalf("reply with a file: %d", r.Code)
		}
		w.call(ana, "DELETE", "/posts/"+root, nil, 204)
		w.call(ana, "DELETE", "/posts/"+pid, nil, 204) // moderation: bia's message with plan and passwd
		for _, id := range []string{plan, page, inReply} {
			if r := ana.get("/files/" + id); r.Code != 404 {
				t.Fatalf("file of a deleted message: %d", r.Code)
			}
		}
		if list := items(t, w.call(ana, "GET", "/channels/"+w.proj+"/files", nil, 200)); len(list) != 2 {
			t.Fatalf("channel files after the delete: %v", list)
		}
		for _, p := range items(t, w.call(ana, "GET", "/channels/"+w.proj+"/posts", nil, 200)) {
			if p["id"] == pid && (p["file_ids"] != nil || p["metadata"] != nil) {
				t.Fatalf("tombstone with files: %v", p)
			}
		}

		// cleanup: deleted files and abandoned uploads lose their bytes; a failing object waits for the next run
		// plan, passwd, pdf (unattached), photo + 2 derived, bomb, secret, reply, the turned photo + 2 derived
		// (unattached), and the three files stored before the fourth of an upload was refused (unattached too)
		before := storedFiles(t, dir)
		if before != 15 {
			t.Fatalf("%d objects stored", before)
		}
		var planKey string
		if err := db.Gorm.Model(&chat.FileInfo{}).Where("id = ?", plan).Pluck("path", &planKey).Error; err != nil {
			t.Fatal(err)
		}
		w.st.failDelete = planKey
		if n, err := w.svc.Cleanup(t.Context()); err == nil || n != 2 {
			t.Fatalf("cleanup with a failing object: %d %v", n, err)
		}
		w.st.failDelete = ""
		if n, err := w.svc.Cleanup(t.Context()); err != nil || n != 1 {
			t.Fatalf("the next run: %d %v", n, err)
		}
		if r := bia.get("/files/" + pdf); r.Code != 200 {
			t.Fatalf("a recent unattached upload was removed: %d", r.Code)
		}
		w.now = w.now.Add(25 * time.Hour)
		if n, err := w.svc.Cleanup(t.Context()); err != nil || n != 5 {
			t.Fatalf("abandoned uploads: %d %v", n, err)
		}
		if r := bia.get("/files/" + pdf); r.Code != 404 {
			t.Fatalf("abandoned upload after the cleanup: %d", r.Code)
		}
		if n := storedFiles(t, dir); n != 5 {
			t.Fatalf("%d objects after the cleanup, want 5", n)
		}
		var rows int64
		if db.Gorm.Model(&chat.FileInfo{}).Count(&rows); rows != 3 {
			t.Fatalf("%d file rows after the cleanup, want 3", rows)
		}

		// archived channel, rate limit, storage down
		w.call(ana, "POST", "/channels/"+w.side+"/archive", nil, 204)
		if r := ana.uploadFiles(w.side, upload{name: "a.txt", data: []byte("x")}); r.Code != 403 || r.code() != "channel_archived" {
			t.Fatalf("upload to an archived channel: %d %s", r.Code, r.Body.String())
		}
		w.st.down = true
		if r := ana.uploadFiles(w.proj, upload{name: "a.txt", data: []byte("x")}); r.Code != 503 || r.code() != "storage_unavailable" {
			t.Fatalf("upload with the storage down: %d %s", r.Code, r.Body.String())
		}
		if r := ana.get("/files/" + picID); r.Code != 503 {
			t.Fatalf("download with the storage down: %d", r.Code)
		}
		if r := w.post(ana, w.proj, "still chatting", "a-5"); r.Code != 201 {
			t.Fatalf("post with the storage down: %d", r.Code)
		}
		w.st.down = false
		w.svc.Limiter = denyAll{}
		if r := ana.uploadFiles(w.proj, upload{name: "a.txt", data: []byte("x")}); r.Code != 429 || r.Header().Get("Retry-After") == "" {
			t.Fatalf("rate limited upload: %d", r.Code)
		}
	})
}

// TestFilesOnS3 runs the upload, derive, download and cleanup path on a bucket.
func TestFilesOnS3(t *testing.T) {
	srv := testenv.S3(t)
	st, err := files.NewS3(config.FilesS3T{
		Endpoint: srv.Endpoint, Bucket: srv.NewBucket(t), Region: "us-east-1", AccessKey: srv.AccessKey, SecretKey: srv.SecretKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	w := newFileWorld(t, dbtest.Open(t, config.DriverSQLite), st)
	if r := w.ana.uploadFiles(w.proj, upload{name: "big.bin", data: make([]byte, 1<<20+1)}); r.Code != 413 {
		t.Fatalf("too large: %d %s", r.Code, r.Body.String())
	}
	up := infos(t, w.ana.uploadFiles(w.proj, upload{name: "photo.jpg", data: photo(t, 640, 480, 1)}, upload{name: "notes.txt", data: []byte("hello s3")}))
	pic, notes := up[0]["id"].(string), up[1]["id"].(string)
	if up[0]["has_preview_image"] != true {
		t.Fatalf("photo: %v", up[0])
	}
	post := w.post(w.ana, w.proj, "", "a-1", pic, notes)
	if post.Code != 201 {
		t.Fatalf("post: %d %s", post.Code, post.Body.String())
	}
	if r := w.bia.get("/files/"+notes, "Range", "bytes=6-7"); r.Code != 206 || r.Body.String() != "s3" {
		t.Fatalf("range from the bucket: %d %q", r.Code, r.Body.String())
	}
	if r := w.bia.get("/files/" + pic + "/thumbnail"); r.Code != 200 || r.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumbnail from the bucket: %d", r.Code)
	}
	w.call(w.ana, "DELETE", "/posts/"+post.body["id"].(string), nil, 204)
	if n, err := w.svc.Cleanup(t.Context()); err != nil || n != 2 {
		t.Fatalf("cleanup: %d %v", n, err)
	}
	var keys []string
	if err := errors.Join(w.e.db.Gorm.Model(&chat.FileInfo{}).Pluck("path", &keys).Error); err != nil || len(keys) != 0 {
		t.Fatalf("rows left: %v %v", keys, err)
	}
}
