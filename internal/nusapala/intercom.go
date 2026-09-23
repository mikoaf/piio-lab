//go:build nusapala && linux && cgo

package nusapala

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"layeh.com/gopus"
)

type call struct {
	id       string
	pc       *webrtc.PeerConnection
	route    *audioRoute
	ctx      context.Context
	cancel   context.CancelFunc
	lastPing atomic.Int64
	ending   bool
	done     chan struct{}
}
type Intercom struct {
	mu      sync.Mutex
	current *call
	audio   *Audio
	status  *Status
	ctx     context.Context
}

func NewIntercom(ctx context.Context, a *Audio, s *Status) *Intercom {
	return &Intercom{ctx: ctx, audio: a, status: s}
}
func (i *Intercom) Offer(ctx context.Context, offer webrtc.SessionDescription) (answer webrtc.SessionDescription, id string, err error) {
	if i.audio == nil {
		return answer, "", fmt.Errorf("audio tidak tersedia; periksa status dan restart setelah konfigurasi diperbaiki")
	}
	if offer.Type != webrtc.SDPTypeOffer {
		return answer, "", fmt.Errorf("SDP harus bertipe offer")
	}
	i.mu.Lock()
	if i.current != nil {
		i.mu.Unlock()
		return answer, "", fmt.Errorf("intercom sedang dipakai")
	}
	pc, e := webrtc.NewPeerConnection(webrtc.Configuration{})
	if e != nil {
		i.mu.Unlock()
		return answer, "", e
	}
	random := make([]byte, 16)
	if _, e = rand.Read(random); e != nil {
		i.mu.Unlock()
		pc.Close()
		return answer, "", e
	}
	callCtx, cancel := context.WithCancel(i.ctx)
	c := &call{done: make(chan struct{}), id: hex.EncodeToString(random), pc: pc, route: &audioRoute{mic: make(chan []int16, 5), speaker: make(chan []int16, 10)}, ctx: callCtx, cancel: cancel}
	c.lastPing.Store(time.Now().UnixNano())
	i.current = c
	i.mu.Unlock()
	defer func() {
		if err != nil {
			i.end(c, "setup gagal: "+err.Error())
		}
	}()
	cfg := i.audio.cfg
	encoder, e := gopus.NewEncoder(cfg.SampleRate, cfg.Channels, gopus.Voip)
	if e != nil {
		return answer, "", e
	}
	encoder.SetBitrate(32000)
	track, e := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "piio")
	if e != nil {
		return answer, "", e
	}
	sender, e := pc.AddTrack(track)
	if e != nil {
		return answer, "", e
	}
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, e := sender.Read(buf); e != nil {
				return
			}
		}
	}()
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		i.status.Event("intercom", "peer="+state.String())
		switch state {
		case webrtc.PeerConnectionStateConnected:
			i.mu.Lock()
			if i.current == c && !c.ending {
				i.audio.route.Store(c.route)
				i.status.Set("intercom", "CONNECTED", "panggilan dua arah")
			}
			i.mu.Unlock()
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateDisconnected, webrtc.PeerConnectionStateClosed:
			go i.end(c, state.String())
		}
	})
	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if !strings.EqualFold(remote.Codec().MimeType, webrtc.MimeTypeOpus) {
			i.status.Event("intercom", "codec tidak didukung: "+remote.Codec().MimeType)
			return
		}
		go i.receive(c, remote)
	})
	i.status.Set("intercom", "CONNECTING", "menunggu koneksi laptop")
	if err = pc.SetRemoteDescription(offer); err != nil {
		return answer, "", err
	}
	answer, err = pc.CreateAnswer(nil)
	if err != nil {
		return answer, "", err
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err = pc.SetLocalDescription(answer); err != nil {
		return answer, "", err
	}
	timer := time.NewTimer(12 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return answer, "", ctx.Err()
	case <-callCtx.Done():
		return answer, "", fmt.Errorf("panggilan dibatalkan")
	case <-timer.C:
		return answer, "", fmt.Errorf("ICE gathering timeout")
	case <-gathered:
	}
	if pc.LocalDescription() == nil {
		return answer, "", fmt.Errorf("SDP answer tidak tersedia")
	}
	go i.send(c, encoder, track)
	go i.watch(c)
	return *pc.LocalDescription(), c.id, nil
}
func (i *Intercom) send(c *call, encoder *gopus.Encoder, track *webrtc.TrackLocalStaticSample) {
	cfg := i.audio.cfg
	for {
		select {
		case <-c.ctx.Done():
			return
		case pcm := <-c.route.mic:
			opus, err := encoder.Encode(pcm, cfg.FrameSize, 4000)
			if err == nil {
				err = track.WriteSample(media.Sample{Data: opus, Duration: time.Duration(cfg.FrameSize) * time.Second / time.Duration(cfg.SampleRate)})
			}
			if err != nil {
				i.status.Result("intercom", err, "")
				go i.end(c, "audio send gagal")
				return
			}
		}
	}
}
func (i *Intercom) receive(c *call, track *webrtc.TrackRemote) {
	cfg := i.audio.cfg
	decoder, err := gopus.NewDecoder(cfg.SampleRate, cfg.Channels)
	if err != nil {
		i.status.Result("intercom", err, "")
		return
	}
	// Browser Opus packets need not have the same duration as the PortAudio frame.
	frame := cfg.FrameSize * cfg.Channels
	pending := make([]int16, 0, 5760*cfg.Channels+frame)
	for c.ctx.Err() == nil {
		packet, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		decoded, err := decoder.Decode(packet.Payload, 5760, false)
		if err != nil {
			i.status.Result("intercom", err, "")
			continue
		}
		pending = append(pending, decoded...)
		for len(pending) >= frame {
			pcm := append([]int16(nil), pending[:frame]...)
			pending = pending[frame:]
			select {
			case c.route.speaker <- pcm:
			case <-c.ctx.Done():
				return
			}
		}
	}
}
func (i *Intercom) watch(c *call) {
	started := time.Now()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			i.end(c, "session selesai")
			return
		case <-ticker.C:
			if time.Since(time.Unix(0, c.lastPing.Load())) > 20*time.Second {
				i.end(c, "heartbeat laptop timeout")
				return
			}
			if time.Since(started) > 30*time.Second && c.pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
				i.end(c, "connection timeout")
				return
			}
		}
	}
}
func (i *Intercom) Ping(id string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.current == nil || i.current.id != id {
		return false
	}
	i.current.lastPing.Store(time.Now().UnixNano())
	return true
}
func (i *Intercom) Hangup(id string) bool {
	i.mu.Lock()
	c := i.current
	i.mu.Unlock()
	if c == nil || c.id != id {
		return false
	}
	i.end(c, "hangup laptop")
	return true
}
func (i *Intercom) Close() {
	i.mu.Lock()
	c := i.current
	i.mu.Unlock()
	if c != nil {
		i.end(c, "shutdown")
		<-c.done
	}
}
func (i *Intercom) end(c *call, reason string) {
	i.mu.Lock()
	if i.current != c || c.ending {
		i.mu.Unlock()
		return
	}
	c.ending = true
	// Keep slot reserved until peer is closed; old callbacks cannot route a new call.
	i.audio.route.Store(nil)
	c.cancel()
	i.mu.Unlock()
	_ = c.pc.Close()
	i.mu.Lock()
	if i.current == c {
		i.current = nil
		i.status.Set("intercom", "IDLE", reason+"; audio tetap persisten")
	}
	close(c.done)
	i.mu.Unlock()
}
