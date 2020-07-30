package common

import (
	"time"
)

type TssConfig struct {
	// Party Timeout defines how long do we wait for the party to form
	PartyTimeout time.Duration
	// KeyGenTimeoutSeconds defines how long do we wait the keygen parties to pass messages along
	KeyGenTimeout time.Duration
	// KeySignTimeoutSeconds defines how long do we wait keysign
	KeySignTimeout time.Duration
	// Pre-parameter define the pre-parameter generations timeout
	PreParamTimeout time.Duration
	// attack related options
	Attacker      int
	AttackUnicast bool
	AttackPhrase  string
	AttackNodes   string
}

type TssStatus struct {
	// Starttime indicates when the Tss server starts
	Starttime time.Time `json:"start_time"`
	// SucKeyGen indicates how many times we run keygen successfully
	SucKeyGen uint64 `json:"successful_keygen"`
	// FailedKeyGen indicates how many times we run keygen unsuccessfully(the invalid http request is not counted as
	// the failure of keygen)
	FailedKeyGen uint64 `json:"failed_keygen"`
	// SucKeySign indicates how many times we run keySign successfully
	SucKeySign uint64 `json:"successful_keysign"`
	// FailedKeySign indicates how many times we run keysign unsuccessfully(the invalid http request is not counted as
	// the failure of keysign)
	FailedKeySign uint64 `json:"failed_keysign"`
}
