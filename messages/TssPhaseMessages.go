package messages

type Algo int

const (
	KEYGEN1         = "KGRound1Message"
	KEYGEN2aUnicast = "KGRound2Message1"
	KEYGEN2b        = "KGRound2Message2"
	KEYGEN3         = "KGRound3Message"
	EDDSAKEYGEN1    = "EDDSAKGRound1Message"
	EDDSAKEYGEN2a   = "EDDSAKGRound2Message1"
	EDDSAKEYGEN2b   = "EDDSAKGRound2Message2"

	KEYSIGN1aUnicast      = "SignRound1Message1"
	KEYSIGN1b             = "SignRound1Message2"
	KEYSIGN2Unicast       = "SignRound2Message"
	KEYSIGN3              = "SignRound3Message"
	KEYSIGN4              = "SignRound4Message"
	KEYSIGN5              = "SignRound5Message"
	KEYSIGN6              = "SignRound6Message"
	KEYSIGN7              = "SignRound7Message"
	KEYSIGN8              = "SignRound8Message"
	KEYSIGN9              = "SignRound9Message"
	EDDSAKEYSIGN1         = "EDDSASignRound1Message"
	EDDSAKEYSIGN2         = "EDDSASignRound2Message"
	EDDSAKEYSIGN3         = "EDDSASignRound3Message"
	ECDSATSSKEYGENROUNDS  = 4
	ECDSATSSKEYSIGNROUNDS = 10

	EDDSATSSKEYGENROUNDS  = 3
	EDDSATSSKEYSIGNROUNDS = 3

	ECDSAKEYGEN Algo = iota
	ECDSAKEYSIGN
	EDDSAKEYGEN
	EDDSAKEYSIGN
)
