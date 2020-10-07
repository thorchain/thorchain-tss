package p2p

import (
	"context"
	"math/rand"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	tnet "github.com/libp2p/go-libp2p-testing/net"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/assert"

	"gitlab.com/thorchain/tss/go-tss/conversion"
)

func setupHosts(t *testing.T, n int) ([]host.Host, map[peer.ID]tnet.Identity, mocknet.Mocknet) {
	mn := mocknet.New(context.Background())
	var hosts []host.Host
	ids := make(map[peer.ID]tnet.Identity)
	for i := 0; i < n; i++ {

		id := tnet.RandIdentityOrFatal(t)
		a := tnet.RandLocalTCPAddress()
		h, err := mn.AddPeer(id.PrivateKey(), a)
		if err != nil {
			t.Fatal(err)
		}
		hosts = append(hosts, h)
		ids[h.ID()] = id
	}

	if err := mn.LinkAll(); err != nil {
		t.Error(err)
	}
	if err := mn.ConnectAllButSelf(); err != nil {
		t.Error(err)
	}
	return hosts, ids, mn
}

func leaderAppearsLastTest(t *testing.T, msgID string, peers []string, pcs []*PartyCoordinator, ids map[peer.ID]tnet.Identity, isBroadcast bool) {
	wg := sync.WaitGroup{}
	for _, el := range pcs[1:] {
		wg.Add(1)
		go func(coordinator *PartyCoordinator) {
			defer wg.Done()
			privKey := ids[coordinator.host.ID()]
			sig, err := privKey.PrivateKey().Sign([]byte(msgID))
			assert.Nil(t, err)
			// we simulate different nodes join at different time
			time.Sleep(time.Millisecond * time.Duration(rand.Int()%100))
			sigChan := make(chan string)
			onlinePeers, _, err := coordinator.JoinPartyWithLeader(msgID, sig, 10, peers, 3, sigChan, isBroadcast)
			assert.Nil(t, err)
			assert.Len(t, onlinePeers, 4)
		}(el)
	}

	time.Sleep(time.Second * 1)
	// we start the leader firstly
	wg.Add(1)
	go func(coordinator *PartyCoordinator) {
		defer wg.Done()
		coordinator.timeout = time.Second * 6
		sigChan := make(chan string)
		privKey := ids[coordinator.host.ID()]
		sig, err := privKey.PrivateKey().Sign([]byte(msgID))
		assert.Nil(t, err)
		// we simulate different nodes join at different time
		onlinePeers, _, err := coordinator.JoinPartyWithLeader(msgID, sig, 10, peers, 3, sigChan, isBroadcast)
		assert.Nil(t, err)
		assert.Len(t, onlinePeers, 4)
	}(pcs[0])
	wg.Wait()
}

func avoidSendingToLeader(t *testing.T, mn mocknet.Mocknet, msgID string, peers []string, pcs []*PartyCoordinator, ids map[peer.ID]tnet.Identity, isBroadcast bool) {
	// we assume that node 2,3 cannot communicate with the leader need the relay of node 1
	mn.UnlinkPeers(pcs[0].host.ID(), pcs[2].host.ID())
	mn.DisconnectPeers(pcs[0].host.ID(), pcs[2].host.ID())
	pcs[1].timeout = time.Second * 7
	defer func() {
		mn.LinkPeers(pcs[0].host.ID(), pcs[2].host.ID())
		mn.ConnectPeers(pcs[0].host.ID(), pcs[2].host.ID())
		pcs[1].timeout = time.Second * 5
	}()

	wg := sync.WaitGroup{}
	wg.Add(1)
	// we start the leader firstly
	go func(coordinator *PartyCoordinator) {
		defer wg.Done()
		privKey := ids[coordinator.host.ID()]
		sig, err := privKey.PrivateKey().Sign([]byte(msgID))
		assert.Nil(t, err)
		// we simulate different nodes join at different time
		sigChan := make(chan string)
		onlinePeers, _, err := coordinator.JoinPartyWithLeader(msgID, sig, 10, peers, 3, sigChan, isBroadcast)
		assert.Nil(t, err)
		assert.Len(t, onlinePeers, 4)
	}(pcs[0])
	time.Sleep(time.Second)
	for i, el := range pcs[1:] {
		wg.Add(1)
		go func(idx int, coordinator *PartyCoordinator) {
			defer wg.Done()
			var onlinePeers []peer.ID
			privKey := ids[coordinator.host.ID()]
			sig, err := privKey.PrivateKey().Sign([]byte(msgID))
			assert.Nil(t, err)
			// we simulate different nodes join at different time
			time.Sleep(time.Millisecond * time.Duration(rand.Int()%100))
			sigChan := make(chan string)
			onlinePeers, _, err = coordinator.JoinPartyWithLeader(msgID, sig, 10, peers, 3, sigChan, isBroadcast)
			assert.Nil(t, err)
			assert.Len(t, onlinePeers, 4)
		}(i, el)
	}
	wg.Wait()
}

