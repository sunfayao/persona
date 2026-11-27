package screenlink

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"time"
)

// udpLimits are testLimits with room for the video tests' frames and no silence rule.
func udpLimits() Limits {
	l := testLimits
	l.WindowVideo = 256 << 10
	l.StallAfter = 2 * time.Second
	l.VideoBacklog = time.Hour
	return l
}

// The video channel rides the flow: every message arrives whole and in order, nothing goes over the relays, RFB stays
// where it was, and the pong counts what was sent.
func TestTheVideoRidesAUDPFlow(t *testing.T) {
	l := openUDPLink(t, udpLimits())
	l.up.prove()
	l.up.use(l.id, 0)
	msgs := videoStream(60)
	writeAll(l.helper, msgs)
	want := join(msgs)
	waitFor(t, "the video over udp", func() bool { return len(l.up.delivered()) >= len(want) })
	if !bytes.Equal(l.up.delivered(), want) {
		t.Fatal("the video over udp differs from what the helper sent")
	}
	if n := len(l.p.received(l.id)); n != 0 {
		t.Fatalf("%d video bytes went over the relays too", n)
	}
	if n := l.p.rawTCPData(l.id); n != 0 {
		t.Fatalf("%d raw video bytes went over TCP while UDP carried the channel", n)
	}
	go l.ss.Write(pattern(5000))
	waitFor(t, "rfb on its relay", func() bool { return len(l.p.received(RFB)) == 5000 })
	data, parity, resent := l.up.ping(0)
	if data < uint64(len(want)) || parity != 0 {
		t.Fatalf("pong counters data %d parity %d (sent %d)", data, parity, len(want))
	}
	t.Logf("%d bytes resent on loopback", resent)
}

// A message lost whole at the end of a burst, with nothing after it to show it was sent, is found by the node's tail
// probe: its last shard comes again, and the phone asks for the rest.
func TestATailLostWholeIsProbed(t *testing.T) {
	l := openUDPLink(t, udpLimits())
	l.up.prove()
	l.up.use(l.id, 0)
	first := videoStream(6)
	last := videoFrameMsg(50, false, 3000) // three shards, all lost the first time
	lastAt := uint64(len(join(first)))
	lost := 0
	l.up.mu.Lock()
	l.up.drop = func(h udpFragmentHead) bool {
		if h.Offset == lastAt && lost < 3 {
			lost++
			return true
		}
		return false
	}
	l.up.mu.Unlock()
	writeAll(l.helper, append(first, last))
	want := append(join(first), last...)
	waitFor(t, "the tail", func() bool { return len(l.up.delivered()) == len(want) })
	if !bytes.Equal(l.up.delivered(), want) {
		t.Fatal("the tail differs")
	}
	// Acknowledged: the probes stop.
	waitFor(t, "everything acknowledged", func() bool {
		l.s.mu.Lock()
		defer l.s.mu.Unlock()
		ch := l.s.chans[l.id]
		return ch.base == ch.sent
	})
}

// A frame the phone gives up is not sent again: the node drops the backlog's frames, says where the stream goes on,
// asks the helper for a keyframe and drops frames until it comes. The backlog's other messages still arrive, after the
// reset and before the keyframe, so it decodes.
func TestAGivenUpFrameGoesOnAtAKeyframe(t *testing.T) {
	l := openUDPLink(t, udpLimits())
	l.up.prove()
	l.up.use(l.id, 0)
	before := [][]byte{videoFrameMsg(1, true, 2000), videoFrameMsg(2, false, 2000)}
	lost := videoFrameMsg(3, false, 5000)
	format := videoMessage(videoFormat, pattern(30))
	after := [][]byte{videoFrameMsg(4, false, 2000), format, videoFrameMsg(5, false, 2000)}
	lostAt := uint64(len(join(before)))
	l.up.mu.Lock()
	l.up.autoAsk = false
	l.up.drop = func(h udpFragmentHead) bool { return h.Offset == lostAt }
	l.up.mu.Unlock()

	writeAll(l.helper, append(append(append([][]byte{}, before...), lost), after...))
	waitFor(t, "the frames before the lost one", func() bool { return len(l.up.delivered()) == int(lostAt) })
	// Everything is read from the helper (the node's buffer holds the backlog) before the phone gives up.
	waitFor(t, "the backlog read", func() bool {
		l.s.mu.Lock()
		defer l.s.mu.Unlock()
		ch := l.s.chans[l.id]
		return ch.base+uint64(len(ch.buf)) == uint64(len(join(before))+len(lost)+len(join(after)))
	})
	keyReq := make(chan []byte, 1)
	go func() { b := make([]byte, 5); io.ReadFull(l.helper, b); keyReq <- b }()
	if off := l.up.skip(); off != lostAt {
		t.Fatalf("gave up %d, want %d", off, lostAt)
	}
	reset := l.up.next(udpReset)
	at := binary.BigEndian.Uint64(reset[2:])
	if reset[1] != l.id || at <= lostAt {
		t.Fatalf("reset of %d at %d", reset[1], at)
	}
	select {
	case b := <-keyReq:
		if !bytes.Equal(b, keyframeRequest) {
			t.Fatalf("the helper got %v", b)
		}
	case <-time.After(time.Second):
		t.Fatal("the helper was not asked for a keyframe")
	}
	l.up.mu.Lock()
	l.up.drop = nil
	l.up.mu.Unlock()
	writeAll(l.helper, [][]byte{videoFrameMsg(6, false, 1000), videoFrameMsg(7, true, 3000)})
	waitFor(t, "the format and the keyframe", func() bool { return len(l.up.delivered()) > int(lostAt) })
	waitFor(t, "the keyframe", func() bool {
		got := frames(t, l.up.delivered()[lostAt:])
		return len(got) == 2
	})
	got := frames(t, l.up.delivered()[lostAt:])
	if got[0][0] != int(videoFormat) || got[1][1] != 7 || got[1][2] != 1 {
		t.Fatalf("after giving up: %v, want the format message then keyframe 7", got)
	}
}

