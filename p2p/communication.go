package p2p

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	relay "github.com/libp2p/go-libp2p-circuit"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/libp2p/go-libp2p-core/routing"
	discovery "github.com/libp2p/go-libp2p-discovery"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	swarm "github.com/libp2p/go-libp2p-swarm"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"gitlab.com/thorchain/tss/go-tss/messages"
)

var JoinPartyProtocol protocol.ID = "/p2p/join-party"

// TSSProtocolID protocol id used for tss
var TSSProtocolID protocol.ID = "/p2p/tss"

const (
	// TimeoutConnecting maximum time for wait for peers to connect
	TimeoutConnecting = time.Minute * 1
)

// Message that get transfer across the wire
type Message struct {
	PeerID  peer.ID
	Payload []byte
}

// Communication use p2p to broadcast messages among all the TSS nodes
type Communication struct {
	rendezvous       string // based on group
	bootstrapPeers   []maddr.Multiaddr
	logger           zerolog.Logger
	listenAddr       maddr.Multiaddr
	host             host.Host
	wg               *sync.WaitGroup
	stopChan         chan struct{} // channel to indicate whether we should stop
	subscribers      map[messages.THORChainTSSMessageType]*MessageIDSubscriber
	subscriberLocker *sync.Mutex
	streamCount      int64
	BroadcastMsgChan chan *messages.BroadcastMsgChan
	externalAddr     maddr.Multiaddr
}

// NewCommunication create a new instance of Communication
func NewCommunication(rendezvous string, bootstrapPeers []maddr.Multiaddr, port int, externalIP string) (*Communication, error) {
	addr, err := maddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port))
	if err != nil {
		return nil, fmt.Errorf("fail to create listen addr: %w", err)
	}
	var externalAddr maddr.Multiaddr = nil
	if len(externalIP) != 0 {
		externalAddr, err = maddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", externalIP, port))
		if err != nil {
			return nil, fmt.Errorf("fail to create listen with given external IP: %w", err)
		}
	}
	return &Communication{
		rendezvous:       rendezvous,
		bootstrapPeers:   bootstrapPeers,
		logger:           log.With().Str("module", "communication").Logger(),
		listenAddr:       addr,
		wg:               &sync.WaitGroup{},
		stopChan:         make(chan struct{}),
		subscribers:      make(map[messages.THORChainTSSMessageType]*MessageIDSubscriber),
		subscriberLocker: &sync.Mutex{},
		streamCount:      0,
		BroadcastMsgChan: make(chan *messages.BroadcastMsgChan, 1024),
		externalAddr:     externalAddr,
	}, nil
}

// GetHost return the host
func (c *Communication) GetHost() host.Host {
	return c.host
}

// GetLocalPeerID from p2p host
func (c *Communication) GetLocalPeerID() string {
	return c.host.ID().String()
}

// Broadcast message to Peers
func (c *Communication) Broadcast(peers []peer.ID, msg []byte, msgID string, streams *sync.Map) {
	if len(peers) == 0 {
		return
	}
	if streams == nil {
		c.logger.Error().Msg("the streams is nil")
		return
	}
	// try to discover all peers and then broadcast the messages
	c.wg.Add(1)
	go c.broadcastToPeers(peers, msg, msgID, streams)
}

func (c *Communication) broadcastToPeers(peers []peer.ID, msg []byte, msgID string, streams *sync.Map) {
	defer c.wg.Done()
	defer func() {
		c.logger.Debug().Msgf("finished sending message to peer(%v)", peers)
	}()

	streams.Range(func(key, value interface{}) bool {
		return false
	})

	for _, p := range peers {
		stream, ok := streams.Load(p)
		if !ok {
			c.logger.Error().Msg("fail to find stream of the given peer")
			continue
		}
		if err := c.writeToStream(p, msg, stream.(network.Stream)); nil != err {
			c.logger.Error().Err(err).Msg("fail to write to stream")
		}
	}
}

func (c *Communication) writeToStream(pID peer.ID, msg []byte, stream network.Stream) error {
	// don't send to ourselves
	if pID == c.host.ID() {
		return nil
	}
	if nil == stream {
		return nil
	}
	c.logger.Debug().Msgf(">>>writing messages to peer(%s)", pID)
	if ApplyDeadline {
		if err := stream.SetWriteDeadline(time.Now().Add(TimeoutWritePayload)); nil != err {
			if errReset := stream.Reset(); errReset != nil {
				return err
			}
			return err
		}
	}
	streamWrite := bufio.NewWriter(stream)
	return WriteStreamWithBuffer(msg, streamWrite)
}

