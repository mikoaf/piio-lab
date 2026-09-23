//go:build nusapala && linux && cgo

package nusapala

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gordonklaus/portaudio"
)

type audioRoute struct {
	mic     chan []int16
	speaker chan []int16
}
type Audio struct {
	cfg           AudioConfig
	status        *Status
	mic, speaker  *portaudio.Stream
	input, output []int16
	route         atomic.Pointer[audioRoute]
	wg            sync.WaitGroup
	cancel        context.CancelFunc
}

func ListAudio(w io.Writer) error {
	if err := portaudio.Initialize(); err != nil {
		return err
	}
	defer portaudio.Terminate()
	devices, err := portaudio.Devices()
	if err != nil {
		return err
	}
	for i, d := range devices {
		fmt.Fprintf(w, "%d: %s | input=%d output=%d rate=%.0f host=%s\n", i, d.Name, d.MaxInputChannels, d.MaxOutputChannels, d.DefaultSampleRate, d.HostApi.Name)
	}
	return nil
}
func OpenAudio(ctx context.Context, cfg AudioConfig, s *Status) (*Audio, error) {
	if err := portaudio.Initialize(); err != nil {
		return nil, err
	}
	a := &Audio{cfg: cfg, status: s, input: make([]int16, cfg.FrameSize*cfg.Channels), output: make([]int16, cfg.FrameSize*cfg.Channels)}
	var err error
	a.mic, err = openAudioStream(cfg, true, a.input)
	if err != nil {
		portaudio.Terminate()
		return nil, fmt.Errorf("open microphone: %w", err)
	}
	a.speaker, err = openAudioStream(cfg, false, a.output)
	if err != nil {
		a.mic.Close()
		portaudio.Terminate()
		return nil, fmt.Errorf("open speaker: %w", err)
	}
	if err = a.mic.Start(); err != nil {
		a.mic.Close()
		a.speaker.Close()
		portaudio.Terminate()
		return nil, err
	}
	if err = a.speaker.Start(); err != nil {
		a.mic.Stop()
		a.mic.Close()
		a.speaker.Close()
		portaudio.Terminate()
		return nil, err
	}
	run, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.wg.Add(2)
	go a.micLoop(run)
	go a.speakerLoop(run)
	s.Set("audio", "READY", fmt.Sprintf("PortAudio persistent %d Hz, %d channels, %d frames", cfg.SampleRate, cfg.Channels, cfg.FrameSize))
	return a, nil
}
func openAudioStream(c AudioConfig, input bool, buf []int16) (*portaudio.Stream, error) {
	index := c.OutputDevice
	in, out := 0, c.Channels
	if input {
		index = c.InputDevice
		in, out = c.Channels, 0
	}
	if index == -1 {
		return portaudio.OpenDefaultStream(in, out, float64(c.SampleRate), c.FrameSize, buf)
	}
	devices, err := portaudio.Devices()
	if err != nil {
		return nil, err
	}
	if index >= len(devices) {
		return nil, fmt.Errorf("device index %d tidak tersedia; gunakan -list-audio", index)
	}
	d := devices[index]
	var p portaudio.StreamParameters
	if input {
		p = portaudio.HighLatencyParameters(d, nil)
		p.Input.Channels = c.Channels
	} else {
		p = portaudio.HighLatencyParameters(nil, d)
		p.Output.Channels = c.Channels
	}
	p.SampleRate = float64(c.SampleRate)
	p.FramesPerBuffer = c.FrameSize
	return portaudio.OpenStream(p, buf)
}
func (a *Audio) micLoop(ctx context.Context) {
	defer a.wg.Done()
	lastError := time.Time{}
	var frames uint64
	for ctx.Err() == nil {
		if err := a.mic.Read(); err != nil {
			if time.Since(lastError) > time.Second {
				a.status.Result("microphone", err, "")
				lastError = time.Now()
			}
			if !pause(ctx, 100*time.Millisecond) {
				return
			}
			continue
		}
		frames++
		if frames == 1 || frames%100 == 0 {
			a.status.Progress("microphone", fmt.Sprintf("capture frames=%d", frames), frames)
		}
		if r := a.route.Load(); r != nil {
			pcm := append([]int16(nil), a.input...)
			applyGain(pcm, a.cfg.InputGain)
			select {
			case r.mic <- pcm:
			default:
			}
		}
	}
}
func (a *Audio) speakerLoop(ctx context.Context) {
	defer a.wg.Done()
	lastError := time.Time{}
	var frames uint64
	for ctx.Err() == nil {
		clear(a.output)
		if r := a.route.Load(); r != nil {
			select {
			case pcm := <-r.speaker:
				copy(a.output, pcm)
			default:
			}
		}
		applyGain(a.output, a.cfg.OutputGain)
		if err := a.speaker.Write(); err != nil {
			if time.Since(lastError) > time.Second {
				a.status.Result("speaker", err, "")
				lastError = time.Now()
			}
			if !pause(ctx, 10*time.Millisecond) {
				return
			}
			continue
		}
		frames++
		if frames == 1 || frames%100 == 0 {
			a.status.Progress("speaker", fmt.Sprintf("playback frames=%d (silence when idle)", frames), frames)
		}
	}
}
func (a *Audio) Close() {
	a.cancel()
	// Abort releases blocking PortAudio I/O before waiting. Close only after readers exit.
	a.mic.Abort()
	a.speaker.Abort()
	a.wg.Wait()
	a.mic.Close()
	a.speaker.Close()
	portaudio.Terminate()
}
func applyGain(pcm []int16, gain float32) {
	for i, v := range pcm {
		f := float32(v) * gain
		if f > 32767 {
			f = 32767
		}
		if f < -32768 {
			f = -32768
		}
		pcm[i] = int16(f)
	}
}
func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
