package keysign

import (
	"bufio"
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

var SignatureNotifierProtocol protocol.ID = "/p2p/signatureNotifier"

type signatureItem struct {
	messageID     string
	peerID        peer.ID
	signatureData *bc.SignatureData
	stream        network.Stream
}

// SignatureNotifier is design to notify the
type SignatureNotifier struct {
	logger       zerolog.Logger
	host         host.Host
	notifierLock *sync.Mutex
	notifiers    map[string]*Notifier
	messages     chan *signatureItem
}

// NewSignatureNotifier create a new instance of SignatureNotifier
func NewSignatureNotifier(host host.Host) *SignatureNotifier {
	s := &SignatureNotifier{
		logger:       log.With().Str("module", "signature_notifier").Logger(),
		host:         host,
		notifierLock: &sync.Mutex{},
		notifiers:    make(map[string]*Notifier),
		messages:     make(chan *signatureItem),
	}
	host.SetStreamHandler(SignatureNotifierProtocol, s.handleStream)
	return s
}

func (s *SignatureNotifier) processSignature(stream network.Stream) {
	defer func() {
		err := stream.Reset()
		if err != nil {
			s.logger.Error().Err(err).Msgf("fail to close reset the stream")
		}
	}()
	remotePeer := stream.Conn().RemotePeer()
	logger := s.logger.With().Str("remote peer", remotePeer.String()).Logger()
	streamReader := bufio.NewReader(stream)
	payload, err := p2p.ReadStreamWithBuffer(streamReader)
	if err != nil {
		if err.Error() == "error in read the message head stream reset" {
			logger.Debug().Msgf("we receive the close reset stream")
			return
		}
		logger.Err(err).Msgf("fail to read payload from stream")
		return
	}
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
		return
	}
}

// HandleStream handle signature notify stream
func (s *SignatureNotifier) handleStream(stream network.Stream) {
	// todo we need to recycling the stream and release them
	go s.processSignature(stream)
}

func (s *SignatureNotifier) sendOneMsgToPeer(m *signatureItem) error {
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

	streamWrite := bufio.NewWriter(m.stream)
	err = p2p.WriteStreamWithBuffer(ksBuf, streamWrite)
	if err != nil {
		if err.Error() == "stream reset" {
			return nil
		}
		s.logger.Debug().Msgf("we quit as the receiver quit")
		return fmt.Errorf("fail to write message to stream:%w", err)
	}
	streamRead := bufio.NewReader(m.stream)
	_, err = p2p.ReadStreamWithBuffer(streamRead)
	if err != nil {
		if err.Error() == "error in read the message head stream reset" {
			return nil
		}
		return err
	}
	return nil
}

// BroadcastSignature sending the keysign signature to all other peers
func (s *SignatureNotifier) BroadcastSignature(messageID string, sig *bc.SignatureData, peers []peer.ID, streams *sync.Map) error {
	return s.broadcastCommon(messageID, sig, peers, streams)
}

func (s *SignatureNotifier) broadcastCommon(messageID string, sig *bc.SignatureData, peers []peer.ID, streams *sync.Map) error {
	var wg sync.WaitGroup
	for _, p := range peers {
		if p == s.host.ID() {
			// don't send the signature to itself
			continue
		}
		st, ok := streams.Load(p)
		if !ok {
			s.logger.Warn().Msgf("fail to find stream for the peer %s", p.String())
			continue
		}
		stream := st.(network.Stream)
		sigItem := signatureItem{
			messageID:     messageID,
			peerID:        p,
			signatureData: sig,
			stream:        stream,
		}
		if err := stream.SetWriteDeadline(time.Now().Add(time.Second * 2)); nil != err {
			return err
		}
		if err := stream.SetReadDeadline(time.Now().Add(time.Second * 2)); nil != err {
			return err
		}
		wg.Add(1)
		go func() {
			defer func() {
				err := stream.Reset()
				if err != nil {
					s.logger.Error().Err(err).Msgf("fail close reset")
					return
				}
				wg.Done()
			}()
			err := s.sendOneMsgToPeer(&sigItem)
			if err != nil {
				s.logger.Error().Err(err).Msgf("fail to send signature notification to the peer")
			}
		}()
	}
	wg.Wait()
	return nil
}

// BroadcastFailed will send keysign failed message to the nodes that are not in the keysign party
func (s *SignatureNotifier) BroadcastFailed(messageID string, peers []peer.ID, streams *sync.Map) error {
	return s.broadcastCommon(messageID, nil, peers, streams)
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
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout: didn't receive signature after %s", timeout)
	}
}