func leaderAppersFirstTest(t *testing.T, msgID string, peers []string, pcs []*PartyCoordinator, ids map[peer.ID]tnet.Identity, isBroadcast bool) {
	wg := sync.WaitGroup{}
	wg.Add(1)
	// we start the leader firstly
	go func(coordinator *PartyCoordinator) {
		defer wg.Done()
		privKey := ids[coordinator.host.ID()]
		sig, err := privKey.PrivateKey().Sign([]byte(msgID))
		assert.Nil(t, err)
		// we simulate different nodes join at different time
		sigChan := make(chan string)
		onlinePeers, _, err := coordinator.JoinPartyWithLeader(msgID, sig, 10, peers, 3, sigChan, isBroadcast)
		assert.Nil(t, err)
		assert.Len(t, onlinePeers, 4)
	}(pcs[0])
	time.Sleep(time.Second)
	for _, el := range pcs[1:] {
		wg.Add(1)
		go func(coordinator *PartyCoordinator) {
			defer wg.Done()

			privKey := ids[coordinator.host.ID()]
			sig, err := privKey.PrivateKey().Sign([]byte(msgID))
			assert.Nil(t, err)
			// we simulate different nodes join at different time
			time.Sleep(time.Millisecond * time.Duration(rand.Int()%100))
			sigChan := make(chan string)
			onlinePeers, _, err := coordinator.JoinPartyWithLeader(msgID, sig, 10, peers, 3, sigChan, isBroadcast)
			assert.Nil(t, err)
			assert.Len(t, onlinePeers, 4)
		}(el)
	}
	wg.Wait()
}

func TestNewPartyCoordinator(t *testing.T) {
	ApplyDeadline = false
	hosts, ids, _ := setupHosts(t, 4)
	var pcs []*PartyCoordinator
	var peers []string
	timeout := time.Second * 5
	for _, el := range hosts {
		pcs = append(pcs, NewPartyCoordinator(el, timeout))
		peers = append(peers, el.ID().String())
	}

	defer func() {
		for _, el := range pcs {
			el.Stop()
		}
	}()

	msgID := conversion.RandStringBytesMask(64)

	leader, err := LeaderNode(msgID, 10, peers)
	assert.Nil(t, err)

	// we sort the slice to ensure the leader is the first one easy for testing
	for i, el := range pcs {
		if el.host.ID().String() == leader {
			if i == 0 {
				break
			}
			temp := pcs[0]
			pcs[0] = el
			pcs[i] = temp
			break
		}
	}
	assert.Equal(t, pcs[0].host.ID().String(), leader)
	// now we test the leader appears firstly and the the members
	leaderAppersFirstTest(t, msgID, peers, pcs, ids, false)
	leaderAppearsLastTest(t, msgID, peers, pcs, ids, false)

	leaderAppersFirstTest(t, msgID, peers, pcs, ids, true)

	leaderAppearsLastTest(t, msgID, peers, pcs, ids, true)
}

