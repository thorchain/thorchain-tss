package main

import (
	"errors"
	"time"

	"gitlab.com/thorchain/tss/go-tss/blame"
	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/conversion"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/keygen/ecdsa"
	"gitlab.com/thorchain/tss/go-tss/keysign"
)

type MockTssServer struct {
	failToStart   bool
	failToKeyGen  bool
	failToKeySign bool
}

func (mts *MockTssServer) Start() error {
	if mts.failToStart {
		return errors.New("you ask for it")
	}
	return nil
}

func (mts *MockTssServer) Stop() {
}

func (mts *MockTssServer) GetLocalPeerID() string {
	return conversion.GetRandomPeerID().String()
}

func (mts *MockTssServer) Keygen(req keygen.Request) (ecdsa.Response, error) {
	if mts.failToKeyGen {
		return ecdsa.Response{}, errors.New("you ask for it")
	}
	return ecdsa.NewResponse(conversion.GetRandomPubKey(), "whatever", common.Success, blame.Blame{}), nil
}

func (mts *MockTssServer) KeySign(req keysign.Request) (keysign.Response, error) {
	if mts.failToKeySign {
		return keysign.Response{}, errors.New("you ask for it")
	}
	return keysign.NewResponse("", "", common.Success, blame.Blame{}), nil
}

func (mts *MockTssServer) GetStatus() common.TssStatus {
	return common.TssStatus{
		Starttime:     time.Now(),
		SucKeyGen:     0,
		FailedKeyGen:  0,
		SucKeySign:    0,
		FailedKeySign: 0,
	}
}
