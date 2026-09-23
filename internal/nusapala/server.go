//go:build nusapala && linux && cgo

package nusapala

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

//go:embed index.html
var page []byte

func Run(ctx context.Context, cfg Config, logger *log.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	status := NewStatus(logger)
	status.Event("system", "mode nusapala: PortAudio + gousb + go-escpos + go-paymentreader")
	var audio *Audio
	if cfg.Audio.Enabled {
		var err error
		audio, err = OpenAudio(ctx, cfg.Audio, status)
		if err != nil {
			status.Result("audio", err, "")
		} else {
			defer audio.Close()
		}
	} else {
		status.Set("audio", "DISABLED", "")
	}
	// Match Nusapala: persistent audio starts before QR and STI.
	peripherals := NewPeripherals(cfg, status)
	peripherals.Start(ctx)
	intercom := NewIntercom(ctx, audio, status)
	defer intercom.Close()
	status.Set("intercom", "IDLE", "belum ada panggilan")
	server := &http.Server{Addr: cfg.Listen, Handler: newHandler(intercom, peripherals, status), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8192}
	done := make(chan error, 1)
	go func() {
		if cfg.TLSCert != "" {
			done <- server.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			done <- server.ListenAndServe()
		}
	}()
	status.Event("web", fmt.Sprintf("listen=%s; laptop: SSH tunnel lalu buka http://localhost:8080", cfg.Listen))
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		return server.Shutdown(shutdown)
	}
}
func newHandler(intercom *Intercom, p *Peripherals, status *Status) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, status.Snapshot()) })
	mux.HandleFunc("POST /api/call", func(w http.ResponseWriter, r *http.Request) {
		var offer webrtc.SessionDescription
		if !readJSON(w, r, &offer) {
			return
		}
		answer, id, err := intercom.Offer(r.Context(), offer)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, struct {
			Answer webrtc.SessionDescription `json:"answer"`
			ID     string                    `json:"id"`
		}{answer, id})
	})
	for _, action := range []string{"ping", "hangup"} {
		action := action
		mux.HandleFunc("POST /api/call/"+action, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				ID string `json:"id"`
			}
			if !readJSON(w, r, &body) {
				return
			}
			var ok bool
			if action == "ping" {
				ok = intercom.Ping(body.ID)
			} else {
				ok = intercom.Hangup(body.ID)
			}
			if !ok {
				http.Error(w, "session tidak ditemukan", http.StatusNotFound)
				return
			}
			writeJSON(w, map[string]bool{"ok": true})
		})
	}
	mux.HandleFunc("POST /api/print", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		if err := p.QueuePrint(body.Text); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			if e != nil || u.Host != r.Host || u.Scheme != scheme {
				http.Error(w, "cross-origin request ditolak", http.StatusForbidden)
				return
			}
		}
		if r.Method == http.MethodPost && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "gunakan application/json", http.StatusUnsupportedMediaType)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		http.Error(w, "JSON tidak valid: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
