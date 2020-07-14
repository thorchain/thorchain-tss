package p2p

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/protocol"
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
			streamReader := bufio.NewReader(stream)
			l, err := ReadStreamWithBuffer(streamReader)
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

				if ApplyDeadline {
					if err := stream.SetWriteDeadline(time.Now().Add(TimeoutWritePayload)); nil != err {
						if errReset := stream.Reset(); errReset != nil {
							return nil
						}
						return nil
					}
				}
				streamWrite := bufio.NewWriter(stream)

				// streamWrite := bufio.NewWriter(stream)
				err := WriteStreamWithBuffer(input, streamWrite)
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
			streamReader := bufio.NewReader(stream)
			l, err := ReadStreamWithBuffer(streamReader)
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