func (c *Communication) readFromStream(stream network.Stream) {
	peerID := stream.Conn().RemotePeer().String()
	defer func() {
		if err := stream.Reset(); nil != err {
			c.logger.Error().Err(err).Msg("fail to close stream")
		}
	}()
	for {
		select {
		case <-c.stopChan:
			return
		default:
			streamReader := bufio.NewReader(stream)
			dataBuf, err := ReadStreamWithBuffer(streamReader)
			if err != nil {
				if err.Error() == "error in read the message head stream reset" {
					c.logger.Debug().Msgf("we release the stream as it is closed(reset)")
					return
				}
				c.logger.Error().Err(err).Msgf("fail to read from stream,peerID: %s", peerID)
				return
			}
			var wrappedMsg messages.WrappedMessage
			if err := json.Unmarshal(dataBuf, &wrappedMsg); nil != err {
				c.logger.Error().Err(err).Msg("fail to unmarshal wrapped message bytes")
				continue
			}
			c.logger.Debug().Msgf(">>>>>>>[%s] %s", wrappedMsg.MessageType, string(wrappedMsg.Payload))
			channel := c.getSubscriber(wrappedMsg.MessageType, wrappedMsg.MsgID)
			if nil == channel {
				c.logger.Info().Msgf("no MsgID %s found for this message,type=%s", wrappedMsg.MsgID, wrappedMsg.MessageType)
				continue
			}
			channel <- &Message{
				PeerID:  stream.Conn().RemotePeer(),
				Payload: dataBuf,
			}
		}
	}
}

func (c *Communication) handleStream(stream network.Stream) {
	// we will read from that stream
	go c.readFromStream(stream)
}

func (c *Communication) bootStrapConnectivityCheck() error {
	if len(c.bootstrapPeers) == 0 {
		c.logger.Info().Msg("we do not have the bootstrap node set, quit the connectivity check")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	for _, el := range c.bootstrapPeers {
		peer, err := peer.AddrInfoFromP2pAddr(el)
		if err != nil {
			c.logger.Error().Err(err).Msg("error in decode the bootstrap node, skip it")
			continue
		}

		outChan := ping.Ping(ctx, c.host, peer.ID)

		for {
			ret, ok := <-outChan
			if !ok {
				break
			}
			if ret.Error == nil {
				c.logger.Info().Msgf("connect to peer %v with RTT %v\n", peer.ID, ret.RTT)
				return nil
			}
		}
	}

	c.logger.Error().Msg("fail to connect to any bootstrap node")
	return errors.New("the node cannot connect to any bootstrap node")
}

func (c *Communication) startChannel(privKeyBytes []byte) error {
	ctx := context.Background()
	p2pPriKey, err := crypto.UnmarshalSecp256k1PrivateKey(privKeyBytes)
	if err != nil {
		c.logger.Error().Msgf("error is %f", err)
		return err
	}

	addressFactory := func(addrs []maddr.Multiaddr) []maddr.Multiaddr {
		if c.externalAddr != nil {
			addrs = append(addrs, c.externalAddr)
		}
		return addrs
	}

	h, err := libp2p.New(ctx,
		libp2p.ListenAddrs([]maddr.Multiaddr{c.listenAddr}...),
		libp2p.Identity(p2pPriKey),
		libp2p.AddrsFactory(addressFactory),
		libp2p.EnableAutoRelay(),
		libp2p.EnableRelay(relay.OptHop),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			// make the DHT with the given Host
			return dht.New(ctx, h)
		}),
	)
	swarm.BackoffBase = time.Millisecond * 500
	swarm.BackoffCoef = time.Millisecond * 100
	if err != nil {
		return fmt.Errorf("fail to create p2p host: %w", err)
	}
	c.host = h
	c.logger.Info().Msgf("Host created, we are: %s, at: %s", h.ID(), h.Addrs())
	h.SetStreamHandler(TSSProtocolID, c.handleStream)
	// Start a DHT, for use in peer discovery. We can't just make a new DHT
	// client because we want each peer to maintain its own local copy of the
	// DHT, so that the bootstrapping node of the DHT can go down without
	// inhibiting future peer discovery.
	kademliaDHT, err := dht.New(ctx, h)
	if err != nil {
		return fmt.Errorf("fail to create DHT: %w", err)
	}
	c.logger.Debug().Msg("Bootstrapping the DHT")
	if err = kademliaDHT.Bootstrap(ctx); err != nil {
		return fmt.Errorf("fail to bootstrap DHT: %w", err)
	}

	var connectionErr error
	for i := 0; i < 5; i++ {
		connectionErr = c.connectToBootstrapPeers()
		if connectionErr == nil {
			break
		}
		c.logger.Info().Msg("cannot connect to any bootstrap node, retry in 5 seconds")
		time.Sleep(time.Second * 5)
	}
	if connectionErr != nil {
		return fmt.Errorf("fail to connect to bootstrap peer: %w", connectionErr)
	}

	// We use a rendezvous point "meet me here" to announce our location.
	// This is like telling your friends to meet you at the Eiffel Tower.
	routingDiscovery := discovery.NewRoutingDiscovery(kademliaDHT)
	discovery.Advertise(ctx, routingDiscovery, c.rendezvous)
	err = c.bootStrapConnectivityCheck()
	if err != nil {
		return err
	}

	c.logger.Info().Msg("Successfully announced!")
	return nil
}

