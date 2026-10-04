package stream

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	enc "github.com/audemed44/parlor/internal/stream/media"
	"github.com/audemed44/parlor/internal/stream/retro"
)

// Job is a game to play, from Parlor.
type Job struct {
	GameID   int64  `json:"game_id"`
	Platform string `json:"platform"`
	// ROM is the ROM's path in the library.
	ROM string `json:"rom"`
	// Offer is the browser's WebRTC offer.
	Offer string `json:"sdp"`
	// CarryOn starts from the state taken when the game was last left.
	CarryOn bool   `json:"carry_on"`
	Device  string `json:"device"`
	// Scale is the internal resolution to start at; 0 for the default.
	Scale int `json:"scale"`
}

// MaxScale is the highest internal resolution offered.
const MaxScale = 4

// MaxSpeed is the fastest fast forward.
const MaxSpeed = 4

// awayTimeout ends a game whose browser went away and didn't come back.
const awayTimeout = 3 * time.Minute

// command is something the browser asked for, carried out between frames
// on the thread running the core.
type command struct {
	Type  string `json:"type"`
	Slot  int    `json:"slot"`
	Value int    `json:"value"`
}

// Session is one game being played.
type Session struct {
	cfg    Config
	job    Job
	sys    System
	parlor *Parlor
	peer   *peer

	cmds     chan command
	input    atomic.Pointer[retro.Input]
	keyframe atomic.Bool

	ctlMu   sync.Mutex
	control *webrtc.DataChannel
	outbox  [][]byte

	// Touched only by the core's thread.
	paused, away bool
	speed        int
	scale        int
	audio        *enc.Audio
	perf         perf

	// The encoder's goroutine: frames in, buffers back.
	frames    chan frame
	free      chan []byte
	encErr    chan error
	encDone   chan struct{}
	video     *enc.Video
	lastVideo time.Time
	started   atomic.Bool

	uploads   chan []byte
	uploaded  chan struct{}
	saveBase  int64
	savePrint string
	saveHash  [32]byte
}

// Start answers the browser's offer; Run then plays the game.
func Start(cfg Config, job Job) (*Session, string, error) {
	sys, ok := Systems[job.Platform]
	if !ok {
		return nil, "", fmt.Errorf("parlor-stream doesn't play %q games", job.Platform)
	}
	s := &Session{
		cfg: cfg, job: job, sys: sys,
		parlor:   &Parlor{URL: cfg.ParlorURL, Token: cfg.Token},
		cmds:     make(chan command, 16),
		speed:    1,
		uploads:  make(chan []byte, 1),
		uploaded: make(chan struct{}),
	}
	s.input.Store(&retro.Input{})
	api, err := newAPI(cfg)
	if err != nil {
		return nil, "", err
	}
	p, answer, err := newPeer(api, job.Offer, func() { s.keyframe.Store(true) })
	if err != nil {
		return nil, "", err
	}
	s.peer = p
	p.pc.OnDataChannel(s.channel)
	p.pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		slog.Info("connection", "state", st.String())
		switch st {
		case webrtc.PeerConnectionStateConnected:
			s.send(command{Type: "back"})
		case webrtc.PeerConnectionStateDisconnected, webrtc.PeerConnectionStateFailed:
			s.send(command{Type: "away"})
		case webrtc.PeerConnectionStateClosed:
			s.send(command{Type: "quit"})
		}
	})
	return s, answer, nil
}

// Stop ends the game as if the player quit: their place and save are kept.
func (s *Session) Stop() { s.send(command{Type: "quit"}) }

func (s *Session) send(c command) {
	select {
	case s.cmds <- c:
	default:
		slog.Warn("dropped a command", "type", c.Type)
	}
}

