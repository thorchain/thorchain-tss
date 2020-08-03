package blame

import (
	"errors"
	"fmt"

	btss "github.com/binance-chain/tss-lib/tss"
	mapset "github.com/deckarep/golang-set"
	"github.com/libp2p/go-libp2p-core/peer"

	"gitlab.com/thorchain/tss/go-tss/conversion"
	"gitlab.com/thorchain/tss/go-tss/messages"
)

func (m *Manager) tssTimeoutBlame(lastMessageType string, partyIDMap map[string]*btss.PartyID) ([]string, error) {
	peersSet := mapset.NewSet()
	for _, el := range partyIDMap {
		if el.Id != m.partyInfo.Party.PartyID().Id {
			peersSet.Add(el.Id)
		}
	}
	standbyNodes := m.roundMgr.GetByRound(lastMessageType)
	if len(standbyNodes) == 0 {
		return nil, nil
	}
	s := make([]interface{}, len(standbyNodes))
	for i, v := range standbyNodes {
		s[i] = v
	}
	standbySet := mapset.NewSetFromSlice(s)

	var blames []string
	diff := peersSet.Difference(standbySet).ToSlice()
	for _, el := range diff {
		blames = append(blames, el.(string))
	}

	blamePubKeys, err := conversion.AccPubKeysFromPartyIDs(blames, m.partyInfo.PartyIDMap)
	if err != nil {
		m.logger.Error().Err(err).Msg("fail to get the public keys of the blame node")
		return nil, err
	}

	return blamePubKeys, nil
}

// this blame blames the node who cause the timeout in node sync
func (m *Manager) NodeSyncBlame(keys []string, onlinePeers []peer.ID) (Blame, error) {
	blame := Blame{
		FailReason: TssSyncFail,
	}
	for _, item := range keys {
		found := false
		peerID, err := conversion.GetPeerIDFromPubKey(item)
		if err != nil {
			return blame, fmt.Errorf("fail to get peer id from pub key")
		}
		for _, p := range onlinePeers {
			if p == peerID {
				found = true
				break
			}
		}
		if !found {
			blame.BlameNodes = append(blame.BlameNodes, NewNode(item, nil, nil))
		}
	}
	return blame, nil
}

// this blame blames the node who cause the timeout in unicast message
func (m *Manager) GetUnicastBlame(lastMsgType string) ([]Node, error) {
	if len(m.lastUnicastPeer) == 0 {
		m.logger.Debug().Msg("we do not have any unicast message received yet")
		return nil, nil
	}
	peersMap := make(map[string]bool)
	peersID, ok := m.lastUnicastPeer[lastMsgType]
	if !ok {
		return nil, fmt.Errorf("fail to find peers of the given msg type %w", ErrTssTimeOut)
	}
	for _, el := range peersID {
		peersMap[el.String()] = true
	}

	var onlinePeers []string
	for key := range peersMap {
		onlinePeers = append(onlinePeers, key)
	}
	_, blamePeers, err := m.GetBlamePubKeysLists(onlinePeers)
	if err != nil {
		m.logger.Error().Err(err).Msg("fail to get the blamed peers")
		return nil, fmt.Errorf("fail to get the blamed peers %w", ErrTssTimeOut)
	}
	var blameNodes []Node
	for _, el := range blamePeers {
		blameNodes = append(blameNodes, NewNode(el, nil, nil))
	}
	return blameNodes, nil
}

// this blame blames the node who cause the timeout in broadcast message
func (m *Manager) GetBroadcastBlame(lastMessageType string) ([]Node, error) {
	blamePeers, err := m.tssTimeoutBlame(lastMessageType, m.partyInfo.PartyIDMap)
	if err != nil {
		m.logger.Error().Err(err).Msg("fail to get the blamed peers")
		return nil, fmt.Errorf("fail to get the blamed peers %w", ErrTssTimeOut)
	}
	var blameNodes []Node
	for _, el := range blamePeers {
		blameNodes = append(blameNodes, NewNode(el, nil, nil))
	}
	return blameNodes, nil
}

// this blame blames the node who provide the wrong share
func (m *Manager) TssWrongShareBlame(wiredMsg *messages.WireMessage) (string, error) {
	shareOwner := wiredMsg.Routing.From
	owner, ok := m.partyInfo.PartyIDMap[shareOwner.Id]
	if !ok {
		m.logger.Error().Msg("cannot find the blame node public key")
		return "", errors.New("fail to find the share Owner")
	}
	pk, err := conversion.PartyIDtoPubKey(owner)
	if err != nil {
		return "", err
	}
	return pk, nil
}

func (m *Manager) GetGlobalBlame() string {
	votes := make(map[string]int)
	m.peerBlame.Store("self", *m.blame)
	m.peerBlame.Range(func(_, value interface{}) bool {
		blame := value.(Blame)
		for _, el := range blame.BlameNodes {
			pubKey := el.Pubkey
			_, ok := votes[pubKey]
			if ok {
				votes[pubKey] += 1
				continue
			}
			votes[pubKey] = 1
		}
		return true
	})
	fmt.Printf("----#####map######-----%v\n", votes)
	c := 0
	blameNode := ""
	for k, v := range votes {
		if v > c {
			c = v
			blameNode = k
		}
	}
	return blameNode
}
