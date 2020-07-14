package p2p

import (
	"bufio"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"gitlab.com/thorchain/tss/go-tss/messages"
)

var errJoinPartyTimeout = errors.New("fail to join party, timeout")

type PartyCoordinator struct {
	logger             zerolog.Logger
	host               host.Host
	stopChan           chan struct{}
	timeout            time.Duration
	peersGroup         map[string]*PeerStatus
	joinPartyGroupLock *sync.Mutex
}

// NewPartyCoordinator create a new instance of PartyCoordinator
func NewPartyCoordinator(host host.Host, timeout time.Duration) *PartyCoordinator {
	// if no timeout is given, default to 10 seconds
	if timeout.Nanoseconds() == 0 {
		timeout = 10 * time.Second
	}
	pc := &PartyCoordinator{
		logger:             log.With().Str("module", "party_coordinator").Logger(),
		host:               host,
		stopChan:           make(chan struct{}),
		timeout:            timeout,
		peersGroup:         make(map[string]*PeerStatus),
		joinPartyGroupLock: &sync.Mutex{},
	}
	host.SetStreamHandler(JoinPartyProtocol, pc.HandleStream)
	return pc
}

// Stop the PartyCoordinator rune
func (pc *PartyCoordinator) Stop() {
	defer pc.logger.Info().Msg("stop party coordinator")
	pc.host.RemoveStreamHandler(JoinPartyProtocol)
	close(pc.stopChan)
}

// HandleStream handle party coordinate stream
func (pc *PartyCoordinator) HandleStream(stream network.Stream) {
	go func() {
		defer func() {
			if err := stream.Reset(); err != nil {
				pc.logger.Err(err).Msg("fail to close the stream")
			}
		}()
		remotePeer := stream.Conn().RemotePeer()
		logger := pc.logger.With().Str("remote peer", remotePeer.String()).Logger()
		logger.Debug().Msg("reading from join party request")
		streamReader := bufio.NewReader(stream)
		for {
			payload, err := ReadStreamWithBuffer(streamReader)
			if err != nil {
				fmt.Printf("%v", err.Error())
				logger.Err(err).Msgf("fail to read payload from stream")
				return
			}
			var msg messages.JoinPartyRequest
			if err := proto.Unmarshal(payload, &msg); err != nil {
				logger.Err(err).Msg("fail to unmarshal join party request")
				return
			}
			pc.joinPartyGroupLock.Lock()
			peerGroup, ok := pc.peersGroup[msg.ID]
			pc.joinPartyGroupLock.Unlock()
			if !ok {
				pc.logger.Info().Msg("this party is not ready")
				// return
				continue
			}
			newFound, err := peerGroup.updatePeer(remotePeer)
			if err != nil {
				pc.logger.Error().Err(err).Msg("receive msg from unknown peer")
				continue
				// return
			}
			if newFound {
				peerGroup.newFound <- true
			}
		}
	}()
}

func (pc *PartyCoordinator) removePeerGroup(messageID string) {
	pc.joinPartyGroupLock.Lock()
	defer pc.joinPartyGroupLock.Unlock()
	delete(pc.peersGroup, messageID)
}

func (pc *PartyCoordinator) createJoinPartyGroups(messageID string, peers []string) (*PeerStatus, error) {
	pIDs, err := pc.getPeerIDs(peers)
	if err != nil {
		pc.logger.Error().Err(err).Msg("fail to parse peer id")
		return nil, err
	}
	pc.joinPartyGroupLock.Lock()
	defer pc.joinPartyGroupLock.Unlock()
	peerStatus := NewPeerStatus(pIDs, pc.host.ID())
	pc.peersGroup[messageID] = peerStatus
	return peerStatus, nil
}

func (pc *PartyCoordinator) getPeerIDs(ids []string) ([]peer.ID, error) {
	result := make([]peer.ID, len(ids))
	for i, item := range ids {
		pid, err := peer.Decode(item)
		if err != nil {
			return nil, fmt.Errorf("fail to decode peer id(%s):%w", item, err)
		}
		result[i] = pid
	}
	return result, nil
}

func (pc *PartyCoordinator) sendRequestToAll(msg *messages.JoinPartyRequest, peers []peer.ID, peerStreams *sync.Map) error {
	var wg sync.WaitGroup
	var sendError error

	for _, el := range peers {
		if el == pc.host.ID() {
			continue
		}
		pStream, ok := peerStreams.Load(el)
		if !ok {
			pc.logger.Error().Msgf("%s cannot find the stream to the peer %s", pc.host.ID(), el.String())
			continue
		}

		wg.Add(1)
		go func(peer peer.ID, s network.Stream) {
			defer wg.Done()
			if s == nil {
				pc.logger.Error().Msgf("the stream to %s is nil", peer.String())
				sendError = errors.New("nil destination stream")
				return
			}
			if err := pc.sendRequestToPeer(msg, pc.host.ID(), s); err != nil {
				pc.logger.Error().Err(err).Msgf("error in send the join party request to peer %s", peer.String())
				sendError = err
				return
			}
		}(el, pStream.(network.Stream))
	}
	wg.Wait()
	return sendError
}

func (pc *PartyCoordinator) sendRequestToPeer(msg *messages.JoinPartyRequest, localPeer peer.ID, pStream network.Stream) error {
	msgBuf, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("fail to marshal msg to bytes: %w", err)
	}
	if ApplyDeadline {
		if err := pStream.SetWriteDeadline(time.Now().Add(TimeoutWritePayload)); nil != err {
			if errReset := pStream.Reset(); errReset != nil {
				return errReset
			}
			return err
		}
	}
	streamWrite := bufio.NewWriter(pStream)
	err = WriteStreamWithBuffer(msgBuf, streamWrite)
	if err != nil {
		if errReset := pStream.Reset(); errReset != nil {
			return errReset
		}
		return fmt.Errorf("fail to write message to stream:%w", err)
	}

	return nil
}

// JoinPartyWithRetry this method provide the functionality to join party with retry and back off
func (pc *PartyCoordinator) JoinPartyWithRetry(msg *messages.JoinPartyRequest, peers []string, peerStreams *sync.Map) ([]peer.ID, error) {
	peerGroup, err := pc.createJoinPartyGroups(msg.ID, peers)
	if err != nil {
		pc.logger.Error().Err(err).Msg("fail to create the join party group")
		return nil, err
	}
	defer pc.removePeerGroup(msg.ID)
	_, offline := peerGroup.getPeersStatus()
	var wg sync.WaitGroup
	done := make(chan struct{})
	defer wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				if err := pc.sendRequestToAll(msg, offline, peerStreams); err != nil {
					pc.logger.Error().Err(err).Msg("fail to send join party to some peers")
				}
			}
			time.Sleep(time.Second)
		}
	}()
	// this is the total time TSS will wait for the party to form
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-peerGroup.newFound:
				pc.logger.Info().Msgf("%s we have found the new peer", pc.host.ID())
				if peerGroup.getCoordinationStatus() {
					close(done)
					return
				}
			case <-time.After(pc.timeout):
				// timeout
				close(done)
				return
			}
		}
	}()
	wg.Wait()
	onlinePeers, _ := peerGroup.getPeersStatus()

	if err := pc.sendRequestToAll(msg, onlinePeers, peerStreams); err != nil {
		pc.logger.Error().Err(err).Msg("fail to send join party to some peers")
	}

	// we always set ourselves as online
	onlinePeers = append(onlinePeers, pc.host.ID())
	if len(onlinePeers) == len(peers) {
		return onlinePeers, nil
	}
	return onlinePeers, errJoinPartyTimeout
}