func (s *Session) channel(dc *webrtc.DataChannel) {
	switch dc.Label() {
	case "input":
		dc.OnMessage(func(m webrtc.DataChannelMessage) {
			if in, err := DecodeInput(m.Data); err == nil {
				if s.sys.DpadToStick {
					in = withStick(in)
				}
				s.input.Store(&in)
			}
		})
	case "control":
		dc.OnOpen(func() {
			s.ctlMu.Lock()
			s.control = dc
			out := s.outbox
			s.outbox = nil
			s.ctlMu.Unlock()
			for _, m := range out {
				_ = dc.SendText(string(m))
			}
		})
		dc.OnMessage(func(m webrtc.DataChannelMessage) {
			var c command
			if json.Unmarshal(m.Data, &c) == nil {
				switch c.Type {
				case "pause", "resume", "speed", "scale", "save_state", "load_state", "reset", "quit":
					s.send(c)
				}
			}
		})
	}
}

// tell sends the browser a message on the control channel.
func (s *Session) tell(v map[string]any) {
	b, _ := json.Marshal(v)
	s.ctlMu.Lock()
	dc := s.control
	if dc == nil {
		s.outbox = append(s.outbox, b)
	}
	s.ctlMu.Unlock()
	if dc != nil {
		_ = dc.SendText(string(b))
	}
}

func (s *Session) saveDir() string { return filepath.Join(s.cfg.Data, "saves", s.sys.SaveDir) }

// Run plays the game until the player quits or goes away for good.
func (s *Session) Run() error {
	runtime.LockOSThread()
	defer s.peer.pc.Close()
	err := s.play()
	if err != nil {
		slog.Error("game failed", "err", err)
		s.tell(map[string]any{"type": "error", "error": err.Error()})
		time.Sleep(500 * time.Millisecond) // let the message out
	}
	return err
}

func (s *Session) play() error {
	ctx := context.Background()
	go s.uploader()
	defer func() {
		close(s.uploads)
		select {
		case <-s.uploaded:
		case <-time.After(30 * time.Second):
			slog.Warn("gave up waiting for the save to upload")
		}
	}()

	// The game's own save files come from Parlor; everything else in the
	// data folder (system files, shader caches) is kept between games.
	sysDir := filepath.Join(s.cfg.Data, "system")
	if err := os.MkdirAll(sysDir, 0o755); err != nil {
		return err
	}
	save, base, err := s.parlor.LatestSave(ctx, s.job.GameID)
	switch {
	case errors.Is(err, ErrNone):
		err = Unpack(s.saveDir(), nil)
	case err == nil:
		s.saveBase = base
		s.saveHash = sha256.Sum256(save)
		err = Unpack(s.saveDir(), save)
	}
	if err != nil {
		return fmt.Errorf("the save: %w", err)
	}
	s.savePrint = Fingerprint(s.saveDir())

	s.scale = s.job.Scale
	if s.scale < 1 || s.scale > MaxScale {
		s.scale = s.cfg.Scale
	}
	opts := map[string]string{}
	for k, v := range s.sys.Options {
		opts[k] = v
	}
	if s.sys.Resolution != "" {
		opts[s.sys.Resolution] = strconv.Itoa(s.scale)
	}
	err = retro.Open(retro.Config{
		Core: filepath.Join(s.cfg.Cores, s.sys.Core), SystemDir: sysDir,
		SaveDir: filepath.Join(s.cfg.Data, "saves"), RenderNode: s.cfg.GPU, Options: opts,
	})
	if err != nil {
		return fmt.Errorf("the core: %w", err)
	}
	retro.SetMaxHeight(s.cfg.MaxHeight)
	rom := filepath.Join(s.cfg.ROMs, filepath.FromSlash(s.job.ROM))
	t := time.Now()
	if err := retro.Load(rom); err != nil {
		return err
	}
	slog.Info("loaded", "game", s.job.GameID, "in", time.Since(t).Round(time.Millisecond))
	defer retro.Close()

	av := retro.Info()
	if av.FPS <= 0 {
		av.FPS = 60
	}
	if s.audio, err = enc.NewAudio(int(av.SampleRate+0.5), 128); err != nil {
		return err
	}
	defer s.audio.Close()
	s.startEncoder()
	defer s.stopEncoder()

	if s.job.CarryOn {
		// A state only loads once the game is running.
		retro.Run()
		if err := s.loadState(ctx, 0); err != nil {
			slog.Warn("couldn't carry on", "err", err)
			s.tell(map[string]any{"type": "carry_on_failed", "error": err.Error()})
		}
	}
	return s.loop(ctx, av.FPS)
}

