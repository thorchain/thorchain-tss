module gitlab.com/thorchain/tss/go-tss

go 1.14

require (
	github.com/binance-chain/tss-lib v0.0.0-20201118045712-70b2cb4bf916
	github.com/blang/semver v3.5.1+incompatible
	github.com/btcsuite/btcd v0.21.0-beta
	github.com/cosmos/cosmos-sdk v0.41.0
	github.com/davidlazar/go-crypto v0.0.0-20200604182044-b73af7476f6c // indirect
	github.com/deckarep/golang-set v1.7.1
	github.com/decred/dcrd/dcrec/secp256k1 v1.0.3
	github.com/golang/protobuf v1.4.3
	github.com/gorilla/mux v1.8.0
	github.com/ipfs/go-log v1.0.4
	github.com/libp2p/go-libp2p v0.11.0
	github.com/libp2p/go-libp2p-core v0.6.1
	github.com/libp2p/go-libp2p-discovery v0.5.0
	github.com/libp2p/go-libp2p-kad-dht v0.10.0
	github.com/libp2p/go-libp2p-peerstore v0.2.6
	github.com/libp2p/go-libp2p-testing v0.2.0
	github.com/libp2p/go-mplex v0.1.3 // indirect
	github.com/libp2p/go-sockaddr v0.1.0 // indirect
	github.com/libp2p/go-yamux v1.3.8 // indirect
	github.com/magiconair/properties v1.8.4
	github.com/multiformats/go-multiaddr v0.3.1
	github.com/olekukonko/tablewriter v0.0.0-20170122224234-a0225b3f23b5
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.8.0
	github.com/prometheus/client_model v0.2.0
	github.com/rs/zerolog v1.20.0
	github.com/stretchr/testify v1.7.0
	github.com/tendermint/btcd v0.1.1
	github.com/tendermint/tendermint v0.34.3
	gitlab.com/thorchain/binance-sdk v1.2.3-0.20210102180356-7b2f8924ed23
	go.uber.org/atomic v1.6.0
	golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad
	golang.org/x/text v0.3.3
	google.golang.org/protobuf v1.25.0
	gopkg.in/check.v1 v1.0.0-20200902074654-038fdea0a05b
)

replace (
	github.com/binance-chain/tss-lib => gitlab.com/thorchain/tss/tss-lib v0.0.0-20201118045712-70b2cb4bf916
	github.com/gogo/protobuf => github.com/regen-network/protobuf v1.3.2-alpha.regen.4
)
