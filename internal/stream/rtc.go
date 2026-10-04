package stream

import (
	"fmt"
	"net"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// peer is the WebRTC connection to the browser: a video and a sound track
// out, the "input" and "control" data channels in (the browser opens
// them). parlor-stream is ICE-lite on one UDP port: the browser always
// reaches the server, never the other way round, so there's nothing to
// discover and no STUN or TURN.
type peer struct {
	pc    *webrtc.PeerConnection
	video *webrtc.TrackLocalStaticSample
	audio *webrtc.TrackLocalStaticSample
	// keyframe is called when the browser lost the picture and needs a
	// new keyframe.
	keyframe func()
}

func newAPI(c Config) (*webrtc.API, error) {
	m := &webrtc.MediaEngine{}
	feedback := []webrtc.RTCPFeedback{{Type: "nack"}, {Type: "nack", Parameter: "pli"}, {Type: "ccm", Parameter: "fir"}}
	err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
			SDPFmtpLine:  "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
			RTCPFeedback: feedback,
		},
		PayloadType: 102,
	}, webrtc.RTPCodecTypeVideo)
	if err != nil {
		return nil, err
	}
	err = m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
			SDPFmtpLine: "minptime=10;useinbandfec=1;stereo=1",
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio)
	if err != nil {
		return nil, err
	}
	ir := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, ir); err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: c.UDPPort})
	if err != nil {
		return nil, fmt.Errorf("the stream's UDP port %d: %w", c.UDPPort, err)
	}
	s := webrtc.SettingEngine{}
	s.SetLite(true)
	s.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	s.SetICEUDPMux(webrtc.NewICEUDPMux(nil, conn))
	s.SetICETimeouts(5*time.Second, 15*time.Second, 2*time.Second)
	if len(c.Hosts) > 0 {
		err := s.SetICEAddressRewriteRules(webrtc.ICEAddressRewriteRule{
			External:        c.Hosts,
			AsCandidateType: webrtc.ICECandidateTypeHost,
			Mode:            webrtc.ICEAddressRewriteReplace,
		})
		if err != nil {
			return nil, err
		}
	}
	return webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(ir), webrtc.WithSettingEngine(s)), nil
}

// answer sets up the connection for the browser's offer and returns the
// answer, with every candidate in it.
func newPeer(api *webrtc.API, offer string, keyframe func()) (*peer, string, error) {
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, "", err
	}
	p := &peer{pc: pc, keyframe: keyframe}
	fail := func(err error) (*peer, string, error) {
		pc.Close()
		return nil, "", err
	}
	p.video, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "parlor")
	if err != nil {
		return fail(err)
	}
	p.audio, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "parlor")
	if err != nil {
		return fail(err)
	}
	if err = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		return fail(fmt.Errorf("the browser's offer: %w", err))
	}
	sender, err := pc.AddTrack(p.video)
	if err != nil {
		return fail(err)
	}
	if _, err = pc.AddTrack(p.audio); err != nil {
		return fail(err)
	}
	go p.readRTCP(sender)
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return fail(err)
	}
	done := webrtc.GatheringCompletePromise(pc)
	if err = pc.SetLocalDescription(answer); err != nil {
		return fail(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	return p, pc.LocalDescription().SDP, nil
}

func (p *peer) readRTCP(sender *webrtc.RTPSender) {
	for {
		packets, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, pkt := range packets {
			switch pkt.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				p.keyframe()
			}
		}
	}
}
