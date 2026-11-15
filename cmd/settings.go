package cmd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	var supervise bool
	var confirmWindow time.Duration
	cmd := &cobra.Command{
		Use:   "settings-applier",
		Short: "Apply the changes made in Administration → Settings (as root, from meethub-settings.service)",
		Long: "Carries out the change the server left in its mailbox (<data_dir>/settings/request.json): checks it again,\n" +
			"keeps the previous config.toml in <config dir>/settings-state, replaces the file, restarts the server and\n" +
			"waits for the owner's confirmation; without it within two minutes the previous file is put back.\n" +
			"First it finishes or undoes a transaction a stop left unfinished. One transaction per run.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if supervise && seed != "" {
				if err := seedConfig(seed, c.cfgFile); err != nil {
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
			cfg, err := config.LoadUnvalidated(file, os.LookupEnv)
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
				return a.Run(ctx)
			}
			a.ConfirmWindow = confirmWindow
			return c.superviseServer(ctx, a, probe, file)
		},
	}
	cmd.Flags().StringVar(&requests, "requests", "", "the server's mailbox (default <app.data_dir>/settings)")
	cmd.Flags().StringVar(&state, "state", "", "the applier's own directory, root's (default <config dir>/settings-state)")
	cmd.Flags().StringVar(&unit, "unit", "meethub.service", "the systemd unit of the server")
	// shortcut: --supervise runs the server as a child instead of under systemd; only the e2e stack uses it
	cmd.Flags().BoolVar(&supervise, "supervise", false, "run the server as a child process and apply in a loop (tests)")
	cmd.Flags().DurationVar(&confirmWindow, "confirm-window", 0, "with --supervise: the confirmation window (tests)")
	cmd.Flags().StringVar(&seed, "seed-config", "", "with --supervise: copy this file to --config when it is missing (tests)")
	for _, f := range []string{"supervise", "confirm-window", "seed-config"} {
		_ = cmd.Flags().MarkHidden(f)
	}
	return cmd
}

// seedConfig copies seed to file when file does not exist yet (a writable copy for the e2e stack).
func seedConfig(seed, file string) error {
	if file == "" {
		return errors.New("--seed-config needs --config")
	}
	if _, err := os.Stat(file); err == nil {
		return nil
	}
	b, err := os.ReadFile(seed) //nolint:gosec // the operator's flag
	if err != nil {
		return err
	}
	return os.WriteFile(file, b, 0o644) //nolint:gosec // config.toml is not a secret
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
	args   []string
	mu     sync.Mutex
	cmd    *exec.Cmd
	exited chan struct{}
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
	return s.start()
}

// superviseServer runs the server as a child and the applier every second, until ctx ends.
func (c *cli) superviseServer(ctx context.Context, a *settings.Applier, probe *readyProbe, file string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	child := &childServer{readyProbe: probe, args: []string{self, "--config", file, "server"}}
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
