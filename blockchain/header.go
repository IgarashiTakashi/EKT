package blockchain

import (
	"encoding/hex"
	"encoding/json"

	"github.com/EducationEKT/EKT/MPTPlus"
	"github.com/EducationEKT/EKT/core/types"
	"github.com/EducationEKT/EKT/core/userevent"
	"github.com/EducationEKT/EKT/crypto"
	"github.com/EducationEKT/EKT/db"
	"github.com/EducationEKT/EKT/log"
)

const (
	HEADER_VERSION_PURE_MTP = 0
	HEADER_VERSION_MIXED    = 1
)

type Header struct {
	Height       int64          `json:"height"`
	Timestamp    int64          `json:"timestamp"`
	TotalFee     int64          `json:"totalFee"`
	PreviousHash types.HexBytes `json:"previousHash"`
	Coinbase     types.HexBytes `json:"miner"`
	StatTree     *MPTPlus.MTP   `json:"statRoot"`
	TokenTree    *MPTPlus.MTP   `json:"tokenRoot"`
	TxHash       types.HexBytes `json:"txHash"`
	ReceiptHash  types.HexBytes `json:"receiptHash"`
	Version      int            `json:"version"`
}

func (header *Header) Bytes() []byte {
	data, _ := json.Marshal(header)
	return data
}

func (header *Header) CaculateHash() []byte {
	return crypto.Sha3_256(header.Bytes())
}