func (s *Session) loop(ctx context.Context, fps float64) error {
	period := time.Duration(float64(time.Second) / fps)
	next := time.Now()
	frame := 0
	var awaySince time.Time
	for {
		// Carry out what the browser asked for; while paused, wait for it.
		wait := time.Duration(0)
		if s.paused || s.away {
			wait = 250 * time.Millisecond
		}
		for more := true; more; {
			var c command
			if wait > 0 {
				select {
				case c = <-s.cmds:
				case <-time.After(wait):
					more = false
					continue
				}
				wait = 0
			} else {
				select {
				case c = <-s.cmds:
				default:
					more = false
					continue
				}
			}
			if c.Type == "quit" {
				return s.finish(ctx)
			}
			if c.Type == "away" && !s.away {
				awaySince = time.Now()
			}
			s.do(ctx, c)
		}
		if s.away && time.Since(awaySince) > awayTimeout {
			slog.Info("the browser went away")
			return s.finish(ctx)
		}
		if s.paused || s.away {
			next = time.Now()
			continue
		}

		retro.SetInput(*s.input.Load())
		drew := false
		t := time.Now()
		for i := 0; i < s.speed; i++ {
			if retro.Run() {
				drew = true
			}
			s.perf.frames++
			if i < s.speed-1 {
				retro.ClearAudio() // fast forward is silent
			}
		}
		if s.speed == 1 {
			if err := s.sendAudio(retro.Audio()); err != nil {
				return err
			}
		}
		retro.ClearAudio()
		s.perf.emulate += time.Since(t)
		if drew {
			s.queueVideo()
		}
		select {
		case err := <-s.encErr:
			return err
		default:
		}
		s.perf.report(s.speed)

		frame++
		if frame%120 == 0 {
			s.checkSave()
		}

		next = next.Add(period)
		if d := time.Until(next); d > 0 {
			time.Sleep(d)
		} else if d < -100*time.Millisecond {
			next = time.Now() // fell behind: don't race to catch up
		}
	}
}

func (s *Session) do(ctx context.Context, c command) {
	switch c.Type {
	case "pause":
		s.paused = true
	case "resume":
		s.paused = false
		s.keyframe.Store(true)
	case "away":
		s.away = true
	case "back":
		s.away = false
		s.keyframe.Store(true)
	case "speed":
		s.speed = min(max(c.Value, 1), MaxSpeed)
	case "scale":
		if s.sys.Resolution != "" && c.Value >= 1 && c.Value <= MaxScale {
			s.scale = c.Value
			retro.SetOption(s.sys.Resolution, strconv.Itoa(c.Value))
		}
	case "reset":
		retro.Reset()
	case "save_state":
		s.saveState(ctx, c.Slot)
	case "load_state":
		if err := s.loadState(ctx, c.Slot); err != nil {
			s.tell(map[string]any{"type": "state_failed", "slot": c.Slot, "error": err.Error()})
		} else {
			s.tell(map[string]any{"type": "state_loaded", "slot": c.Slot})
		}
	}
}

// frame is a copy of a frame on its way to the encoder, with the
// resolution it was drawn at.
type frame struct {
	retro.Frame
	scale int
}

// startEncoder encodes and sends frames on a goroutine of its own, so the
// core's thread goes on to the next frame meanwhile.
func (s *Session) startEncoder() {
	s.frames = make(chan frame, 1)
	s.free = make(chan []byte, 3)
	for range 3 {
		s.free <- nil
	}
	s.encErr = make(chan error, 1)
	s.encDone = make(chan struct{})
	go s.encoder()
}

func (s *Session) stopEncoder() {
	close(s.frames)
	<-s.encDone
}

