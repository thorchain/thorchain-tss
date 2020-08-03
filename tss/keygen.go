package tss

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"gitlab.com/thorchain/tss/go-tss/blame"
	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/conversion"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/messages"
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

	defer func() {
		t.p2pCommunication.CancelSubscribe(messages.TSSKeyGenMsg, msgID)
		t.p2pCommunication.CancelSubscribe(messages.TSSKeyGenVerMsg, msgID)
		t.p2pCommunication.CancelSubscribe(messages.TSSControlMsg, msgID)
		t.p2pCommunication.CancelSubscribe(messages.TSSTaskDone, msgID)

		t.p2pCommunication.ReleaseStream(msgID)
		t.partyCoordinator.ReleaseStream(msgID)
	}()

	onlinePeers, err := t.joinParty(msgID, req.Keys)
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
		fmt.Println("doneeeeee???")
		var wg sync.WaitGroup
		finishChan := make(chan struct{})
		wg.Add(1)
		go keygenInstance.GetTssCommonStruct().ProcessJoinPartyTaskDone(finishChan, &wg, len(onlinePeers)-1)
		time.Sleep(time.Second * 5)
		keygenInstance.GetTssCommonStruct().P2PPeers = onlinePeers
		keygenInstance.GetTssCommonStruct().NotifyTaskDone(false, blameNodes)
		select {
		case <-time.After(time.Second * 5):
			close(finishChan)

		case <-keygenInstance.GetTssCommonStruct().GetTaskDone():
			close(finishChan)
		}
		wg.Wait()
		globalBlameNode := blameMgr.GetGlobalBlame()
		blameNodes.BlameNodes = []blame.Node{{
			Pubkey: globalBlameNode,
		}}
		t.logger.Error().Err(err).Msgf("fail to form keysign party with online:%v", onlinePeers)
		return keygen.Response{
			Status: common.Fail,
			Blame:  blameNodes,
		}, nil

	}

	t.logger.Debug().Msg("keygen party formed")
	// the statistic of keygen only care about Tss it self, even if the
	// following http response aborts, it still counted as a successful keygen
	// as the Tss model runs successfully.
	k, err := keygenInstance.GenerateNewKey(req)
	blameMgr := keygenInstance.GetTssCommonStruct().GetBlameMgr()
	if err != nil {
		atomic.AddUint64(&t.Status.FailedKeyGen, 1)
		t.logger.Error().Err(err).Msg("err in keygen")
		blameNodes := blameMgr.GetBlame()
		globalBlameNode := blameMgr.GetGlobalBlame()
		// replace the local blame nodes with the global one
		blameNodes.BlameNodes = []blame.Node{{
			Pubkey: globalBlameNode,
		}}
		return keygen.NewResponse("", "", common.Fail, *blameNodes), err
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
