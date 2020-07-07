package p2p

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
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
	defer func() {
		if err := stream.Close(); err != nil {
			pc.logger.Err(err).Msg("fail to close the stream")
		}
	}()
	remotePeer := stream.Conn().RemotePeer()
	logger := pc.logger.With().Str("remote peer", remotePeer.String()).Logger()
	logger.Debug().Msg("reading from join party request")
	payload, err := ReadStreamWithBuffer(stream)
	if err != nil {
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
		return
	}
	newFound, err := peerGroup.updatePeer(remotePeer)
	if err != nil {
		pc.logger.Error().Err(err).Msg("receive msg from unknown peer")
		return
	}
	if newFound {
		peerGroup.newFound <- true
	}
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

func (pc *PartyCoordinator) sendRequestToAll(msg *messages.JoinPartyRequest, peers []peer.ID, peerStreams map[peer.ID]network.Stream) error {
	var wg sync.WaitGroup
	var sendError error
	wg.Add(len(peers))
	for _, el := range peers {
		pStream := peerStreams[el]
		go func(peer peer.ID, s network.Stream) {
			defer wg.Done()
			if err := pc.sendRequestToPeer(msg, peer, pStream); err != nil {
				pc.logger.Error().Err(err).Msgf("error in send the join party request to peer %s", peer.String())
				sendError = err
				return
			}
		}(el, pStream)
	}
	wg.Wait()
	return sendError
}

func (pc *PartyCoordinator) sendRequestToPeer(msg *messages.JoinPartyRequest, remotePeer peer.ID, pStream network.Stream) error {
	msgBuf, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("fail to marshal msg to bytes: %w", err)
	}
	pc.logger.Info().Msgf("open stream to (%s) successfully", remotePeer)
	err = WriteStreamWithBuffer(msgBuf, pStream)
	if err != nil {
		if errReset := pStream.Reset(); errReset != nil {
			return errReset
		}
		return fmt.Errorf("fail to write message to stream:%w", err)
	}

	return nil
}

// JoinPartyWithRetry this method provide the functionality to join party with retry and back off
func (pc *PartyCoordinator) JoinPartyWithRetry(msg *messages.JoinPartyRequest, peers []string, peerStreams map[peer.ID]network.Stream) ([]peer.ID, error) {
	peerGroup, err := pc.createJoinPartyGroups(msg.ID, peers)
	if err != nil {
		pc.logger.Error().Err(err).Msg("fail to create the join party group")
		return nil, err
	}
	defer pc.removePeerGroup(msg.ID)
	_, offline := peerGroup.getPeersStatus()
	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		wg.Done()
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
				pc.logger.Info().Msg("we have found the new peer")
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
		pc.logger.Error().Err(err).Msg("fail to send join pary to some peers")
	}
	// we always set ourselves as online
	onlinePeers = append(onlinePeers, pc.host.ID())
	if len(onlinePeers) == len(peers) {
		return onlinePeers, nil
	}
	return onlinePeers, errJoinPartyTimeout
}

func (c *Communication) GetStream(remotePeer peer.ID, p protocol.ID) (network.Stream, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*4)
	defer cancel()
	var stream network.Stream
	var streamError error
	var err error
	streamGetChan := make(chan struct{})
	go func() {
		defer close(streamGetChan)
		for i := 0; i < 5; i++ {
			stream, err = c.host.NewStream(ctx, remotePeer, p)
			if err != nil {
				streamError = fmt.Errorf("fail to create stream to peer(%s):%w", remotePeer, err)
				c.logger.Error().Err(err).Msgf("fail to create stream with retry %d", i)
				time.Sleep(time.Second)
				fmt.Printf("\nwe continue......\n")
				continue
			}
			break
		}
	}()

	select {
	case <-streamGetChan:
		if streamError != nil {
			c.logger.Error().Err(streamError).Msg("fail to open stream")
			return nil, streamError
		}
	case <-ctx.Done():
		c.logger.Error().Err(ctx.Err()).Msg("fail to open stream with context timeout")
		// we reset the whole connection of this peer
		err := c.host.Network().ClosePeer(remotePeer)
		c.logger.Error().Err(err).Msgf("fail to clolse the connection to peer %s", remotePeer.String())
		return nil, ctx.Err()
	}

	return stream, nil
}
