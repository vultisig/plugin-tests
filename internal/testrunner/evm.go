package testrunner

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

type EVMFixture struct {
	TxB64        string
	MsgB64       string
	MsgSHA256B64 string
}

type dynamicFeeTxWithoutSignature struct {
	ChainID    *big.Int
	Nonce      uint64
	GasTipCap  *big.Int
	GasFeeCap  *big.Int
	Gas        uint64
	To         *common.Address `rlp:"nil"`
	Value      *big.Int
	Data       []byte
	AccessList ethtypes.AccessList
}

func GenerateEVMFixture(chainID int64, to string, valueWei string, gas, nonce uint64) (*EVMFixture, error) {
	chainIDBig := big.NewInt(chainID)
	toAddr := common.HexToAddress(to)

	value := new(big.Int)
	if valueWei != "" {
		_, ok := value.SetString(valueWei, 10)
		if !ok {
			return nil, fmt.Errorf("invalid value: %q is not a valid base-10 integer", valueWei)
		}
	} else {
		value.SetInt64(1000000000000000)
	}

	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainIDBig,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Gas:       gas,
		To:        &toAddr,
		Value:     value,
		Data:      nil,
	})

	unsignedTx := dynamicFeeTxWithoutSignature{
		ChainID:    chainIDBig,
		Nonce:      nonce,
		GasTipCap:  big.NewInt(1000000000),
		GasFeeCap:  big.NewInt(20000000000),
		Gas:        gas,
		To:         &toAddr,
		Value:      value,
		Data:       nil,
		AccessList: ethtypes.AccessList{},
	}

	txBytes, err := rlp.EncodeToBytes(unsignedTx)
	if err != nil {
		return nil, fmt.Errorf("failed to RLP encode: %w", err)
	}

	typedTxBytes := append([]byte{byte(ethtypes.DynamicFeeTxType)}, txBytes...)
	txB64 := base64.StdEncoding.EncodeToString(typedTxBytes)

	signer := ethtypes.LatestSignerForChainID(chainIDBig)
	hash := signer.Hash(tx)
	msgBytes := hash.Bytes()
	msgB64 := base64.StdEncoding.EncodeToString(msgBytes)

	msgSha256 := sha256.Sum256(msgBytes)
	msgSha256B64 := base64.StdEncoding.EncodeToString(msgSha256[:])

	return &EVMFixture{
		TxB64:        txB64,
		MsgB64:       msgB64,
		MsgSHA256B64: msgSha256B64,
	}, nil
}
