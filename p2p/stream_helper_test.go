package p2p

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/magiconair/properties/assert"
)

const testProtocolID protocol.ID = "/p2p/test-stream"

type MockNetworkStream struct {
	*bytes.Buffer
	protocol            protocol.ID
	errSetReadDeadLine  bool
	errSetWriteDeadLine bool
	errRead             bool
}

func NewMockNetworkStream() *MockNetworkStream {
	return &MockNetworkStream{
		Buffer:   &bytes.Buffer{},
		protocol: testProtocolID,
	}
}

func (m MockNetworkStream) Read(buf []byte) (int, error) {
	if m.errRead {
		return 0, errors.New("you asked for it")
	}
	return m.Buffer.Read(buf)
}

func (m MockNetworkStream) Close() error {
	return nil
}

func (m MockNetworkStream) Reset() error {
	return nil
}

func (m MockNetworkStream) SetDeadline(time.Time) error {
	return nil
}

func (m MockNetworkStream) SetReadDeadline(time.Time) error {
	if m.errSetReadDeadLine {
		return errors.New("you asked for it")
	}
	return nil
}

func (m MockNetworkStream) SetWriteDeadline(time.Time) error {
	if m.errSetWriteDeadLine {
		return errors.New("you asked for it")
	}
	return nil
}

func (m MockNetworkStream) Protocol() protocol.ID {
	return m.protocol
}

func (m MockNetworkStream) SetProtocol(id protocol.ID) {
	m.protocol = id
}

func (m MockNetworkStream) Stat() network.Stat {
	return network.Stat{
		Direction: 0,
		Extra:     make(map[interface{}]interface{}),
	}
}

func (m MockNetworkStream) Conn() network.Conn {
	return nil
}

func TestReadLength(t *testing.T) {
	testCases := []struct {
		name           string
		streamProvider func() network.Stream
		expectedLength uint32
		expectError    bool
		validator      func(t *testing.T)
	}{
		{
			name:           "happy path",
			expectedLength: 1024,
			expectError:    false,
			streamProvider: func() network.Stream {
				s := NewMockNetworkStream()
				buf := make([]byte, LengthHeader)
				binary.LittleEndian.PutUint32(buf, 1024)
				s.Buffer.Write(buf)
				s.Buffer.Write(bytes.Repeat([]byte("a"), 1024))
				return s
			},
		},
		{
			name:           "fail to set read dead line should return an error",
			expectedLength: 1024,
			expectError:    true,
			streamProvider: func() network.Stream {
				s := NewMockNetworkStream()
				s.errSetReadDeadLine = true
				buf := make([]byte, LengthHeader)
				binary.LittleEndian.PutUint32(buf, 1024)
				s.Buffer.Write(buf)
				s.Buffer.Write(bytes.Repeat([]byte("a"), 1024))
				return s
			},
		},
		{
			name:           "read exactly the given length of data",
			expectedLength: 1024,
			expectError:    false,
			streamProvider: func() network.Stream {
				s := NewMockNetworkStream()
				buf := make([]byte, LengthHeader)
				binary.LittleEndian.PutUint32(buf, 1024)
				s.Buffer.Write(buf)
				s.Buffer.Write(bytes.Repeat([]byte("a"), 1026))
				return s
			},
		},
		{
			name:           "fail to read should return an error",
			expectedLength: 1024,
			expectError:    true,
			streamProvider: func() network.Stream {
				s := NewMockNetworkStream()
				buf := make([]byte, LengthHeader)
				binary.LittleEndian.PutUint32(buf, 1024)
				s.Buffer.Write(buf)
				s.errRead = true
				return s
			},
		},
	}
	for _, tc := range testCases {
		ApplyDeadline = true
		t.Run(tc.name, func(st *testing.T) {
			stream := tc.streamProvider()
			l, err := ReadStreamWithBuffer(stream)
			if tc.expectError && err == nil {
				st.Errorf("expecting error , however got none")
				st.FailNow()
			}
			if !tc.expectError && err != nil {
				st.Error(err)
				st.FailNow()
			}
			if !tc.expectError && tc.expectedLength != uint32(len(l)) {
				st.Errorf("expecting length to be %d, however got :%d", tc.expectedLength, l)
				st.FailNow()
			}
		})
	}
}

func TestReadPayload(t *testing.T) {
	testCases := []struct {
		name           string
		streamProvider func() *MockNetworkStream
		expectedBytes  []byte
		expectError    bool
	}{
		{
			name: "happy path",
			streamProvider: func() *MockNetworkStream {
				stream := NewMockNetworkStream()
				input := []byte("hello world")
				err := WriteStreamWithBuffer(input, stream)
				if err != nil {
					t.Errorf("fail to write the data to stream")
					t.FailNow()
				}
				return stream
			},
			expectedBytes: []byte("hello world"),
			expectError:   false,
		},
	}
	for _, tc := range testCases {
		ApplyDeadline = true
		t.Run(tc.name, func(st *testing.T) {
			stream := tc.streamProvider()
			l, err := ReadStreamWithBuffer(stream)
			if err != nil {
				st.Errorf("fail to read length:%s", err)
				st.FailNow()
			}
			if tc.expectError && err == nil {
				st.Errorf("expecting error , however got none")
				st.FailNow()
			}
			if !tc.expectError && err != nil {
				st.Error(err)
				st.FailNow()
			}

			if !tc.expectError && !bytes.Equal(tc.expectedBytes, l) {
				st.Errorf("expecting %s, however got :%s", string(tc.expectedBytes), string(l))
				st.FailNow()
			}
		})
	}
}

func TestStreamManager(t *testing.T) {
	streamMgr := NewStreamMgr()
	stream := NewMockNetworkStream()

	streamMgr.AddStream("1", nil)
	assert.Equal(t, len(streamMgr.unusedStreams), 0)
	streamMgr.AddStream("1", stream)
	streamMgr.AddStream("2", stream)
	streamMgr.AddStream("3", stream)
	streamMgr.ReleaseStream("1")
	_, ok := streamMgr.unusedStreams["2"]
	assert.Equal(t, ok, true)
	_, ok = streamMgr.unusedStreams["3"]
	assert.Equal(t, ok, true)
	streamMgr.ReleaseStream("2")
	_, ok = streamMgr.unusedStreams["2"]
	assert.Equal(t, ok, false)
	streamMgr.ReleaseStream("3")
	assert.Equal(t, len(streamMgr.unusedStreams), 0)
	streamMgr.ReleaseStream("3")
	assert.Equal(t, len(streamMgr.unusedStreams), 0)
}
