package cmd

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jniltinho/meethub/internal/buildinfo"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/settings"
)

func newSettingsApplierCmd(c *cli) *cobra.Command {
	var requests, state, unit, seed string
	var supervise, recoverOnly bool
	var confirmWindow time.Duration
	var maxPerHour int
	cmd := &cobra.Command{
		Use:   "settings-applier",
		Short: "Apply the changes made in Administration → Settings (as root, from meethub-settings.service)",
		Long: "Carries out the change the server left in its mailbox (<data_dir>/settings/request.json): checks it again,\n" +
			"keeps the previous config.toml in <config dir>/settings-state, replaces the file, restarts the server and\n" +
			"waits for the owner's confirmation; without it within two minutes the previous file is put back.\n" +
			"First it finishes or undoes a transaction a stop left unfinished. One transaction per run.\n\n" +
			"With --supervise (container installs, opt-in) it runs the server itself, as a child process, and keeps\n" +
			"applying: --config names the file the server runs with, created at the first start from --seed-config.\n" +
			"The applier is then the server's own user, so nothing keeps a server taken over from that file.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if supervise {
				if c.cfgFile == "" {
					return errors.New("--supervise needs --config: the file the server runs with")
				}
				if seed == "" { // the file the server would read by itself (the all-in-one image: one in /data, or mounted)
					found, err := config.FindFile("")
					if err != nil {
						return err
					}
					seed = found
				}
				if err := prepareConfig(seed, c.cfgFile, slog.New(slog.NewTextHandler(c.errOut, nil))); err != nil {
					return err
				}
			}
			file, err := config.FindFile(c.cfgFile)
			if err != nil {
				return err
			}
			if file == "" {
				return errors.New("no config.toml: give its path with --config")
			}
			// only the address and TLS are needed: no secret is read through its _FILE variable
			secretFiles := map[string]bool{}
			for _, f := range config.Default().Fields() {
				secretFiles[f.Env+"_FILE"] = f.Secret
			}
			cfg, err := config.LoadUnvalidated(file, func(k string) (string, bool) {
				if secretFiles[k] {
					return "", false
				}
				return os.LookupEnv(k)
			})
			if err != nil {
				return err
			}
			if requests == "" {
				requests = filepath.Join(cfg.App.DataDir, "settings")
			}
			if state == "" {
				state = filepath.Join(filepath.Dir(file), settings.StateDirName)
			}
			if requests, err = filepath.Abs(requests); err != nil {
				return err
			}
			if state, err = filepath.Abs(state); err != nil {
				return err
			}
			probe := &readyProbe{cfg: cfg}
			a := &settings.Applier{Config: file, Requests: requests, State: state, Version: buildinfo.Version,
				Logger: slog.New(slog.NewTextHandler(c.errOut, nil))}
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			if !supervise {
				a.Server = &systemdServer{unit: unit, readyProbe: probe}
				if recoverOnly {
					return a.Recover(ctx)
				}
				return a.Run(ctx)
			}
			a.ConfirmWindow, a.MaxPerHour = confirmWindow, maxPerHour
			return c.superviseServer(ctx, a, probe, file)
		},
	}
	cmd.Flags().StringVar(&requests, "requests", "", "the server's mailbox (default <app.data_dir>/settings)")
	cmd.Flags().StringVar(&state, "state", "", "the applier's own directory, root's (default <config dir>/settings-state)")
	cmd.Flags().StringVar(&unit, "unit", "meethub.service", "the systemd unit of the server")
	cmd.Flags().BoolVar(&recoverOnly, "recover", false, "only finish or undo an interrupted change, leave any request (package scripts)")
	cmd.Flags().BoolVar(&supervise, "supervise", false, "run the server as a child process and keep applying (container installs)")
	cmd.Flags().StringVar(&seed, "seed-config", "", "with --supervise: the file --config starts as, when it does not exist yet (default: the config.toml the server would find)")
	cmd.Flags().DurationVar(&confirmWindow, "confirm-window", 0, "with --supervise: the confirmation window (tests)")
	cmd.Flags().IntVar(&maxPerHour, "max-per-hour", 0, "with --supervise: changes applied per hour (tests; default 3)")
	for _, f := range []string{"confirm-window", "max-per-hour"} {
		_ = cmd.Flags().MarkHidden(f)
	}
	return cmd
}

