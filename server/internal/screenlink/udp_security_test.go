package screenlink

import (
	"bytes"
	"crypto/rand"
	"testing"
	"time"
)

// The video over UDP's threat model (#7873), one test per threat:
//
//   - Replay: a packet captured on the way and sent again is dropped (TestUDPDropsWhatDoesNotOpen, TestTheReplayWindow).
//   - Tampering: a packet changed on the way, header or body, doesn't open (TestUDPDropsWhatDoesNotOpen).
//   - A wrong key: a packet sealed with any key but the flow's own direction's doesn't open, including the node's own
//     packets reflected back and keys exported from another connection (TestUDPKeysAreBoundToTheirConnectionAndDirection).
//   - Unauthenticated packets get no answer at all (TestUDPDropsWhatDoesNotOpen, TestNothingButAHelloBeforeTheProof).
//   - Amplification: nothing is sent to an address the phone hasn't proved, and the one answer before the proof (the
//     hello's challenge) is smaller than the hello (TestUDPNeverAmplifies).
//   - Nonces: each direction has its own key and IV and numbers its packets from 0 without repeating; a flow that ran
//     out of numbers is closed rather than reuse one (TestUDPNoncesNeverRepeat).

// noPong fails if a pong reaches the phone within d.
func (u *udpPhone) noPong(d time.Duration, what string) {
	u.t.Helper()
	deadline := time.After(d)
	for {
		select {
		case b := <-u.ctl:
			if b[0] == udpPong {
				u.t.Fatalf("%s: answered", what)
			}
		case <-deadline:
			return
		}
	}
}

func TestTheReplayWindow(t *testing.T) {
	var w replayWindow
	for _, c := range []struct {
		pn   uint64
		want bool
	}{
		{0, true}, {0, false}, {2, true}, {1, true}, {1, false}, {2, false},
		{5000, true}, {5000 - replaySpan, false}, {5000 - replaySpan + 1, true}, {4999, true}, {4999, false},
		{1 << 40, true}, {5000, false}, {(1 << 40) - 1, true}, {1<<64 - 1, false},
	} {
		if got := w.accept(c.pn); got != c.want {
			t.Fatalf("packet %d: accepted %v, want %v", c.pn, got, c.want)
		}
	}
}

func TestUDPNoncesNeverRepeat(t *testing.T) {
	material := make([]byte, udpKeyMaterial)
	_, _ = rand.Read(material)
	keys, err := newUDPKeys(material)
	if err != nil {
		t.Fatal(err)
	}
	if keys.ivPhone == keys.ivNode || bytes.Equal(material[:32], material[44:76]) {
		t.Fatal("the two directions share a key or an IV")
	}
	var flow [8]byte
	toPhone := &udpSealer{aead: keys.toPhone, iv: keys.ivPhone, flow: flow}
	toNode := &udpSealer{aead: keys.toNode, iv: keys.ivNode, flow: flow}
	seen := map[[13]byte]bool{} // direction, nonce
	for i := 0; i < 5000; i++ {
		for d, s := range []*udpSealer{toPhone, toNode} {
			p, ok := s.seal([]byte{udpPing})
			if !ok {
				t.Fatal("ran out of packet numbers")
			}
			_, pn, _ := udpHeader(p)
			if pn != uint64(i) {
				t.Fatalf("packet %d numbered %d", i, pn)
			}
			var n [13]byte
			n[0] = byte(d)
			copy(n[1:], udpNonce(s.iv, pn))
			if seen[n] {
				t.Fatalf("a nonce repeated at packet %d", i)
			}
			seen[n] = true
		}
	}
	// The same body and number seal differently in the two directions.
	a := sealUDP(keys.toPhone, keys.ivPhone, flow, 7, []byte("same"))
	b := sealUDP(keys.toNode, keys.ivNode, flow, 7, []byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("both directions sealed alike")
	}
	// A sealer that reaches the limit stops instead of wrapping around.
	toPhone.next = udpPacketLimit - 1
	if _, ok := toPhone.seal([]byte{udpPing}); !ok {
		t.Fatal("the last packet number was refused")
	}
	if _, ok := toPhone.seal([]byte{udpPing}); ok {
		t.Fatal("sealed past the limit")
	}
}

// flow is the node's flow for the link's phone.
func (l *udpLink) flow() *udpFlow {
	l.s.direct.mu.Lock()
	defer l.s.direct.mu.Unlock()
	return l.s.direct.flows[l.up.flow]
}
