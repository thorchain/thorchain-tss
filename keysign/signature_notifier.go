package keysign

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/binance-chain/tss-lib/common"
	tsslibcommon "github.com/binance-chain/tss-lib/common"
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

var signatureNotifierProtocol protocol.ID = "/p2p/signatureNotifier"

type signatureItem struct {
	messageID     string
	peerID        peer.ID
	signatureData []*common.ECSignature
}

// SignatureNotifier is design to notify the
type SignatureNotifier struct {
	logger       zerolog.Logger
	host         host.Host
	notifierLock *sync.Mutex
	notifiers    map[string]*Notifier
	messages     chan *signatureItem
	streamMgr    *p2p.StreamMgr
}

// NewSignatureNotifier create a new instance of SignatureNotifier
func NewSignatureNotifier(host host.Host) *SignatureNotifier {
	s := &SignatureNotifier{
		logger:       log.With().Str("module", "signature_notifier").Logger(),
		host:         host,
		notifierLock: &sync.Mutex{},
		notifiers:    make(map[string]*Notifier),
		messages:     make(chan *signatureItem),
		streamMgr:    p2p.NewStreamMgr(),
	}
	host.SetStreamHandler(signatureNotifierProtocol, s.handleStream)
	return s
}

// HandleStream handle signature notify stream
func (s *SignatureNotifier) handleStream(stream network.Stream) {
	remotePeer := stream.Conn().RemotePeer()
	logger := s.logger.With().Str("remote peer", remotePeer.String()).Logger()
	logger.Debug().Msg("reading signature notifier message")
	payload, err := p2p.ReadStreamWithBuffer(stream)
	if err != nil {
		logger.Err(err).Msgf("fail to read payload from stream")
		s.streamMgr.AddStream("UNKNOWN", stream)
		return
	}
	// we tell the sender we have received the message
	err = p2p.WriteStreamWithBuffer([]byte("done"), stream)
	if err != nil {
		logger.Error().Err(err).Msgf("fail to write the reply to peer: %s", remotePeer)
	}
	var msg messages.KeysignSignature
	if err := proto.Unmarshal(payload, &msg); err != nil {
		logger.Err(err).Msg("fail to unmarshal join party request")
		s.streamMgr.AddStream("UNKNOWN", stream)
		return
	}
	s.streamMgr.AddStream(msg.ID, stream)
	var signatures []*common.ECSignature
	if len(msg.Signatures) > 0 && msg.KeysignStatus == messages.KeysignSignature_Success {
		for _, el := range msg.Signatures {
			var signature common.ECSignature
			if err := proto.Unmarshal(el, &signature); err != nil {
				logger.Error().Err(err).Msg("fail to unmarshal signature data")
				return
			}
			signatures = append(signatures, &signature)
		}
	}
	s.notifierLock.Lock()
	defer s.notifierLock.Unlock()
	n, ok := s.notifiers[msg.ID]
	if !ok {
		logger.Debug().Msgf("notifier for message id(%s) not exist", msg.ID)
		return
	}
	finished, err := n.ProcessSignature(signatures)
	if err != nil {
		logger.Error().Err(err).Msg("fail to verify local signature data")
		return
	}
	if finished {
		delete(s.notifiers, msg.ID)
	}
}

func (s *SignatureNotifier) sendOneMsgToPeer(m *signatureItem) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()
	stream, err := s.host.NewStream(ctx, m.peerID, signatureNotifierProtocol)
	if err != nil {
		return fmt.Errorf("fail to create stream to peer(%s):%w", m.peerID, err)
	}
	s.logger.Debug().Msgf("open stream to (%s) successfully", m.peerID)
	defer func() {
		s.streamMgr.AddStream(m.messageID, stream)
	}()
	ks := &messages.KeysignSignature{
		ID:            m.messageID,
		KeysignStatus: messages.KeysignSignature_Failed,
	}

	if m.signatureData != nil {
		var signatures [][]byte
		for _, el := range m.signatureData {
			buf, err := proto.Marshal(el)
			if err != nil {
				return fmt.Errorf("fail to marshal signature data to bytes:%w", err)
			}
			signatures = append(signatures, buf)
		}
		ks.Signatures = signatures
		ks.KeysignStatus = messages.KeysignSignature_Success
	}
	ksBuf, err := proto.Marshal(ks)
	if err != nil {
		return fmt.Errorf("fail to marshal Keysign Signature to bytes:%w", err)
	}
	err = p2p.WriteStreamWithBuffer(ksBuf, stream)
	if err != nil {
		return fmt.Errorf("fail to write message to stream:%w", err)
	}
	// we wait for 1 second to allow the receive notify us
	if err := stream.SetReadDeadline(time.Now().Add(time.Second * 1)); nil != err {
		return err
	}
	ret := make([]byte, 8)
	_, err = stream.Read(ret)
	return err
}

// BroadcastSignature sending the keysign signature to all other peers
func (s *SignatureNotifier) BroadcastSignature(messageID string, sig []*tsslibcommon.ECSignature, peers []peer.ID) error {
	return s.broadcastCommon(messageID, sig, peers)
}

func (s *SignatureNotifier) broadcastCommon(messageID string, sig []*tsslibcommon.ECSignature, peers []peer.ID) error {
	wg := sync.WaitGroup{}
	for _, p := range peers {
		if p == s.host.ID() {
			// don't send the signature to itself
			continue
		}
		signature := &signatureItem{
			messageID:     messageID,
			peerID:        p,
			signatureData: sig,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.sendOneMsgToPeer(signature)
			if err != nil {
				s.logger.Error().Err(err).Msgf("fail to send signature to peer %s", signature.peerID.String())
			}
		}()
	}
	wg.Wait()
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
func (s *SignatureNotifier) WaitForSignature(messageID string, message [][]byte, poolPubKey string, timeout time.Duration, sigChan chan string) ([]*common.ECSignature, error) {
	n, err := NewNotifier(messageID, message, poolPubKey)
	if err != nil {
		return nil, fmt.Errorf("fail to create notifier")
	}
	s.addToNotifiers(n)
	defer s.removeNotifier(n)

	select {
	case d := <-n.GetResponseChannel():
		return d, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout: didn't receive signature after %s", timeout)
	case <-sigChan:
		return nil, p2p.ErrSigGenerated
	}
}

func (s *SignatureNotifier) ReleaseStream(msgID string) {
	s.streamMgr.ReleaseStream(msgID)
}
