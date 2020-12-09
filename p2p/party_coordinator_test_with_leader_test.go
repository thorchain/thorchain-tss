package p2p

import (
	"context"
	"math/rand"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/host"
	tnet "github.com/libp2p/go-libp2p-testing/net"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/assert"

	"gitlab.com/thorchain/tss/go-tss/conversion"
)

func init() {
	ApplyDeadline = false
}

func setupHosts(t *testing.T, n int) []host.Host {
	mn := mocknet.New(context.Background())
	var hosts []host.Host
	for i := 0; i < n; i++ {

		id := tnet.RandIdentityOrFatal(t)
		a := tnet.RandLocalTCPAddress()
		h, err := mn.AddPeer(id.PrivateKey(), a)
		if err != nil {
			t.Fatal(err)
		}
		hosts = append(hosts, h)
	}

	if err := mn.LinkAll(); err != nil {
		t.Error(err)
	}
	if err := mn.ConnectAllButSelf(); err != nil {
		t.Error(err)
	}
	return hosts
}

func leaderAppearsLastTest(t *testing.T, msgID string, peers []string, pcs []*PartyCoordinator) {
	wg := sync.WaitGroup{}

	for _, el := range pcs[1:] {
		wg.Add(1)
		go func(coordinator *PartyCoordinator) {
			defer wg.Done()
			// we simulate different nodes join at different time
			time.Sleep(time.Millisecond * time.Duration(rand.Int()%100))
			sigChan := make(chan string)
			onlinePeers, _, err := coordinator.JoinPartyWithLeader(msgID, 10, peers, 3, sigChan)
			assert.Nil(t, err)
			assert.Len(t, onlinePeers, 4)
		}(el)
	}

	time.Sleep(time.Second * 2)
	// we start the leader firstly
	wg.Add(1)
	go func(coordinator *PartyCoordinator) {
		defer wg.Done()
		sigChan := make(chan string)
		// we simulate different nodes join at different time
		onlinePeers, _, err := coordinator.JoinPartyWithLeader(msgID, 10, peers, 3, sigChan)
		assert.Nil(t, err)
		assert.Len(t, onlinePeers, 4)
	}(pcs[0])
	wg.Wait()
}

func leaderAppersFirstTest(t *testing.T, msgID string, peers []string, pcs []*PartyCoordinator) {
	wg := sync.WaitGroup{}
	wg.Add(1)
	// we start the leader firstly
	go func(coordinator *PartyCoordinator) {
		defer wg.Done()
		// we simulate different nodes join at different time
		sigChan := make(chan string)
		onlinePeers, _, err := coordinator.JoinPartyWithLeader(msgID, 10, peers, 3, sigChan)
		assert.Nil(t, err)
		assert.Len(t, onlinePeers, 4)
	}(pcs[0])
	time.Sleep(time.Second)
	for _, el := range pcs[1:] {
		wg.Add(1)
		go func(coordinator *PartyCoordinator) {
			defer wg.Done()
			// we simulate different nodes join at different time
			time.Sleep(time.Millisecond * time.Duration(rand.Int()%100))
			sigChan := make(chan string)
			onlinePeers, _, err := coordinator.JoinPartyWithLeader(msgID, 10, peers, 3, sigChan)
			assert.Nil(t, err)
			assert.Len(t, onlinePeers, 4)
		}(el)
	}
	wg.Wait()
}

func TestNewPartyCoordinator(t *testing.T) {
	hosts := setupHosts(t, 4)
	var pcs []*PartyCoordinator
	var peers []string

	timeout := time.Second * 4
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
	leaderAppersFirstTest(t, msgID, peers, pcs)
	leaderAppearsLastTest(t, msgID, peers, pcs)
}

func TestNewPartyCoordinatorTimeOut(t *testing.T) {
	timeout := time.Second * 3
	hosts := setupHosts(t, 4)
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
			sigChan := make(chan string)
			_, _, err := coordinator.JoinPartyWithLeader(msgID, 10, peers, 3, sigChan)
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
			sigChan := make(chan string)
			onlinePeers, _, err := coordinator.JoinPartyWithLeader(msgID, 10, peers, 3, sigChan)
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