func TestNewPartyCoordinatorBroadcast(t *testing.T) {
	ApplyDeadline = false
	hosts, ids, mn := setupHosts(t, 4)
	var pcs []*PartyCoordinator
	var peers []string
	timeout := time.Second * 5
	for _, el := range hosts {
		pcs = append(pcs, NewPartyCoordinator(el, timeout))
		peers = append(peers, el.ID().String())
	}

	defer func() {
		for _, el := range pcs {
			el.Stop()
		}
	}()

	msgID := conversion.RandStringBytesMask(64)

	leader, err := LeaderNode(msgID, 10, peers)
	assert.Nil(t, err)

	// we sort the slice to ensure the leader is the first one easy for testing
	for i, el := range pcs {
		if el.host.ID().String() == leader {
			if i == 0 {
				break
			}
			temp := pcs[0]
			pcs[0] = el
			pcs[i] = temp
			break
		}
	}
	assert.Equal(t, pcs[0].host.ID().String(), leader)
	// now we test the leader appears firstly and the the members
	avoidSendingToLeader(t, mn, msgID, peers, pcs, ids, true)
}

func TestNewPartyCoordinatorBroadcastTimeOut(t *testing.T) {
	ApplyDeadline = false
	timeout := time.Second * 3
	hosts, ids, _ := setupHosts(t, 4)
	var pcs []*PartyCoordinator
	var peers []string
	for _, el := range hosts {
		pcs = append(pcs, NewPartyCoordinator(el, timeout))
	}
	sort.Slice(pcs, func(i, j int) bool {
		return pcs[i].host.ID().String() > pcs[j].host.ID().String()
	})
	for _, el := range pcs {
		peers = append(peers, el.host.ID().String())
	}

	defer func() {
		for _, el := range pcs {
			el.Stop()
		}
	}()

	msgID := conversion.RandStringBytesMask(64)
	wg := sync.WaitGroup{}
	leader, err := LeaderNode(msgID, 10, peers)
	assert.Nil(t, err)

	// we sort the slice to ensure the leader is the first one easy for testing
	for i, el := range pcs {
		if el.host.ID().String() == leader {
			if i == 0 {
				break
			}
			temp := pcs[0]
			pcs[0] = el
			pcs[i] = temp
			break
		}
	}
	assert.Equal(t, pcs[0].host.ID().String(), leader)

	// we test the leader is offline
	for _, el := range pcs[1:] {
		wg.Add(1)
		expectedOnline := []string{pcs[1].host.ID().String(), pcs[2].host.ID().String(), pcs[3].host.ID().String()}
		sort.Strings(expectedOnline)
		go func(coordinator *PartyCoordinator) {
			defer wg.Done()
			privKey := ids[coordinator.host.ID()]
			sig, err := privKey.PrivateKey().Sign([]byte(msgID))
			assert.Nil(t, err)
			sigChan := make(chan string)
			onlines, _, err := coordinator.JoinPartyWithLeader(msgID, sig, 10, peers, 3, sigChan, true)
			var result []string
			for _, el := range onlines {
				result = append(result, el.String())
			}
			sort.Strings(result)
			assert.EqualValues(t, expectedOnline, result)
			assert.Equal(t, err, ErrLeaderNotReady)
		}(el)

	}
	wg.Wait()
	// we test two of nodes is not ready
	msgID = conversion.RandStringBytesMask(64)
	var expected []string
	for _, el := range pcs[:3] {
		expected = append(expected, el.host.ID().String())
		wg.Add(1)
		go func(coordinator *PartyCoordinator) {
			defer wg.Done()
			privKey := ids[coordinator.host.ID()]
			sig, err := privKey.PrivateKey().Sign([]byte(msgID))
			assert.Nil(t, err)
			sigChan := make(chan string)
			onlinePeers, _, err := coordinator.JoinPartyWithLeader(msgID, sig, 10, peers, 3, sigChan, true)
			assert.Equal(t, ErrJoinPartyTimeout, err)
			var onlinePeersStr []string
			for _, el := range onlinePeers {
				onlinePeersStr = append(onlinePeersStr, el.String())
			}
			sort.Strings(onlinePeersStr)
			sort.Strings(expected)
			sort.Strings(expected[:2])
			assert.EqualValues(t, expected, onlinePeersStr)
		}(el)
	}
	wg.Wait()
}

