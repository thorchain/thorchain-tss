package tss

import (
	"fmt"
	"sync/atomic"

	"github.com/libp2p/go-libp2p-core/network"
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
	peerStream := make(map[peer.ID]network.Stream)

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
		t.p2pCommunication, peerStream)

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

	for _, el := range peerIDs {
		pid, err := peer.Decode(el)
		if el == t.GetLocalPeerID() {
			continue
		}
		if err != nil {
			return keygen.Response{}, fmt.Errorf("fail to decode peer id(%s):%w", el, err)
		}
		s, err := t.p2pCommunication.GetStream(pid, p2p.JoinPartyProtocol)
		if err != nil {
			return keygen.Response{}, fmt.Errorf("fail to get stream for stream (%s):%w", el, err)
		}
		peerStream[pid] = s
	}
	defer p2p.ReleaseStream(&t.logger, peerStream)

	onlinePeers, err := t.joinParty(msgID, req.Keys, peerStream)
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
		t.logger.Error().Err(err).Msgf("fail to form keysign party with online:%v", onlinePeers)
		return keygen.Response{
			Status: common.Fail,
			Blame:  blameNodes,
		}, nil

	}

	t.logger.Info().Msg("keygen party formed")
	p2p.SetStreamsProtocol(peerStream, p2p.TSSProtocolID)
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
