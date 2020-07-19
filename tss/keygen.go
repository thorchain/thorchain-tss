package tss

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/libp2p/go-libp2p-core/peer"

	"gitlab.com/thorchain/tss/go-tss/blame"
	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/conversion"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/messages"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

func (t *TssServer) Keygen(req keygen.Request) (keygen.Response, error) {
	t.tssKeyGenLocker.Lock()
	defer t.tssKeyGenLocker.Unlock()
	status := common.Success
	msgID, err := t.requestToMsgId(req)
	if err != nil {
		return keygen.Response{}, err
	}

	keygenInstance := keygen.NewTssKeyGen(
		t.p2pCommunication.GetLocalPeerID(),
		t.conf,
		t.localNodePubKey,
		t.p2pCommunication.BroadcastMsgChan,
		t.stopChan,
		t.preParams,
		msgID,
		t.stateManager,
		t.privateKey,
		t.p2pCommunication)

	keygenMsgChannel := keygenInstance.GetTssKeyGenChannels()
	t.p2pCommunication.SetSubscribe(messages.TSSKeyGenMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(messages.TSSKeyGenVerMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(messages.TSSControlMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(messages.TSSTaskDone, msgID, keygenMsgChannel)

	defer t.p2pCommunication.CancelSubscribe(messages.TSSKeyGenMsg, msgID)
	defer t.p2pCommunication.CancelSubscribe(messages.TSSKeyGenVerMsg, msgID)
	defer t.p2pCommunication.CancelSubscribe(messages.TSSControlMsg, msgID)
	defer t.p2pCommunication.CancelSubscribe(messages.TSSTaskDone, msgID)

	// now we get the stream for all the communications

	peerIDs, err := conversion.GetPeerIDsFromPubKeys(req.Keys)
	if err != nil {
		return keygen.Response{}, err
	}
	var streamsJoinParty sync.Map
	var streamsTss sync.Map
	streamPeers := []peer.ID{t.p2pCommunication.GetHost().ID()}
	for _, el := range peerIDs {
		pid, err := peer.Decode(el)
		if err != nil {
			return keygen.Response{}, fmt.Errorf("fail to decode peer id(%s):%w", el, err)
		}
		if pid == t.p2pCommunication.GetHost().ID() {
			continue
		}
		s, err := p2p.GetStream(&t.logger, t.p2pCommunication.GetHost(), pid, p2p.JoinPartyProtocol)
		if err != nil {
			t.logger.Error().Err(err).Msgf("fail to create stream to peer %s", pid)
			continue
		}
		s2, err := p2p.GetStream(&t.logger, t.p2pCommunication.GetHost(), pid, p2p.TSSProtocolID)
		if err != nil {
			t.logger.Error().Err(err).Msgf("fail to create stream to peer %s", pid)
			continue
		}
		streamPeers = append(streamPeers, pid)
		streamsJoinParty.Store(pid, s)
		streamsTss.Store(pid, s2)
	}
	defer p2p.ReleaseStream(&t.logger, &streamsJoinParty)
	defer p2p.ReleaseStream(&t.logger, &streamsTss)
	if len(streamPeers) != len(peerIDs) {
		// we have some peers fail to connect
		blameMgr := keygenInstance.GetTssCommonStruct().GetBlameMgr()
		blameNodes, err := blameMgr.NodeSyncBlame(req.Keys, streamPeers)
		if err != nil {
			t.logger.Err(err).Msg("fail to get peers to blame")
		}
		fmt.Printf("#########we(%s)---11---%v>>>%v\n", t.p2pCommunication.GetHost().ID(), len(blameNodes.BlameNodes), blameNodes.String())
		t.logger.Error().Err(err).Msgf("fail to open stream with blame nodes:%v", blameNodes.BlameNodes)
		return keygen.Response{
			Status: common.Fail,
			Blame:  blameNodes,
		}, nil
	}

	keygenInstance.GetTssCommonStruct().UpdateStreams(&streamsTss)
	onlinePeers, err := t.joinParty(msgID, req.Keys, &streamsJoinParty)
	if err != nil {
		if onlinePeers == nil {
			t.logger.Error().Err(err).Msg("error before we start join party")
			return keygen.Response{
				Status: common.Fail,
				Blame:  blame.NewBlame(blame.InternalError, []blame.Node{}),
			}, nil
		}
		blameMgr := keygenInstance.GetTssCommonStruct().GetBlameMgr()
		blameNodes, err := blameMgr.NodeSyncBlame(req.Keys, onlinePeers)
		if err != nil {
			t.logger.Err(err).Msg("fail to get peers to blame")
		}
		// make sure we blame the leader as well
		t.logger.Error().Err(err).Msgf("fail to form keygen party with online:%v", onlinePeers)
		return keygen.Response{
			Status: common.Fail,
			Blame:  blameNodes,
		}, nil

	}

	t.logger.Info().Msg("keygen party formed")
	// the statistic of keygen only care about Tss it self, even if the
	// following http response aborts, it still counted as a successful keygen
	// as the Tss model runs successfully.
	k, err := keygenInstance.GenerateNewKey(req)
	blameMgr := keygenInstance.GetTssCommonStruct().GetBlameMgr()
	if err != nil {
		atomic.AddUint64(&t.Status.FailedKeyGen, 1)
		t.logger.Error().Err(err).Msg("err in keygen")
		blameNodes := *blameMgr.GetBlame()
		return keygen.NewResponse("", "", common.Fail, blameNodes), err
	} else {
		atomic.AddUint64(&t.Status.SucKeyGen, 1)
	}

	newPubKey, addr, err := conversion.GetTssPubKey(k)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to generate the new Tss key")
		status = common.Fail
	}

	blameNodes := *blameMgr.GetBlame()
	return keygen.NewResponse(
		newPubKey,
		addr.String(),
		status,
		blameNodes,
	), nil
}
