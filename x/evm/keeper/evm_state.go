// Copyright (c) 2023-2024 Nibi, Inc.
package keeper

import (
	"fmt"
	"slices"

	"github.com/NibiruChain/collections"
	"github.com/cosmos/cosmos-sdk/codec"
	sdkstore "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/NibiruChain/nibiru/eth"
	"github.com/NibiruChain/nibiru/x/evm"
)

type (
	AccStatePrimaryKey = collections.Pair[gethcommon.Address, gethcommon.Hash]
	CodeHash           = []byte
)

// EvmState isolates the key-value stores (collections) for the x/evm module.
type EvmState struct {
	ModuleParams collections.Item[evm.Params]

	// ContractBytecode: Map from (byte)code hash -> contract bytecode
	ContractBytecode collections.Map[CodeHash, []byte]

	// AccState: Map from eth address (account) and hash of a state key -> smart
	// contract state. Each contract essentially has its own key-value store.
	//
	//  - primary key (PK): (EthAddr+EthHash). The contract is the primary key
	//  because there's exactly one deployer and withdrawer.
	//  - value (V): State value bytes.
	AccState collections.Map[
		AccStatePrimaryKey, // account (EthAddr) + state key (EthHash)
		[]byte,
	]

	// BlockGasUsed: Gas used by Ethereum txs in the block (transient).
	BlockGasUsed collections.ItemTransient[uint64]
	// BlockLogSize: EVM tx log size for the block (transient).
	BlockLogSize collections.ItemTransient[uint64]
	// BlockTxIndex: EVM tx index for the block (transient).
	BlockTxIndex collections.ItemTransient[uint64]
	// BlockBloom: Bloom filters.
	BlockBloom collections.ItemTransient[[]byte]
}

func (k *Keeper) EVMState() EvmState { return k.EvmState }

func NewEvmState(
	cdc codec.BinaryCodec,
	storeKey sdkstore.StoreKey,
	storeKeyTransient sdkstore.StoreKey,
) EvmState {
	return EvmState{
		ModuleParams: collections.NewItem(
			storeKey, evm.KeyPrefixParams,
			collections.ProtoValueEncoder[evm.Params](cdc),
		),
		ContractBytecode: collections.NewMap(
			storeKey, evm.KeyPrefixAccCodes,
			eth.KeyEncoderBytes,
			eth.ValueEncoderBytes,
		),
		AccState: collections.NewMap(
			storeKey, evm.KeyPrefixAccState,
			collections.PairKeyEncoder(eth.KeyEncoderEthAddr, eth.KeyEncoderEthHash),
			eth.ValueEncoderBytes,
		),
		BlockGasUsed: collections.NewItemTransient(
			storeKeyTransient,
			evm.NamespaceBlockGasUsed,
			collections.Uint64ValueEncoder,
		),
		BlockLogSize: collections.NewItemTransient(
			storeKeyTransient,
			evm.NamespaceBlockLogSize,
			collections.Uint64ValueEncoder,
		),
		BlockBloom: collections.NewItemTransient(
			storeKeyTransient,
			evm.NamespaceBlockBloom,
			eth.ValueEncoderBytes,
		),
		BlockTxIndex: collections.NewItemTransient(
			storeKeyTransient,
			evm.NamespaceBlockTxIndex,
			collections.Uint64ValueEncoder,
		),
	}
}

// BytesToHex converts a byte array to a hexadecimal string
func BytesToHex(bz []byte) string {
	return fmt.Sprintf("%x", bz)
}

func (state EvmState) SetAccCode(ctx sdk.Context, codeHash, code []byte) {
	if len(code) > 0 {
		state.ContractBytecode.Insert(ctx, codeHash, code)
	} else {
		// Ignore collections "key not found" error because erasing an empty
		// state is perfectly valid here.
		_ = state.ContractBytecode.Delete(ctx, codeHash)
	}
}

func (state EvmState) GetContractBytecode(
	ctx sdk.Context, codeHash []byte,
) (code []byte) {
	return state.ContractBytecode.GetOr(ctx, codeHash, []byte{})
}

// GetParams returns the total set of evm parameters.
func (k Keeper) GetParams(ctx sdk.Context) (params evm.Params) {
	params, _ = k.EvmState.ModuleParams.Get(ctx)
	return params
}

// SetParams: Setter for the module parameters.
func (k Keeper) SetParams(ctx sdk.Context, params evm.Params) {
	slices.Sort(params.ActivePrecompiles)
	k.EvmState.ModuleParams.Set(ctx, params)
}

// SetState update contract storage, delete if value is empty.
func (state EvmState) SetAccState(
	ctx sdk.Context, addr eth.EthAddr, stateKey eth.EthHash, stateValue []byte,
) {
	if len(stateValue) != 0 {
		state.AccState.Insert(ctx, collections.Join(addr, stateKey), stateValue)
	} else {
		_ = state.AccState.Delete(ctx, collections.Join(addr, stateKey))
	}
}

// GetState: Implements `statedb.Keeper` interface: Loads smart contract state.
func (k *Keeper) GetState(ctx sdk.Context, addr eth.EthAddr, stateKey eth.EthHash) eth.EthHash {
	return gethcommon.BytesToHash(k.EvmState.AccState.GetOr(
		ctx,
		collections.Join(addr, stateKey),
		[]byte{},
	))
}