func (header Header) GetAccount(address []byte) (*types.Account, error) {
	value, err := header.StatTree.GetValue(address)
	if err != nil {
		return nil, err
	}
	var account types.Account
	err = json.Unmarshal(value, &account)
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (header Header) ExistAddress(address []byte) bool {
	return header.StatTree.ContainsKey(address)
}

func GenesisHeader(accounts []types.Account) *Header {
	header := &Header{
		Height:       0,
		TotalFee:     0,
		PreviousHash: nil,
		Timestamp:    0,
		StatTree:     MPTPlus.NewMTP(db.GetDBInst()),
		TokenTree:    MPTPlus.NewMTP(db.GetDBInst()),
		Version:      HEADER_VERSION_MIXED,
	}

	for _, account := range accounts {
		header.StatTree.MustInsert(account.Address, account.ToBytes())
	}

	return header
}

func NewHeader(last Header, packTime int64, parentHash types.HexBytes, coinbase types.HexBytes) *Header {
	header := &Header{
		Height:       last.Height + 1,
		Timestamp:    packTime,
		TotalFee:     0,
		PreviousHash: parentHash,
		Coinbase:     coinbase,
		StatTree:     MPTPlus.MTP_Tree(db.GetDBInst(), last.StatTree.Root),
		TokenTree:    MPTPlus.MTP_Tree(db.GetDBInst(), last.TokenTree.Root),
		Version:      HEADER_VERSION_MIXED,
	}

	return header
}

func (header *Header) NewSubTransaction(txs userevent.SubTransactions) bool {
	m := make(map[string]*types.Account)
	for _, tx := range txs {
		from, exist1 := m[hex.EncodeToString(tx.From[:32])]
		to, exist2 := m[hex.EncodeToString(tx.To[:32])]
		if !exist1 {
			from, _ = header.GetAccount(tx.From[:32])
		}
		if !exist2 {
			_to, err := header.GetAccount(tx.To[:32])
			if err != nil {
				if len(tx.To) == 64 {
					return false
				} else {
					_to = types.NewAccount(tx.To)
				}
			}
			if len(tx.To) == 64 {
				if _to.Contracts == nil {
					return false
				}
				if _, exist := _to.Contracts[hex.EncodeToString(tx.To[32:])]; !exist {
					return false
				}
			}
			to = _to
		}
		if !header.HandleTx(from, to, tx) {
			return false
		} else {
			m[hex.EncodeToString(tx.From[:32])] = from
			m[hex.EncodeToString(tx.To[:32])] = to
		}
	}
	for _, account := range m {
		header.StatTree.MustInsert(account.Address, account.ToBytes())
	}
	return true
}

func (header *Header) HandleTx(from, to *types.Account, tx userevent.SubTransaction) bool {
	if !header.CheckSubTx(from, to, tx) {
		return false
	} else {
		return header.Transfer(from, to, tx)
	}
}

func (header *Header) Transfer(from, to *types.Account, tx userevent.SubTransaction) bool {
	if len(tx.From) == 32 {
		switch tx.TokenAddress {
		case types.EKTAddress:
			from.Amount -= tx.Amount
		case types.GasAddress:
			from.Gas -= tx.Amount
		default:
			from.Balances[tx.TokenAddress] -= tx.Amount
		}
	} else {
		contractAccount := from.Contracts[hex.EncodeToString(tx.From[32:])]
		switch tx.TokenAddress {
		case types.EKTAddress:
			contractAccount.Amount -= tx.Amount
		case types.GasAddress:
			contractAccount.Gas -= tx.Amount
		default:
			contractAccount.Balances[tx.TokenAddress] -= tx.Amount
		}
		from.Contracts[hex.EncodeToString(tx.From[32:])] = contractAccount
	}

	if len(tx.To) == 32 {
		switch tx.TokenAddress {
		case types.EKTAddress:
			to.Amount += tx.Amount
		case types.GasAddress:
			to.Gas += tx.Amount
		default:
			if to.Balances == nil {
				to.Balances = map[string]int64{}
				to.Balances[tx.TokenAddress] = 0
			}
			to.Balances[tx.TokenAddress] += tx.Amount
		}
	} else {
		contractAccount := to.Contracts[hex.EncodeToString(tx.To[32:])]
		switch tx.TokenAddress {
		case types.EKTAddress:
			contractAccount.Amount += tx.Amount
		case types.GasAddress:
			contractAccount.Gas += tx.Amount
		default:
			if contractAccount.Balances == nil {
				contractAccount.Balances = map[string]int64{}
				contractAccount.Balances[tx.TokenAddress] = 0
			}
			contractAccount.Balances[tx.TokenAddress] += tx.Amount
		}
		to.Contracts[hex.EncodeToString(tx.To[32:])] = contractAccount
	}
	return true
}

func (header *Header) CheckSubTx(from, to *types.Account, tx userevent.SubTransaction) bool {
	if len(tx.From) == 32 {
		switch tx.TokenAddress {
		case types.EKTAddress:
			return from.Amount > tx.Amount
		case types.GasAddress:
			return from.Gas > tx.Amount
		default:
			if from.Balances != nil && from.Balances[tx.TokenAddress] > tx.Amount {
				return true
			}
		}
	} else if from.Contracts == nil {
		return false
	} else {
		subAddr := tx.From[32:]
		contractAccount := from.Contracts[hex.EncodeToString(subAddr)]
		switch tx.TokenAddress {
		case types.EKTAddress:
			return contractAccount.Amount > tx.Amount
		case types.GasAddress:
			return contractAccount.Gas > tx.Amount
		default:
			if contractAccount.Balances != nil && contractAccount.Balances[tx.TokenAddress] > tx.Amount {
				return true
			}
		}
	}
	return false
}

func (header *Header) CheckFromAndBurnGas(tx userevent.Transaction) bool {
	if len(tx.From) != 32 {
		return false
	}
	if len(tx.To) != 32 && len(tx.To) != 64 {
		return false
	}
	account, err := header.GetAccount(tx.GetFrom())
	if err != nil || account == nil || account.Gas < tx.Fee || account.GetNonce()+1 != tx.GetNonce() {
		return false
	}
	switch tx.TokenAddress {
	case types.EKTAddress:
		if account.Amount < tx.Amount {
			return false
		}
	case types.GasAddress:
		if account.Gas < tx.Amount+tx.Fee {
			return false
		}
	default:
		if account.Balances == nil || account.Balances[tx.TokenAddress] < tx.Amount {
			return false
		}
	}
	account.BurnGas(tx.Fee)
	header.StatTree.MustInsert(account.Address, account.ToBytes())
	return true
}

func (header *Header) CheckTransfer(tx userevent.Transaction) bool {
	return header.CheckFromAndBurnGas(tx)
}

func FromBytes2Header(data []byte) *Header {
	var header Header
	err := json.Unmarshal(data, &header)
	if err != nil {
		return nil
	}
	return &header
}

func (header *Header) UpdateMiner() {
	account, err := header.GetAccount(header.Coinbase)
	if account == nil || err != nil {
		account = types.NewAccount(header.Coinbase)
	}
	account.Gas += header.TotalFee
	err = header.StatTree.MustInsert(header.Coinbase, account.ToBytes())
	if err != nil {
		log.Crit("Update miner failed, %s", err.Error())
	}
}

func Decimals(decimal int64) int64 {
	result := int64(1)
	for i := int64(0); i < decimal; i++ {
		result *= 10
	}
	return result
}
