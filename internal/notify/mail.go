package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	textTemplate "text/template"
	"time"
	"unicode/utf8"

	"github.com/jniltinho/meethub/internal/org"
)

// Strings are the English texts of the e-mails. Their translations live with
// the SPA's (frontend/src/i18n/email.ts lists them; the build writes them to
// i18n/email.json in the embedded SPA, which LoadCatalog reads): translations
// exist only in the frontend catalogs.
var Strings = []string{
	"{name} wrote to you in MeetHub",
	"{n} new messages for you in MeetHub",
	"Messages for you while you were away",
	"{name} in {place}",
	"a direct message",
	"a group conversation",
	"And {n} more.",
	"Open in MeetHub",
	"You receive this e-mail because you were sent a direct message or mentioned while away.",
	"Turn off these e-mails",
}

// Catalog holds the e-mail translations: language → English text → translation.
type Catalog map[string]map[string]string

// LoadCatalog reads i18n/email.json from the SPA; without it (a build without the
// SPA, the tests) e-mails are in English.
func LoadCatalog(dist fs.FS) Catalog {
	b, err := fs.ReadFile(dist, "i18n/email.json")
	if err != nil {
		return nil
	}
	var c Catalog
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	return c
}

// T translates an e-mail text and fills its {placeholders}.
func (c Catalog) T(lang, key string, args ...string) string {
	s := key
	if tr, ok := c[lang][key]; ok && tr != "" {
		s = tr
	}
	for i := 0; i+1 < len(args); i += 2 {
		s = strings.ReplaceAll(s, "{"+args[i]+"}", args[i+1])
	}
	return s
}

// line is one message of an e-mail.
type line struct {
	Who, Excerpt, Link string
}

type mailData struct {
	Org, Title, Footer, Unsubscribe, UnsubscribeLabel, More, OpenLabel string
	Lines                                                              []line
}

var htmlBody = template.Must(template.New("html").Parse(`<!doctype html>
<html><body style="margin:0;padding:24px;background:#f4f5f7;font-family:system-ui,sans-serif;color:#1b1f2a">
<div style="max-width:560px;margin:0 auto;background:#ffffff;border-radius:8px;padding:24px">
<p style="margin:0 0 4px;font-size:13px;color:#4a5160">{{.Org}}</p>
<h1 style="margin:0 0 16px;font-size:18px">{{.Title}}</h1>
{{range .Lines}}<div style="margin:0 0 16px">
<p style="margin:0;font-weight:600">{{.Who}}</p>
{{if .Excerpt}}<p style="margin:4px 0;white-space:pre-wrap">{{.Excerpt}}</p>{{end}}
<p style="margin:4px 0"><a href="{{.Link}}" style="color:#1d4fd8">{{$.OpenLabel}}</a></p>
</div>{{end}}
{{if .More}}<p>{{.More}}</p>{{end}}
<p style="margin:24px 0 0;font-size:12px;color:#4a5160">{{.Footer}} <a href="{{.Unsubscribe}}" style="color:#1d4fd8">{{.UnsubscribeLabel}}</a></p>
</div></body></html>
`))

var textBody = textTemplate.Must(textTemplate.New("text").Parse(`{{.Title}}
{{range .Lines}}
{{.Who}}
{{if .Excerpt}}{{.Excerpt}}
{{end}}{{$.OpenLabel}}: {{.Link}}
{{end}}{{if .More}}
{{.More}}
{{end}}
--
{{.Footer}}
{{.UnsubscribeLabel}}: {{.Unsubscribe}}
`))

// compose builds the e-mail: headers, then a text and an HTML part.
func (s *Service) compose(ctx context.Context, to, lang string, items []pending, token string, now time.Time) ([]byte, error) {
	o, err := org.Get(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(s.PublicURL, "/")
	d := mailData{
		Org:              o.Name,
		Title:            s.Catalog.T(lang, "Messages for you while you were away"),
		Footer:           s.Catalog.T(lang, "You receive this e-mail because you were sent a direct message or mentioned while away."),
		UnsubscribeLabel: s.Catalog.T(lang, "Turn off these e-mails"),
		OpenLabel:        s.Catalog.T(lang, "Open in MeetHub"),
		Unsubscribe:      base + "/unsubscribe?token=" + token,
	}
	for i, it := range items {
		if i == MaxListed {
			d.More = s.Catalog.T(lang, "And {n} more.", "n", strconv.Itoa(len(items)-MaxListed))
			break
		}
		place := "#" + it.ChannelName
		switch it.ChannelType {
		case "D":
			place = s.Catalog.T(lang, "a direct message")
		case "G":
			place = s.Catalog.T(lang, "a group conversation")
		}
		l := line{Who: s.Catalog.T(lang, "{name} in {place}", "name", it.AuthorName, "place", place), Link: base + "/pl/" + it.PostID}
		if s.Excerpt {
			l.Excerpt = excerpt(it.Message)
		}
		d.Lines = append(d.Lines, l)
	}
	subject := s.Catalog.T(lang, "{n} new messages for you in MeetHub", "n", strconv.Itoa(len(items)))
	if len(items) == 1 {
		subject = s.Catalog.T(lang, "{name} wrote to you in MeetHub", "name", items[0].AuthorName)
	}
	var text, html bytes.Buffer
	if err := textBody.Execute(&text, d); err != nil {
		return nil, err
	}
	if err := htmlBody.Execute(&html, d); err != nil {
		return nil, err
	}
	return s.message(to, subject, d.Unsubscribe, base, token, now, text.Bytes(), html.Bytes())
}

// excerpt is the start of a message: its first lines up to excerptMax runes.
func excerpt(msg string) string {
	msg = strings.TrimSpace(msg)
	if utf8.RuneCountInString(msg) <= excerptMax {
		return msg
	}
	r := []rune(msg)
	return strings.TrimSpace(string(r[:excerptMax])) + "…"
}

func (s *Service) message(to, subject, unsubscribePage, base, token string, now time.Time, text, html []byte) ([]byte, error) {
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return nil, fmt.Errorf("notify: from: %w", err)
	}
	rnd := make([]byte, 12)
	if _, err := rand.Read(rnd); err != nil {
		return nil, err
	}
	boundary := "meethub-" + hex.EncodeToString(rnd)
	host := "meethub"
	if u, err := url.Parse(base); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	_, domain, _ := strings.Cut(from.Address, "@")
	if domain == "" {
		domain = host
	}
	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", from.String())
	header("To", (&mail.Address{Address: to}).String())
	header("Subject", mime.QEncoding.Encode("utf-8", subject))
	header("Date", now.UTC().Format(time.RFC1123Z))
	header("Message-ID", "<"+hex.EncodeToString(rnd)+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Auto-Submitted", "auto-generated")
	// one-click unsubscribe (RFC 8058) posts to the API; the page of the link in the body asks first
	header("List-Unsubscribe", "<"+base+"/api/v1/notifications/unsubscribe?token="+token+">, <"+unsubscribePage+">")
	header("List-Unsubscribe-Post", "List-Unsubscribe=One-Click")
	header("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	for _, part := range []struct {
		ctype string
		body  []byte
	}{{"text/plain; charset=utf-8", text}, {"text/html; charset=utf-8", html}} {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary, part.ctype)
		w := quotedprintable.NewWriter(&b)
		if _, err := w.Write(bytes.ReplaceAll(part.body, []byte("\n"), []byte("\r\n"))); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes(), nil
}