// RFB never rides a flow: a use for it on the flow is ignored, and paths keep going on the relays and TLS.
func TestRFBNeverRidesAUDPFlow(t *testing.T) {
	l := openUDPLink(t, udpLimits())
	l.up.prove()
	resumed := l.up.use(RFB, 0)
	if len(resumed) != 1 {
		t.Fatalf("resumed RFB on the flow: %v", resumed[1:])
	}
	go l.ss.Write(pattern(3000))
	waitFor(t, "rfb", func() bool { return len(l.p.received(RFB)) == 3000 })
	l.s.mu.Lock()
	onFlow := l.s.chans[RFB].active.flow != nil
	l.s.mu.Unlock()
	if onFlow {
		t.Fatal("RFB moved onto the flow")
	}
	// A transport the node picks by itself (fallbackLocked) is never the flow either.
	l.s.mu.Lock()
	for tr := range l.s.ts {
		if tr.flow == nil {
			continue
		}
		for _, ch := range l.s.chans {
			if alt := l.s.fallbackLocked(ch); alt == tr {
				l.s.mu.Unlock()
				t.Fatal("fallbackLocked picked the flow")
			}
		}
	}
	l.s.mu.Unlock()
}

// directTransport is the session's one direct TLS route.
func directTransport(s *Session) *transport {
	s.mu.Lock()
	defer s.mu.Unlock()
	for t := range s.ts {
		if t.tls != nil {
			return t
		}
	}
	return nil
}

// Only frames that aren't keyframes may be given up; every other message, a keyframe included, must arrive.
func TestOnlyFramesThatArentKeyframesMayBeGivenUp(t *testing.T) {
	for _, c := range []struct {
		name string
		msg  []byte
		want byte
	}{
		{"a frame", videoFrameMsg(7, false, 100), udpDroppable},
		{"a keyframe", videoFrameMsg(8, true, 100), 0},
		{"format", videoMessage(videoFormat, pattern(20)), 0},
		{"a frame too short to say", videoMessage(videoFrame, pattern(5)), 0},
	} {
		if got := udpFlags(c.msg); got != c.want {
			t.Errorf("%s: flags %d, want %d", c.name, got, c.want)
		}
	}
}

// The pacing follows the phone's request down to a slow link's rate: only 1.25 × the video's own rate and 1 Mbit/s
// hold it up, and 150 Mbit/s caps it.
func TestThePacingFollowsThePhone(t *testing.T) {
	for _, c := range []struct{ want, video, got int }{
		{0, 0, udpPaceDefault},
		{3_750, 1_500, 3_750}, // a 3 Mbit/s cellular bottleneck: what the phone asks for
		{1_000, 1_500, 1_875}, // never below 1.25 × the video
		{100, 0, udpPaceMin},
		{900_000, 0, udpPaceMax},
	} {
		if got := udpPaceFloor(c.want, c.video); got != c.got {
			t.Errorf("want %d, video %d: paced at %d, want %d", c.want, c.video, got, c.got)
		}
	}
}
