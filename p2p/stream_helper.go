package p2p

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/rs/zerolog"
)

const (
	LengthHeader        = 4 // LengthHeader represent how many bytes we used as header
	TimeoutReadPayload  = time.Second * 40
	TimeoutWritePayload = time.Second * 40
	MaxPayload          = 2 * 1024 * 1024 // 512kb

)

// applyDeadline will be true , and only disable it when we are doing test
// the reason being the p2p network , mocknet, mock stream doesn't support SetReadDeadline ,SetWriteDeadline feature
var ApplyDeadline = true

func ReadStreamNoBuffer(stream network.Stream) ([]byte, error) {
	buf := make([]byte, MaxPayload)
	n, err := stream.Read(buf)
	if err != nil {
		return nil, err
	}
	if n == MaxPayload {
		return nil, errors.New("data too large")
	}
	// fmt.Printf("-we get -->%v", buf[:n])
	//buf2, err := base64.StdEncoding.DecodeString(string(buf[:n]))
	//if err != nil {
	//	return nil, err
	//}
	fmt.Printf("we receive %d\n", n-1)
	return buf[:n-1], nil
}

func WriteStreamNoBuffer(stream network.Stream, buf []byte) error {
	dat2 := append(buf, 04)
	n, err := stream.Write(dat2)
	if err != nil {
		return err
	}
	if n != len(dat2) {
		return fmt.Errorf("we want to write %d and we have %d data unwrite", len(dat2), len(dat2)-1-n)
	}
	return nil
}

// ReadStreamWithBuffer read data from the given stream
func ReadStreamWithBuffer(streamReader *bufio.Reader) ([]byte, error) {
	lengthBytes := make([]byte, LengthHeader)
	n, err := io.ReadFull(streamReader, lengthBytes)
	if n != LengthHeader || err != nil {
		return nil, fmt.Errorf("error in read the message head %w", err)
	}
	length := binary.LittleEndian.Uint32(lengthBytes)
	if length > MaxPayload {
		return nil, fmt.Errorf("payload length:%d exceed max payload length:%d", length, MaxPayload)
	}

	fmt.Printf(">>>>we read>>>>>>%d\n", length)
	dataBuf := make([]byte, length)
	n, err = io.ReadFull(streamReader, dataBuf)
	if uint32(n) != length || err != nil {
		return nil, fmt.Errorf("short read err(%w), we would like to read: %d, however we only read: %d", err, length, n)
	}
	return dataBuf, nil
}

// WriteStreamWithBuffer write the message to stream
func WriteStreamWithBuffer(msg []byte, streamWrite *bufio.Writer) error {
	length := uint32(len(msg))
	lengthBytes := make([]byte, LengthHeader)
	binary.LittleEndian.PutUint32(lengthBytes, length)
	fmt.Printf("#####we write ####%d\n", length)
	n, err := streamWrite.Write(lengthBytes)
	if n != LengthHeader || err != nil {
		return fmt.Errorf("fail to write head: %w", err)
	}
	n, err = streamWrite.Write(msg)
	if err != nil {
		return err
	}
	err = streamWrite.Flush()
	if uint32(n) != length || err != nil {
		if err.Error() == "stream reset" {
			return err
		}
		return fmt.Errorf("short write, we would like to write: %d, however we only write: %d", length, n)
	}

	return nil
}

func ReleaseStream(l *zerolog.Logger, s *sync.Map) {
	s.Range(func(k, v interface{}) bool {
		el := v.(network.Stream)
		pid := k.(peer.ID)
		if err := el.Reset(); err != nil {
			l.Error().Err(err).Msgf("fail to release the stream of peer %s", pid.String())
		}
		return true
	})
}

//func SetStreamsProtocol(s *sync.Map, proto protocol.ID) {
//	s.Range(func(_, value interface{}) bool {
//		value.(network.Stream).SetProtocol(proto)
//		return true
//	})
//}

func GetStream(l *zerolog.Logger, h host.Host, remotePeer peer.ID, p protocol.ID) (network.Stream, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*4)
	defer cancel()
	var stream network.Stream
	var streamError error
	var err error
	streamGetChan := make(chan struct{})
	go func() {
		defer close(streamGetChan)
		for i := 0; i < 4; i++ {
			stream, err = h.NewStream(ctx, remotePeer, p)
			if err != nil {
				streamError = fmt.Errorf("fail to create stream to peer(%s):%w", remotePeer, err)
				if l != nil {
					l.Error().Err(err).Msgf("fail to create stream with retry %d", i)
					time.Sleep(time.Second)
					continue
				}
			}
			break
		}
	}()

	select {
	case <-streamGetChan:
		if streamError != nil {
			l.Error().Err(streamError).Msg("fail to open stream")
			return nil, streamError
		}
	case <-ctx.Done():
		l.Error().Err(ctx.Err()).Msg("fail to open stream with context timeout")
		// we reset the whole connection of this peer
		err := h.Network().ClosePeer(remotePeer)
		l.Error().Err(err).Msgf("fail to clolse the connection to peer %s", remotePeer.String())
		return nil, ctx.Err()
	}

	return stream, nil
}