// prepareConfig makes sure the file a supervised server runs with exists: its directory is created and a missing
// file starts as a copy of seed, or empty without one (an install configured by variables alone). The file is then
// the configuration: a seed that changed afterwards is only reported, since the file holds the changes made since.
func prepareConfig(seed, file string, logger *slog.Logger) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		return err
	}
	var b []byte
	if seed != "" {
		var err error
		if b, err = os.ReadFile(seed); err != nil && !errors.Is(err, fs.ErrNotExist) { //nolint:gosec // the operator's flag
			return err
		}
	}
	sum := fmt.Sprintf("%x\n", sha256.Sum256(b))
	mark := file + ".seed" // the checksum of the seed the file started as
	if _, err := os.Stat(file); err == nil {
		if was, err := os.ReadFile(mark); err == nil && string(was) != sum { //nolint:gosec // beside the operator's --config
			logger.Warn("settings: the seed changed after it was copied and is not read again: edit the file in use, or delete it to start again from the seed",
				"seed", seed, "in_use", file)
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// whole or not at all: a copy cut short (a full disk) must not be taken for the configuration at the next start
	tmp := file + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644) //nolint:gosec // config.toml is not a secret
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.WriteFile(mark, []byte(sum), 0o644) //nolint:gosec // a checksum
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, file)
}

// readyProbe asks the local server's /api/v1/ready for its boot id, at the address the health probe uses. Over
// HTTPS the certificate is not checked: the applier runs as root and does not read files of the service's data
// directory (a self-signed certificate lives there), and the answer only tells which process is ready.
type readyProbe struct{ cfg *config.Config }

func (p *readyProbe) BootID(ctx context.Context) (string, error) {
	var tlsConfig *tls.Config
	if cert, _ := p.cfg.TLSFiles(); cert != "" {
		tlsConfig = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12} //nolint:gosec // readiness only, nothing sent
	}
	u, err := healthURL(p.cfg.HTTP.Addr, tlsConfig != nil)
	if err != nil {
		return "", err
	}
	u = strings.TrimSuffix(u, "/health") + "/ready"
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil, TLSClientConfig: tlsConfig}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		BootID string `json:"boot_id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil || resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ready: status %s", resp.Status)
	}
	return body.BootID, nil
}

// systemdServer restarts the server's unit.
type systemdServer struct {
	unit string
	*readyProbe
}

func (s *systemdServer) Restart(ctx context.Context) error {
	if out, err := exec.CommandContext(ctx, "systemctl", "restart", s.unit).CombinedOutput(); err != nil { //nolint:gosec // the unit is the operator's flag
		return fmt.Errorf("systemctl restart %s: %w: %s", s.unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// childServer runs `meethub server` as a child process and restarts it when it exits (as Restart=on-failure).
type childServer struct {
	*readyProbe
	stopping context.Context // done once the applier is told to stop
	args     []string
	mu       sync.Mutex
	cmd      *exec.Cmd
	exited   chan struct{}
}

func (s *childServer) start() error {
	cmd := exec.Command(s.args[0], s.args[1:]...) //nolint:gosec,noctx // this binary; stopped by stop()
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	s.cmd, s.exited = cmd, exited
	return nil
}

func (s *childServer) stop() {
	if s.cmd == nil {
		return
	}
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-s.exited:
	case <-time.After(45 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.exited
	}
}

func (s *childServer) Restart(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stop()
	// the container is stopping: a change being undone has its file back, and the next start runs with it
	if s.stopping.Err() != nil {
		return errors.New("the applier is stopping: the server is not started again")
	}
	return s.start()
}

// superviseServer runs the server as a child and the applier every second, until ctx ends.
func (c *cli) superviseServer(ctx context.Context, a *settings.Applier, probe *readyProbe, file string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	child := &childServer{readyProbe: probe, stopping: ctx, args: []string{self, "--config", file, "server"}}
	a.Server = child
	child.mu.Lock()
	err = child.start()
	child.mu.Unlock()
	if err != nil {
		return err
	}
	defer func() { child.mu.Lock(); child.stop(); child.mu.Unlock() }()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		if err := a.Run(ctx); err != nil {
			_, _ = fmt.Fprintln(c.errOut, "settings applier:", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		child.mu.Lock()
		select {
		case <-child.exited: // it stopped by itself: start it again, as systemd would
			err = child.start()
		default:
		}
		child.mu.Unlock()
		if err != nil {
			return err
		}
	}
}
