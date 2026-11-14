package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/meetings"
)

// newMeetings wires the meetings service; without LiveKit settings it is disabled (its routes answer 503).
func newMeetings(cfg *config.Config, db *database.DB, chatSvc *chat.Service, pub chat.Publisher, logger *slog.Logger) *meetings.Service {
	lk := cfg.LiveKit
	svc := &meetings.Service{DB: db, Chat: chatSvc, Publisher: pub, Logger: logger}
	if lk.Enabled() {
		svc.URL, svc.APIKey, svc.APISecret = lk.URL, lk.APIKey, lk.APISecret
		svc.Rooms = meetings.NewLiveKitRooms(lk.ServerURL(), lk.APIKey, lk.APISecret)
	}
	return svc
}

func newMeetingsCmd(c *cli) *cobra.Command {
	cmd := &cobra.Command{Use: "meetings", Short: "Check the LiveKit server used by meetings", Args: cobra.NoArgs}
	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Report whether LiveKit accepts the key, the browser URL and whether TURN allocations succeed",
		Long: "Calls LiveKit's room API (livekit.api_url) with the configured key, joins a throwaway room over its\n" +
			"signalling to get the TURN servers it hands browsers, and requests a TURN allocation on each. Exits 0 only\n" +
			"when every check passes. A TURN server on a loopback address is tried at livekit.api_url's host. The key,\n" +
			"the secret and the tokens are never printed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := c.config()
			if err != nil {
				return err
			}
			lk := cfg.LiveKit
			if !lk.Enabled() {
				return errors.New("meetings are off: livekit.url is not set (MEETHUB_LIVEKIT_URL)")
			}
			results := meetings.Check(cmd.Context(), lk.ServerURL(), lk.APIKey, lk.APISecret)
			failed := 0
			for _, r := range results {
				if !r.OK {
					failed++
				}
			}
			if c.asJSON {
				err = c.printJSON(map[string]any{"browser_url": lk.URL, "checks": results, "ok": failed == 0})
			} else {
				tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
				_, _ = fmt.Fprintf(tw, "Browser URL\t\t%s\t\n", lk.URL)
				for _, r := range results {
					status := "ok"
					if !r.OK {
						status = "FAIL"
					}
					_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Name, status, r.Target, r.Detail)
				}
				err = tw.Flush()
			}
			if err != nil {
				return err
			}
			if failed > 0 {
				return fmt.Errorf("meetings check: %d of %d checks failed", failed, len(results))
			}
			return nil
		},
	})
	return cmd
}