// queueVideo hands the last frame to the encoder. If the encoder is still
// busy and a frame is already waiting, that one is dropped for this one.
// Three buffers go round: one being encoded, one waiting, one filling.
func (s *Session) queueVideo() {
	f := retro.LastFrame()
	if f.Pix == nil {
		return
	}
	var buf []byte
	select {
	case old := <-s.frames: // still waiting: replace it
		buf = old.Pix
	default:
		buf = <-s.free
	}
	f.Pix = append(buf[:0], f.Pix...)
	s.frames <- frame{f, s.scale}
}

func (s *Session) encoder() {
	defer close(s.encDone)
	defer func() {
		if s.video != nil {
			s.video.Close()
		}
	}()
	for f := range s.frames {
		t := time.Now()
		err := s.sendVideo(f)
		s.free <- f.Pix
		if err != nil {
			s.encErr <- err
			return
		}
		s.perf.encode.Add(int64(time.Since(t)))
		s.perf.sent.Add(1)
	}
}

func (s *Session) sendVideo(f frame) error {
	if s.video == nil || s.video.Width != f.Width || s.video.Height != f.Height {
		if s.video != nil {
			s.video.Close()
		}
		kbps := s.cfg.Kbps
		if kbps <= 0 {
			kbps = bitrate(f.Width, f.Height)
		}
		v, err := enc.NewVideo(enc.VideoConfig{
			Kind: s.cfg.Encoder, Device: s.cfg.GPU, Width: f.Width, Height: f.Height, FPS: 60, Kbps: kbps,
		})
		if err != nil && s.cfg.Encoder != "x264" {
			slog.Warn("falling back to x264", "encoder", s.cfg.Encoder, "err", err)
			s.cfg.Encoder = "x264"
			v, err = enc.NewVideo(enc.VideoConfig{Kind: "x264", Width: f.Width, Height: f.Height, FPS: 60, Kbps: kbps})
		}
		if err != nil {
			return err
		}
		slog.Info("streaming", "size", fmt.Sprintf("%dx%d", f.Width, f.Height), "encoder", s.cfg.Encoder, "kbps", kbps)
		s.video = v
		s.keyframe.Store(true)
	}
	data, err := s.video.Encode(f.Pix, f.Format, f.Stride, f.Flip, s.keyframe.Swap(false))
	if err != nil || len(data) == 0 {
		return err
	}
	now := time.Now()
	d := time.Second / 60
	if !s.lastVideo.IsZero() {
		d = now.Sub(s.lastVideo)
	}
	s.lastVideo = now
	if err := s.peer.video.WriteSample(media.Sample{Data: data, Duration: d}); err != nil {
		return err
	}
	if !s.started.Swap(true) {
		s.tell(map[string]any{"type": "started", "scale": f.scale})
	}
	return nil
}

func (s *Session) sendAudio(samples []int16) error {
	if err := s.audio.Push(samples); err != nil {
		return err
	}
	for {
		pkt, err := s.audio.Next()
		if err != nil || pkt == nil {
			return err
		}
		if err := s.peer.audio.WriteSample(media.Sample{Data: pkt, Duration: 20 * time.Millisecond}); err != nil {
			return err
		}
	}
}

// checkSave queues the save for upload when its files changed.
func (s *Session) checkSave() {
	if s.sys.SaveDir == "" {
		return
	}
	fp := Fingerprint(s.saveDir())
	if fp == s.savePrint {
		return
	}
	s.savePrint = fp
	data, err := Pack(s.saveDir())
	if err != nil {
		slog.Error("couldn't pack the save", "err", err)
		return
	}
	h := sha256.Sum256(data)
	if data == nil || h == s.saveHash {
		return
	}
	s.saveHash = h
	// Only the newest matters: replace one still waiting.
	select {
	case <-s.uploads:
	default:
	}
	s.uploads <- data
}