func TestNewPartyCoordinatorTimeOut(t *testing.T) {
	ApplyDeadline = false
	timeout := time.Second * 3
	hosts, ids, _ := setupHosts(t, 4)
	var pcs []*PartyCoordinator
	var peers []string
	for _, el := range hosts {
		pcs = append(pcs, NewPartyCoordinator(el, timeout))
	}
	sort.Slice(pcs, func(i, j int) bool {
		return pcs[i].host.ID().String() > pcs[j].host.ID().String()
	})
	for _, el := range pcs {
		peers = append(peers, el.host.ID().String())
	}

	defer func() {
		for _, el := range pcs {
			el.Stop()
		}
	}()

	msgID := conversion.RandStringBytesMask(64)
	wg := sync.WaitGroup{}
	leader, err := LeaderNode(msgID, 10, peers)
	assert.Nil(t, err)

	// we sort the slice to ensure the leader is the first one easy for testing
	for i, el := range pcs {
		if el.host.ID().String() == leader {
			if i == 0 {
				break
			}
			temp := pcs[0]
			pcs[0] = el
			pcs[i] = temp
			break
		}
	}
	assert.Equal(t, pcs[0].host.ID().String(), leader)

	// we test the leader is offline
	for _, el := range pcs[1:] {
		wg.Add(1)
		go func(coordinator *PartyCoordinator) {
			defer wg.Done()
			privKey := ids[coordinator.host.ID()]
			sig, err := privKey.PrivateKey().Sign([]byte(msgID))
			assert.Nil(t, err)
			sigChan := make(chan string)
			_, _, err = coordinator.JoinPartyWithLeader(msgID, sig, 10, peers, 3, sigChan, false)
			assert.Equal(t, err, ErrLeaderNotReady)
		}(el)

	}
	wg.Wait()
	// we test one of node is not ready
	var expected []string
	for _, el := range pcs[:3] {
		expected = append(expected, el.host.ID().String())
		wg.Add(1)
		go func(coordinator *PartyCoordinator) {
			defer wg.Done()
			privKey := ids[coordinator.host.ID()]
			sig, err := privKey.PrivateKey().Sign([]byte(msgID))
			assert.Nil(t, err)
			sigChan := make(chan string)
			onlinePeers, _, err := coordinator.JoinPartyWithLeader(msgID, sig, 10, peers, 3, sigChan, false)
			assert.Equal(t, ErrJoinPartyTimeout, err)
			var onlinePeersStr []string
			for _, el := range onlinePeers {
				onlinePeersStr = append(onlinePeersStr, el.String())
			}
			sort.Strings(onlinePeersStr)
			sort.Strings(expected)
			sort.Strings(expected[:3])
			assert.EqualValues(t, expected, onlinePeersStr)
		}(el)
	}
	wg.Wait()
}

func TestGetPeerIDs(t *testing.T) {
	ApplyDeadline = false
	id1 := tnet.RandIdentityOrFatal(t)
	mn := mocknet.New(context.Background())
	// add peers to mock net

	a1 := tnet.RandLocalTCPAddress()
	h1, err := mn.AddPeer(id1.PrivateKey(), a1)
	if err != nil {
		t.Fatal(err)
	}
	p1 := h1.ID()
	timeout := time.Second * 2
	pc := NewPartyCoordinator(h1, timeout)
	r, err := pc.getPeerIDs([]string{})
	assert.Nil(t, err)
	assert.Len(t, r, 0)
	input := []string{
		p1.String(),
	}
	r1, err := pc.getPeerIDs(input)
	assert.Nil(t, err)
	assert.Len(t, r1, 1)
	assert.Equal(t, r1[0], p1)
	input = append(input, "whatever")
	r2, err := pc.getPeerIDs(input)
	assert.NotNil(t, err)
	assert.Len(t, r2, 0)
}
