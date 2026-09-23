//go:build nusapala && linux && cgo

package nusapala

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"layeh.com/gopus"
)

// Exercises real ICE, DTLS, RTP and Opus with an in-memory audio route. No USB
// device or microphone is opened by this test.
func TestWebRTCTwoWayAndHangupKeepsEngine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s := NewStatus(log.New(io.Discard, "", 0))
	a := &Audio{cfg: DefaultConfig().Audio, status: s}
	intercom := NewIntercom(ctx, a, s)
	defer intercom.Close()
	browser, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	remoteAudio, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "mic", "laptop")
	if err != nil {
		t.Fatal(err)
	}
	sender, err := browser.AddTrack(remoteAudio)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		b := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(b); err != nil {
				return
			}
		}
	}()
	received := make(chan struct{}, 1)
	browser.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if _, _, err := track.ReadRTP(); err == nil {
			received <- struct{}{}
		}
	})
	offer, err := browser.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(browser)
	if err = browser.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		t.Fatal("browser ICE timeout")
	}
	answer, id, err := intercom.Offer(ctx, *browser.LocalDescription())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = intercom.Offer(ctx, *browser.LocalDescription()); err == nil {
		t.Fatal("second call accepted")
	}
	if err = browser.SetRemoteDescription(answer); err != nil {
		t.Fatal(err)
	}
	var route *audioRoute
	for route == nil {
		route = a.route.Load()
		if !pause(ctx, 10*time.Millisecond) {
			t.Fatal("connection timeout")
		}
	}
	route.mic <- make([]int16, 480)
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("Pi audio not received by laptop")
	}
	encoder, err := gopus.NewEncoder(48000, 1, gopus.Voip)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := encoder.Encode(make([]int16, 960), 960, 4000)
	if err != nil {
		t.Fatal(err)
	}
	// A browser sends 20 ms while the Pi uses 10 ms frames.
	if err = remoteAudio.WriteSample(media.Sample{Data: packet, Duration: 20 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		select {
		case pcm := <-route.speaker:
			if len(pcm) != 480 {
				t.Fatalf("frame len=%d", len(pcm))
			}
		case <-ctx.Done():
			t.Fatal("laptop audio not routed to Pi speaker")
		}
	}
	if intercom.Hangup("wrong-id") {
		t.Fatal("foreign session hangup accepted")
	}
	if !intercom.Ping(id) {
		t.Fatal("heartbeat rejected")
	}
	if !intercom.Hangup(id) {
		t.Fatal("hangup rejected")
	}
	if a.route.Load() != nil {
		t.Fatal("route retained after hangup")
	}
	if intercom.audio != a {
		t.Fatal("persistent engine replaced")
	}
	// A failed SDP offer must release the single-call slot.
	if _, _, err = intercom.Offer(ctx, webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "bad"}); err == nil {
		t.Fatal("bad SDP accepted")
	}
	intercom.mu.Lock()
	current := intercom.current
	intercom.mu.Unlock()
	if current != nil {
		t.Fatal("failed setup leaked session")
	}
}