// uploader sends saves to Parlor, one at a time. A save another device
// made meanwhile isn't lost: Parlor keeps every version, so this one is
// added on top.
func (s *Session) uploader() {
	defer close(s.uploaded)
	for data := range s.uploads {
		s.tell(map[string]any{"type": "saving"})
		for attempt := 0; ; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			v, err := s.parlor.PutSave(ctx, s.job.GameID, data, s.saveBase, s.job.Device, false)
			var conflict *Conflict
			if errors.As(err, &conflict) {
				slog.Warn("another device saved meanwhile; keeping both", "theirs", conflict.Latest.ID)
				v, err = s.parlor.PutSave(ctx, s.job.GameID, data, s.saveBase, s.job.Device, true)
			}
			cancel()
			if err == nil {
				s.saveBase = v.ID
				s.tell(map[string]any{"type": "saved", "at": v.Created})
				break
			}
			slog.Error("couldn't upload the save", "err", err)
			s.tell(map[string]any{"type": "save_failed", "error": err.Error()})
			if attempt == 5 {
				break
			}
			time.Sleep(5 * time.Second)
		}
	}
}

func (s *Session) saveState(ctx context.Context, slot int) {
	data, err := s.capture()
	if err == nil {
		var v State
		v, err = s.parlor.PutState(ctx, s.job.GameID, slot, data, s.job.Device, time.Now())
		if err == nil {
			s.tell(map[string]any{"type": "state_saved", "slot": slot, "state": v})
			return
		}
	}
	s.tell(map[string]any{"type": "state_failed", "slot": slot, "error": err.Error()})
}

// capture is a save state wrapped in a picture of the screen.
func (s *Session) capture() ([]byte, error) {
	state, err := retro.Serialize()
	if err != nil {
		return nil, err
	}
	f := retro.LastFrame()
	if f.Pix == nil {
		return state, nil
	}
	return Wrap(Picture(f), state)
}

func (s *Session) loadState(ctx context.Context, slot int) error {
	data, err := s.parlor.GetState(ctx, s.job.GameID, slot)
	if err != nil {
		return err
	}
	state, err := Unwrap(data)
	if err != nil {
		return err
	}
	if err := retro.Unserialize(state); err != nil {
		return err
	}
	s.keyframe.Store(true)
	return nil
}

// finish keeps the player's place and their save, and says goodbye.
func (s *Session) finish(ctx context.Context) error {
	s.checkSave()
	if s.started.Load() {
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		data, err := s.capture()
		if err == nil {
			_, err = s.parlor.PutState(ctx, s.job.GameID, 0, data, s.job.Device, time.Now())
		}
		if err != nil {
			slog.Error("couldn't keep the player's place", "err", err)
		}
	}
	s.tell(map[string]any{"type": "ended"})
	time.Sleep(200 * time.Millisecond)
	return nil
}

// perf logs how the game keeps up, every 30 s: frames emulated and sent
// a second, and the time each tick spends emulating and encoding.
type perf struct {
	since   time.Time
	ticks   int
	frames  int
	emulate time.Duration
	// From the encoder's goroutine.
	sent, encode atomic.Int64
}

func (p *perf) report(speed int) {
	p.ticks++
	if p.since.IsZero() {
		p.since = time.Now()
		return
	}
	d := time.Since(p.since)
	if d < 30*time.Second {
		return
	}
	ms := func(t time.Duration, n int) string {
		return strconv.FormatFloat(float64(t.Microseconds())/1000/float64(max(n, 1)), 'f', 1, 64) + "ms"
	}
	sent := int(p.sent.Swap(0))
	encode := time.Duration(p.encode.Swap(0))
	slog.Info("performance", "emulated_fps", int(float64(p.frames)/d.Seconds()), "sent_fps", int(float64(sent)/d.Seconds()),
		"speed", speed, "emulate_per_tick", ms(p.emulate, p.ticks), "readback_per_frame", ms(retro.TakeReadback(), p.frames),
		"encode_per_frame", ms(encode, sent), "busy", strconv.Itoa(int(100*p.emulate/d))+"%")
	p.since, p.ticks, p.frames, p.emulate = time.Now(), 0, 0, 0
}
