package keysign

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	bc "github.com/binance-chain/tss-lib/common"
	"github.com/golang/protobuf/proto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"gitlab.com/thorchain/tss/go-tss/messages"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

const signatureNotifiers = 10

var signatureNotifierProtocol protocol.ID = "/p2p/signatureNotifier"

type signatureItem struct {
	messageID     string
	peerID        peer.ID
	signatureData *bc.SignatureData
}

// SignatureNotifier is design to notify the
type SignatureNotifier struct {
	logger       zerolog.Logger
	host         host.Host
	stopChan     chan struct{}
	notifierLock *sync.Mutex
	notifiers    map[string]*Notifier
	messages     chan *signatureItem
	wg           *sync.WaitGroup
	streamMgr    *p2p.StreamMgr
}

// NewSignatureNotifier create a new instance of SignatureNotifier
func NewSignatureNotifier(host host.Host) *SignatureNotifier {
	s := &SignatureNotifier{
		logger:       log.With().Str("module", "signature_notifier").Logger(),
		host:         host,
		notifierLock: &sync.Mutex{},
		notifiers:    make(map[string]*Notifier),
		stopChan:     make(chan struct{}),
		messages:     make(chan *signatureItem),
		wg:           &sync.WaitGroup{},
		streamMgr:    p2p.NewStreamMgr(),
	}
	host.SetStreamHandler(signatureNotifierProtocol, s.handleStream)
	return s
}

// HandleStream handle signature notify stream
func (s *SignatureNotifier) handleStream(stream network.Stream) {
	defer func() {
		if err := stream.Reset(); err != nil {
			s.logger.Err(err).Msg("fail to close the stream")
		}
	}()
	remotePeer := stream.Conn().RemotePeer()
	logger := s.logger.With().Str("remote peer", remotePeer.String()).Logger()
	logger.Debug().Msg("reading signature notifier message")
	payload, err := p2p.ReadStreamWithBuffer(stream)
	if err != nil {
		logger.Err(err).Msgf("fail to read payload from stream")
		return
	}
	_ = payload
	var msg messages.KeysignSignature
	if err := proto.Unmarshal(payload, &msg); err != nil {
		logger.Err(err).Msg("fail to unmarshal join party request")
		return
	}
	var signature bc.SignatureData
	if len(msg.Signature) > 0 && msg.KeysignStatus == messages.KeysignSignature_Success {
		if err := proto.Unmarshal(msg.Signature, &signature); err != nil {
			logger.Error().Err(err).Msg("fail to unmarshal signature data")
			return
		}
	}
	s.notifierLock.Lock()
	defer s.notifierLock.Unlock()
	n, ok := s.notifiers[msg.ID]
	if !ok {
		logger.Debug().Msgf("notifier for message id(%s) not exist", msg.ID)
		return
	}
	finished, err := n.ProcessSignature(&signature)
	if err != nil {
		logger.Error().Err(err).Msg("fail to update local signature data")
		return
	}
	if finished {
		delete(s.notifiers, msg.ID)
	}
}

func (s *SignatureNotifier) Start() {
	for i := 0; i < signatureNotifiers; i++ {
		s.wg.Add(1)
		go s.sendMessageToPeer()
	}
}

// Stop the signature notifier
func (s *SignatureNotifier) Stop() {
	close(s.stopChan)
	s.wg.Wait()
}

func (s *SignatureNotifier) sendMessageToPeer() {
	s.logger.Debug().Msg("start to send message to peers")
	defer s.logger.Debug().Msg("stop send message to peers")
	defer s.wg.Done()
	for {
		select {
		case <-s.stopChan:
			return
		case msg := <-s.messages:
			if err := s.sendOneMsgToPeer(msg); err != nil {
				s.logger.Error().Err(err).Msgf("fail to send message(%s) to peer:%s", msg.messageID, msg.peerID)
			}
		}
	}
}

func (s *SignatureNotifier) sendOneMsgToPeer(m *signatureItem) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	stream, err := s.host.NewStream(ctx, m.peerID, signatureNotifierProtocol)
	if err != nil {
		return fmt.Errorf("fail to create stream to peer(%s):%w", m.peerID, err)
	}
	s.logger.Debug().Msgf("open stream to (%s) successfully", m.peerID)
	defer func() {
		s.streamMgr.AddStream(m.messageID, stream)
		if err := stream.Close(); err != nil {
			s.logger.Error().Err(err).Msg("fail to close stream")
		}
	}()
	ks := &messages.KeysignSignature{
		ID:            m.messageID,
		KeysignStatus: messages.KeysignSignature_Failed,
	}

	if m.signatureData != nil {
		buf, err := proto.Marshal(m.signatureData)
		if err != nil {
			return fmt.Errorf("fail to marshal signature data to bytes:%w", err)
		}
		ks.Signature = buf
		ks.KeysignStatus = messages.KeysignSignature_Success
	}
	ksBuf, err := proto.Marshal(ks)
	if err != nil {
		return fmt.Errorf("fail to marshal Keysign Signature to bytes:%w", err)
	}

	err = p2p.WriteStreamWithBuffer(ksBuf, stream)
	if err != nil {
		if errReset := stream.Reset(); errReset != nil {
			return errReset
		}
		return fmt.Errorf("fail to write message to stream:%w", err)
	}
	return nil
}

// BroadcastSignature sending the keysign signature to all other peers
func (s *SignatureNotifier) BroadcastSignature(messageID string, sig *bc.SignatureData, peers []peer.ID) error {
	return s.broadcastCommon(messageID, sig, peers)
}

func (s *SignatureNotifier) broadcastCommon(messageID string, sig *bc.SignatureData, peers []peer.ID) error {
	for _, p := range peers {
		if p == s.host.ID() {
			// don't send the signature to itself
			continue
		}
		select {
		case s.messages <- &signatureItem{
			messageID:     messageID,
			peerID:        p,
			signatureData: sig,
		}:
		case <-s.stopChan:
			return nil
		}
	}
	return nil
}

// BroadcastFailed will send keysign failed message to the nodes that are not in the keysign party
func (s *SignatureNotifier) BroadcastFailed(messageID string, peers []peer.ID) error {
	return s.broadcastCommon(messageID, nil, peers)
}

func (s *SignatureNotifier) addToNotifiers(n *Notifier) {
	s.notifierLock.Lock()
	defer s.notifierLock.Unlock()
	s.notifiers[n.MessageID] = n
}

func (s *SignatureNotifier) removeNotifier(n *Notifier) {
	s.notifierLock.Lock()
	defer s.notifierLock.Unlock()
	delete(s.notifiers, n.MessageID)
}

// WaitForSignature wait until keysign finished and signature is available
func (s *SignatureNotifier) WaitForSignature(messageID string, message []byte, poolPubKey string, timeout time.Duration) (*bc.SignatureData, error) {
	n, err := NewNotifier(messageID, message, poolPubKey)
	if err != nil {
		return nil, fmt.Errorf("fail to create notifier")
	}
	s.addToNotifiers(n)
	defer s.removeNotifier(n)

	select {
	case d := <-n.GetResponseChannel():
		return d, nil
	case <-s.stopChan:
		return nil, errors.New("request to exit")
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout: didn't receive signature after %s", timeout)
	}
}

func (s *SignatureNotifier) ReleaseStream(msgID string) {
	s.streamMgr.ReleaseStream(msgID)
}
