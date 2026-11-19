package rtsp

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"
)

const testSDP = "v=0\r\no=- 1 1 IN IP4 0.0.0.0\r\ns=live\r\nm=audio 0 RTP/AVP 97\r\na=control:track2\r\n" +
	"m=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\n" +
	"a=fmtp:96 packetization-mode=1;profile-level-id=640028;sprop-parameter-sets=Z2QAKA==,aO48gA==\r\n" +
	"a=control:track1\r\n"

func TestParseSDP(t *testing.T) {
	control, params, err := parseSDP(testSDP)
	if err != nil || control != "track1" || len(params) != 2 {
		t.Fatalf("%q %v %v", control, params, err)
	}
	sps, _ := base64.StdEncoding.DecodeString("Z2QAKA==")
	if !bytes.Equal(params[0], sps) {
		t.Errorf("sps %x", params[0])
	}
	if _, _, err := parseSDP("v=0\r\nm=audio 0 RTP/AVP 97\r\n"); err == nil {
		t.Error("no video track")
	}
}

// rtp makes an RTP packet with the payload p.
func rtp(marker bool, p ...byte) []byte {
	h := []byte{0x80, 96, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1}
	if marker {
		h[1] |= 0x80
	}
	return append(h, p...)
}

func TestDepacketize(t *testing.T) {
	c := &Conn{params: [][]byte{{0x67, 1}, {0x68, 2}}}
	// STAP-A with two NAL units (SEI, AUD), no marker.
	if au := c.depacketize(rtp(false, 24, 0, 2, 0x06, 9, 0, 1, 0x09)); au != nil {
		t.Fatal("an access unit before the marker")
	}
	// A FU-A IDR slice in three parts, the marker on the last.
	c.depacketize(rtp(false, 0x7c, 0x85, 0xa1))
	c.depacketize(rtp(false, 0x7c, 0x05, 0xa2))
	au := c.depacketize(rtp(true, 0x7c, 0x45, 0xa3))
	want := []byte{0, 0, 0, 1, 0x67, 1, 0, 0, 0, 1, 0x68, 2, // the parameters, once
		0, 0, 0, 1, 0x06, 9, 0, 0, 0, 1, 0x09,
		0, 0, 0, 1, 0x65, 0xa1, 0xa2, 0xa3}
	if !bytes.Equal(au, want) {
		t.Fatalf("au %x\nwant %x", au, want)
	}
	// A single NAL unit: no parameters this time.
	au = c.depacketize(rtp(true, 0x41, 0xb0))
	if !bytes.Equal(au, []byte{0, 0, 0, 1, 0x41, 0xb0}) {
		t.Errorf("single %x", au)
	}
	// A FU-A with no start is dropped.
	c.depacketize(rtp(false, 0x7c, 0x05, 0xa2))
	if au := c.depacketize(rtp(true, 0x41, 0xb1)); !bytes.Equal(au, []byte{0, 0, 0, 1, 0x41, 0xb1}) {
		t.Errorf("lost start %x", au)
	}
}

func TestDigest(t *testing.T) {
	// The example of RFC 2617, with the method and the URI of RTSP.
	c := &Conn{user: "Mufasa", pass: "Circle Of Life"}
	if err := c.setAuth(`Digest realm="testrealm@host.com", nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093"`); err != nil {
		t.Fatal(err)
	}
	got := c.auth("DESCRIBE", "rtsp://h/1")
	ha1 := md5hex("Mufasa:testrealm@host.com:Circle Of Life")
	want := md5hex(ha1 + ":dcd98b7102dd2f0e8b11d0f600bfb0c093:" + md5hex("DESCRIBE:rtsp://h/1"))
	if !bytes.Contains([]byte(got), []byte(`response="`+want+`"`)) || !bytes.Contains([]byte(got), []byte(`uri="rtsp://h/1"`)) {
		t.Errorf("%s", got)
	}
}

// An RTSPS camera with no check of its certificate does not connect.
func TestDialNeedsCheck(t *testing.T) {
	if _, err := Dial(context.Background(), "rtsps://bblp:x@127.0.0.1:1/streaming/live/1", nil); err != ErrNoCheck {
		t.Errorf("%v", err)
	}
}