func (c *Communication) connectToOnePeer(pID peer.ID) (network.Stream, error) {
	c.logger.Debug().Msgf("peer:%s,current:%s", pID, c.host.ID())
	// dont connect to itself
	if pID == c.host.ID() {
		return nil, nil
	}
	c.logger.Debug().Msgf("connect to peer : %s", pID.String())
	ctx, cancel := context.WithTimeout(context.Background(), TimeoutConnecting)
	defer cancel()
	stream, err := c.host.NewStream(ctx, pID, TSSProtocolID)
	if err != nil {
		return nil, fmt.Errorf("fail to create new stream to peer: %s, %w", pID, err)
	}
	return stream, nil
}

func (c *Communication) connectToBootstrapPeers() error {
	// Let's connect to the bootstrap nodes first. They will tell us about the
	// other nodes in the network.
	if len(c.bootstrapPeers) == 0 {
		c.logger.Info().Msg("no bootstrap node set, we skip the connection")
		return nil
	}
	var wg sync.WaitGroup
	connRet := make(chan bool, len(c.bootstrapPeers))
	for _, peerAddr := range c.bootstrapPeers {
		pi, err := peer.AddrInfoFromP2pAddr(peerAddr)
		if err != nil {
			return fmt.Errorf("fail to add peer: %w", err)
		}
		wg.Add(1)
		go func(connRet chan bool) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), TimeoutConnecting)
			defer cancel()
			if err := c.host.Connect(ctx, *pi); err != nil {
				c.logger.Error().Err(err).Msgf("fail to connect to %s", pi.String())
				connRet <- false
				return
			}
			connRet <- true
			c.logger.Info().Msgf("Connection established with bootstrap node: %s", *pi)
		}(connRet)
	}
	wg.Wait()
	for i := 0; i < len(c.bootstrapPeers); i++ {
		if <-connRet {
			return nil
		}
	}
	return errors.New("fail to connect to any peer")
}

// Start will start the communication
func (c *Communication) Start(priKeyBytes []byte) error {
	err := c.startChannel(priKeyBytes)
	if err == nil {
		c.wg.Add(1)
		go c.ProcessBroadcast()
	}
	return err
}

// Stop communication
func (c *Communication) Stop() error {
	// we need to stop the handler and the p2p services firstly, then terminate the our communication threads
	if err := c.host.Close(); err != nil {
		c.logger.Err(err).Msg("fail to close host network")
	}

	close(c.stopChan)
	c.wg.Wait()
	return nil
}

func (c *Communication) SetSubscribe(topic messages.THORChainTSSMessageType, msgID string, channel chan *Message) {
	c.subscriberLocker.Lock()
	defer c.subscriberLocker.Unlock()

	messageIDSubscribers, ok := c.subscribers[topic]
	if !ok {
		messageIDSubscribers = NewMessageIDSubscriber()
		c.subscribers[topic] = messageIDSubscribers
	}
	messageIDSubscribers.Subscribe(msgID, channel)
}

func (c *Communication) getSubscriber(topic messages.THORChainTSSMessageType, msgID string) chan *Message {
	c.subscriberLocker.Lock()
	defer c.subscriberLocker.Unlock()
	messageIDSubscribers, ok := c.subscribers[topic]
	if !ok {
		c.logger.Debug().Msgf("fail to find subscribers for %s", topic)
		return nil
	}
	return messageIDSubscribers.GetSubscriber(msgID)
}

func (c *Communication) CancelSubscribe(topic messages.THORChainTSSMessageType, msgID string) {
	c.subscriberLocker.Lock()
	defer c.subscriberLocker.Unlock()

	messageIDSubscribers, ok := c.subscribers[topic]
	if !ok {
		c.logger.Debug().Msgf("cannot find the given channels %s", topic.String())
		return
	}
	if nil == messageIDSubscribers {
		return
	}
	messageIDSubscribers.UnSubscribe(msgID)
	if messageIDSubscribers.IsEmpty() {
		delete(c.subscribers, topic)
	}
}

func (c *Communication) ProcessBroadcast() {
	c.logger.Info().Msg("start to process broadcast message channel")
	defer c.logger.Info().Msg("stop process broadcast message channel")
	defer c.wg.Done()
	for {
		select {
		case msg := <-c.BroadcastMsgChan:
			wrappedMsgBytes, err := json.Marshal(msg.WrappedMessage)
			if err != nil {
				c.logger.Error().Err(err).Msg("fail to marshal a wrapped message to json bytes")
				continue
			}
			c.logger.Debug().Msgf("broadcast message %s to %+v", msg.WrappedMessage, msg.PeersID)
			c.Broadcast(msg.PeersID, wrappedMsgBytes, msg.WrappedMessage.MsgID, msg.Streams)

		case <-c.stopChan:
			return
		}
	}
}
