package tss

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/libp2p/go-libp2p-core/peer"

	"gitlab.com/thorchain/tss/go-tss/blame"
	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/conversion"
	"gitlab.com/thorchain/tss/go-tss/keysign"
	"gitlab.com/thorchain/tss/go-tss/messages"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

func (t *TssServer) KeySign(req keysign.Request) (keysign.Response, error) {
	t.logger.Info().Str("pool pub key", req.PoolPubKey).
		Str("signer pub keys", strings.Join(req.SignerPubKeys, ",")).
		Str("msg", req.Message).
		Msg("received keysign request")
	emptyResp := keysign.Response{}
	msgID, err := t.requestToMsgId(req)
	if err != nil {
		return emptyResp, err
	}

	keysignInstance := keysign.NewTssKeySign(
		t.p2pCommunication.GetLocalPeerID(),
		t.conf,
		t.p2pCommunication.BroadcastMsgChan,
		t.stopChan,
		msgID,
		t.privateKey,
		t.p2pCommunication,
		t.stateManager,
	)

	keySignChannels := keysignInstance.GetTssKeySignChannels()
	t.p2pCommunication.SetSubscribe(messages.TSSKeySignMsg, msgID, keySignChannels)
	t.p2pCommunication.SetSubscribe(messages.TSSKeySignVerMsg, msgID, keySignChannels)
	t.p2pCommunication.SetSubscribe(messages.TSSControlMsg, msgID, keySignChannels)
	t.p2pCommunication.SetSubscribe(messages.TSSTaskDone, msgID, keySignChannels)

	defer func() {
		t.p2pCommunication.CancelSubscribe(messages.TSSKeySignMsg, msgID)
		t.p2pCommunication.CancelSubscribe(messages.TSSKeySignVerMsg, msgID)
		t.p2pCommunication.CancelSubscribe(messages.TSSControlMsg, msgID)
		t.p2pCommunication.CancelSubscribe(messages.TSSTaskDone, msgID)
	}()

	localStateItem, err := t.stateManager.GetLocalState(req.PoolPubKey)
	if err != nil {
		return emptyResp, fmt.Errorf("fail to get local keygen state: %w", err)
	}
	msgToSign, err := base64.StdEncoding.DecodeString(req.Message)
	if err != nil {
		return emptyResp, fmt.Errorf("fail to decode message(%s): %w", req.Message, err)
	}
	if len(req.SignerPubKeys) == 0 {
		return emptyResp, errors.New("empty signer pub keys")
	}

	threshold, err := common.GetThreshold(len(localStateItem.ParticipantKeys))
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to get the threshold")
		return emptyResp, errors.New("fail to get threshold")
	}
	if len(req.SignerPubKeys) <= threshold {
		t.logger.Error().Msgf("not enough signers, threshold=%d and signers=%d", threshold, len(req.SignerPubKeys))
		return emptyResp, errors.New("not enough signers")
	}

	peerIDs, err := conversion.GetPeerIDsFromPubKeys(req.SignerPubKeys)
	if err != nil {
		return keysign.Response{}, err
	}
	var streamsJoinparty sync.Map
	var streamsTss sync.Map
	var streamsNofifier sync.Map
	for _, el := range peerIDs {
		pid, err := peer.Decode(el)
		if el == t.GetLocalPeerID() {
			continue
		}
		if err != nil {
			return keysign.Response{}, fmt.Errorf("fail to decode peer id(%s):%w", el, err)
		}
		s, err := p2p.GetStream(&t.logger, t.p2pCommunication.GetHost(), pid, p2p.JoinPartyProtocol)
		if err != nil {
			return keysign.Response{}, fmt.Errorf("fail to get stream for stream (%s):%w", el, err)
		}
		s2, err := p2p.GetStream(&t.logger, t.p2pCommunication.GetHost(), pid, p2p.TSSProtocolID)
		if err != nil {
			return keysign.Response{}, fmt.Errorf("fail to get stream for stream (%s):%w", el, err)
		}
		s3, err := p2p.GetStream(&t.logger, t.p2pCommunication.GetHost(), pid, keysign.SignatureNotifierProtocol)
		if err != nil {
			return keysign.Response{}, fmt.Errorf("fail to get stream for stream (%s):%w", el, err)
		}
		streamsJoinparty.Store(pid, s)
		streamsTss.Store(pid, s2)
		streamsNofifier.Store(pid, s3)
	}
	defer p2p.ReleaseStream(&t.logger, &streamsJoinparty)
	defer p2p.ReleaseStream(&t.logger, &streamsTss)
	defer p2p.ReleaseStream(&t.logger, &streamsNofifier)
	keysignInstance.GetTssCommonStruct().UpdateStreams(&streamsTss)

	if !t.isPartOfKeysignParty(req.SignerPubKeys) {
		// TSS keysign include both form party and keysign itself, thus we wait twice of the timeout
		data, err := t.signatureNotifier.WaitForSignature(msgID, msgToSign, req.PoolPubKey, t.conf.KeySignTimeout)
		if err != nil {
			return emptyResp, fmt.Errorf("fail to get signature:%w", err)
		}
		if data == nil || (len(data.S) == 0 && len(data.R) == 0) {
			return emptyResp, errors.New("keysign failed")
		}
		return keysign.NewResponse(
			base64.StdEncoding.EncodeToString(data.R),
			base64.StdEncoding.EncodeToString(data.S),
			common.Success,
			blame.Blame{},
		), nil
	}
	blameMgr := keysignInstance.GetTssCommonStruct().GetBlameMgr()
	// get all the tss nodes that were part of the original key gen
	signers, err := conversion.GetPeerIDs(localStateItem.ParticipantKeys)
	if err != nil {
		return emptyResp, fmt.Errorf("fail to convert pub keys to peer id:%w", err)
	}

	for _, pid := range signers {
		if pid.String() == t.GetLocalPeerID() {
			continue
		}

		s3, err := p2p.GetStream(&t.logger, t.p2pCommunication.GetHost(), pid, keysign.SignatureNotifierProtocol)
		if err != nil {
			return keysign.Response{}, fmt.Errorf("fail to get stream for stream (%s):%w", pid, err)
		}
		streamsNofifier.Store(pid, s3)
	}
	defer p2p.ReleaseStream(&t.logger, &streamsNofifier)

	onlinePeers, err := t.joinParty(msgID, req.SignerPubKeys, &streamsJoinparty)
	if err != nil {
		if onlinePeers == nil {
			t.logger.Error().Err(err).Msg("error before we start join party")
			t.broadcastKeysignFailure(msgID, signers, &streamsNofifier)
			return keysign.Response{
				Status: common.Fail,
				Blame:  blame.NewBlame(blame.InternalError, []blame.Node{}),
			}, nil
		}

		blameNodes, err := blameMgr.NodeSyncBlame(req.SignerPubKeys, onlinePeers)
		if err != nil {
			t.logger.Err(err).Msg("fail to get peers to blame")
		}
		t.broadcastKeysignFailure(msgID, signers, &streamsNofifier)
		// make sure we blame the leader as well
		t.logger.Error().Err(err).Msgf("fail to form keysign party with online:%v", onlinePeers)
		return keysign.Response{
			Status: common.Fail,
			Blame:  blameNodes,
		}, nil
	}

	signatureData, err := keysignInstance.SignMessage(msgToSign, localStateItem, req.SignerPubKeys)
	// the statistic of keygen only care about Tss it self, even if the following http response aborts,
	// it still counted as a successful keygen as the Tss model runs successfully.
	if err != nil {
		t.logger.Error().Err(err).Msg("err in keysign")
		atomic.AddUint64(&t.Status.FailedKeySign, 1)
		t.broadcastKeysignFailure(msgID, signers, &streamsNofifier)
		blameNodes := *blameMgr.GetBlame()
		return keysign.Response{
			Status: common.Fail,
			Blame:  blameNodes,
		}, nil
	}

	atomic.AddUint64(&t.Status.SucKeySign, 1)

	// update signature notification
	if err := t.signatureNotifier.BroadcastSignature(msgID, signatureData, signers, &streamsNofifier); err != nil {
		return emptyResp, fmt.Errorf("fail to broadcast signature:%w", err)
	}
	return keysign.NewResponse(
		base64.StdEncoding.EncodeToString(signatureData.R),
		base64.StdEncoding.EncodeToString(signatureData.S),
		common.Success,
		blame.Blame{},
	), nil
}

func (t *TssServer) broadcastKeysignFailure(messageID string, peers []peer.ID, streams *sync.Map) {
	if err := t.signatureNotifier.BroadcastFailed(messageID, peers, streams); err != nil {
		t.logger.Err(err).Msg("fail to broadcast keysign failure")
	}
}

func (t *TssServer) isPartOfKeysignParty(parties []string) bool {
	for _, item := range parties {
		if t.localNodePubKey == item {
			return true
		}
	}
	return false
}
