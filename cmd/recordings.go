package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"

	"github.com/jniltinho/meethub/internal/ai"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/files"
	"github.com/jniltinho/meethub/internal/jobs"
	"github.com/jniltinho/meethub/internal/meetings"
	"github.com/jniltinho/meethub/internal/recordings"
	"github.com/jniltinho/meethub/internal/summaries"
)

// LifecycleQueue is the queue of the recordings' sweeper and retention, served by its own workers (design D9).
const LifecycleQueue = "meethub:lifecycle"

// newRecordings wires meeting recordings; when they are on it opens the capture volume and runs the startup probe,
// whose failure stops the server naming the setting to fix (design D10).
func newRecordings(ctx context.Context, cfg *config.Config, db *database.DB, chatSvc *chat.Service, meet *meetings.Service,
	storage files.Storage, rdb *redis.Client, logger *slog.Logger,
) (*recordings.Service, error) {
	svc := &recordings.Service{Config: cfg.Recordings, Logger: logger}
	if !cfg.Recordings.Enabled {
		return svc, nil
	}
	lk := cfg.LiveKit
	svc.DB, svc.Chat, svc.Meetings, svc.Storage, svc.Redis = db, chatSvc, meet, storage, rdb
	svc.Rooms = meetings.NewLiveKitRooms(lk.ServerURL(), lk.APIKey, lk.APISecret)
	svc.Dir, svc.APIURL, svc.APIKey, svc.Secret = cfg.RecordingsDir(), lk.ServerURL(), lk.APIKey, lk.APISecret
	svc.FFmpeg = &recordings.FFmpeg{Path: cfg.Recordings.FFmpegPath}
	if err := svc.Prepare(ctx); err != nil {
		return nil, fmt.Errorf("recordings: %w", err)
	}
	logger.Info("recordings on", "mode", cfg.Recordings.Mode, "dir", svc.Dir, "volume", svc.Volume())
	return svc, nil
}

// newSummaries wires meeting summaries (add-meeting-summaries). They are available when AI is configured and
// recordings are on; then recordings keep the setting in force at their start and cut summary sources.
func newSummaries(ctx context.Context, cfg *config.Config, db *database.DB, chatSvc *chat.Service, rec *recordings.Service, storage files.Storage,
	rdb *redis.Client, logger *slog.Logger,
) *summaries.Service {
	a := cfg.AI
	svc := &summaries.Service{DB: db, Chat: chatSvc, Recordings: rec, Storage: storage, Config: a, Redis: rdb, Logger: logger,
		FFmpeg: rec.FFmpeg, Dir: rec.Dir, SecretKey: cfg.Secret.Bytes(),
	}
	// the keys are read at every request: one entered in Administration is used without a restart
	svc.Speech = &ai.Client{BaseURL: a.TranscriptionURL(), KeyFunc: svc.SpeechKey, Timeout: a.Timeout.Std()}
	svc.Writer = &ai.Client{BaseURL: a.BaseURL, KeyFunc: svc.WriterKey, Timeout: a.Timeout.Std()}
	if rec.DB == nil { // recordings off: the summaries' cleanup still reaches the objects' inventory
		rec.DB, rec.Storage = db, storage
	}
	if !rec.Enabled() {
		if a.Configured() {
			logger.Warn("summaries: AI is configured but recordings are off: meeting summaries are unavailable")
		}
		return svc
	}
	// wired whenever recordings are on: a key entered later makes summaries available while the server runs
	rec.Policy, rec.PieceBytes = svc.Policy, a.MaxUploadBytes/4
	rec.OnQueued = func(ctx context.Context, id string) { svc.Enqueue(ctx, id, ulid.Make().String()) }
	logger.Info("summaries", "available", svc.Ready(ctx), "transcription_model", a.TranscriptionModel, "summary_model", a.SummaryModel)
	return svc
}

// summaryTimeout bounds one attempt of a summary: four times the longest recording, at least two hours.
func summaryTimeout(cfg *config.Config) time.Duration {
	return max(4*time.Duration(cfg.Recordings.MaxMinutes)*time.Minute, 2*time.Hour)
}

// lifecycleTasks are the periodic tasks of recordings.
func lifecycleTasks(svc *recordings.Service, logger *slog.Logger) []jobs.TaskType {
	return []jobs.TaskType{
		{Name: "recordings:sweep", MaxAttempts: 1, Period: recordings.SweepEvery, Timeout: recordings.SweepEvery,
			Handler: func(ctx context.Context, _ *asynq.Task) error { return svc.Sweep(ctx) }},
		{Name: "recordings:retention", MaxAttempts: 3, Period: 24 * time.Hour, Timeout: 30 * time.Minute,
			Handler: func(ctx context.Context, _ *asynq.Task) error {
				err := svc.Expire(ctx)
				if err != nil {
					logger.WarnContext(ctx, "recordings: retention", "error", err)
				}
				return err
			}},
	}
}

func newRecordingsCmd(c *cli) *cobra.Command {
	cmd := &cobra.Command{Use: "recordings", Short: "Check the ffmpeg and the capture directory used by meeting recordings", Args: cobra.NoArgs}
	var mode string
	probe := &cobra.Command{
		Use:   "probe",
		Short: "Compose a short generated recording with the configured ffmpeg, as the server does at startup",
		Long: "Generates a two-second capture (two voices and, in video mode, a camera) in recordings.dir and composes\n" +
			"it with recordings.ffmpeg_path exactly as a recording is composed. --mode all checks both modes.\n" +
			"Exits 0 only when every mode produced a playable file.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := c.config()
			if err != nil {
				return err
			}
			r := cfg.Recordings
			modes := []string{r.Mode}
			switch mode {
			case "":
			case "all":
				modes = []string{config.RecordAudio, config.RecordVideo}
			case config.RecordAudio, config.RecordVideo:
				modes = []string{mode}
			default:
				return errors.New("--mode must be audio, video or all")
			}
			ff := &recordings.FFmpeg{Path: r.FFmpegPath}
			for _, m := range modes {
				if err := recordings.Probe(cmd.Context(), ff, cfg.RecordingsDir(), m, r.VideoFormat); err != nil {
					return fmt.Errorf("%s: %w", m, err)
				}
				_, _ = fmt.Fprintf(c.out, "%s: ok\n", m)
			}
			return nil
		},
	}
	probe.Flags().StringVar(&mode, "mode", "", "audio, video or all (default: recordings.mode)")
	cmd.AddCommand(probe)
	return cmd
}
