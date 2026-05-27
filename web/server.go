package web

import (
	"context"
	"fmt"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

// Config holds runtime options for the HTTP server.
type Config struct {
	Listen         string
	Provider       StateProvider
	WifiInterface  string // default wlan0
	WifiUseSudo    bool   // default true
}

// Server is the HTTP API + static UI for phev2mqtt.
type Server struct {
	cfg      Config
	provider StateProvider
	creds    *credentials
	sessions *sessionStore
	wifi     *WifiManager
	mux      *http.ServeMux
	httpSrv  *http.Server
}

// NewServer constructs a Server with credentials loaded from viper. It does
// not start listening; call Run for that.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("web: Provider is required")
	}
	if cfg.Listen == "" {
		cfg.Listen = ":8888"
	}
	if cfg.WifiInterface == "" {
		cfg.WifiInterface = "wlan0"
	}
	creds, err := loadCredentials()
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:      cfg,
		provider: cfg.Provider,
		creds:    creds,
		sessions: newSessionStore(),
		wifi: &WifiManager{
			Iface:   cfg.WifiInterface,
			UseSudo: cfg.WifiUseSudo,
		},
	}
	s.mux = s.buildMux()
	s.httpSrv = &http.Server{
		Addr:              cfg.Listen,
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s, nil
}

func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/healthz", s.handleHealth)

	// Auth: login is unauthenticated; everything else under /api/ requires
	// a valid session cookie.
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("/api/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("/api/password", s.requireAuth(s.handlePassword))

	mux.HandleFunc("/api/status", s.requireAuth(s.handleStatus))
	mux.HandleFunc("/api/climate/start", s.requireAuth(s.handleClimateStart))
	mux.HandleFunc("/api/climate/stop", s.requireAuth(s.handleClimateStop))
	mux.HandleFunc("/api/lights/parking", s.requireAuth(s.handleLightsParking))
	mux.HandleFunc("/api/lights/headlights", s.requireAuth(s.handleLightsHeadlights))
	mux.HandleFunc("/api/charging/cancel-timer", s.requireAuth(s.handleCancelChargeTimer))
	mux.HandleFunc("/api/reconnect/mqtt", s.requireAuth(s.handleReconnectMQTT))
	mux.HandleFunc("/api/reconnect/phev", s.requireAuth(s.handleReconnectPhev))
	mux.HandleFunc("/api/climate/timers/", s.requireAuth(s.handleClimateTimer))
	mux.HandleFunc("/api/gps/enabled", s.requireAuth(s.handleGPSEnabled))
	mux.HandleFunc("/api/bridge/paused", s.requireAuth(s.handleBridgePaused))
	mux.HandleFunc("/api/system/reboot", s.requireAuth(s.handleReboot))
	mux.HandleFunc("/api/wifi/status", s.requireAuth(s.handleWifiStatus))
	mux.HandleFunc("/api/wifi/scan", s.requireAuth(s.handleWifiScan))
	mux.HandleFunc("/api/wifi/connect", s.requireAuth(s.handleWifiConnect))
	mux.HandleFunc("/api/wifi/saved", s.requireAuth(s.handleWifiSaved))
	mux.HandleFunc("/api/wifi/forget", s.requireAuth(s.handleWifiForget))

	return mux
}

// Run starts the HTTP listener. It returns when ctx is cancelled (after a
// graceful shutdown) or the listener fails.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		log.Infof("web UI listening on %s", s.cfg.Listen)
		if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}
