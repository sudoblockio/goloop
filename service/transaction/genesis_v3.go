package transaction

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"

	"github.com/icon-project/goloop/service/contract"
	"github.com/icon-project/goloop/service/state"
	"github.com/icon-project/goloop/service/txresult"

	"github.com/icon-project/goloop/service/scoredb"

	"github.com/icon-project/goloop/common"
	"github.com/icon-project/goloop/common/crypto"
	"github.com/icon-project/goloop/common/errors"
	"github.com/icon-project/goloop/module"
)

type preInstalledScores struct {
	Owner       *common.Address  `json:"owner"`
	ContentType string           `json:"contentType"`
	ContentID   string           `json:"contentId"`
	Content     string           `json:"content"`
	Params      *json.RawMessage `json:"params"`
}
type accountInfo struct {
	Name    string              `json:"name"`
	Address common.Address      `json:"address"`
	Balance *common.HexInt      `json:"balance"`
	Score   *preInstalledScores `json:"score"`
}

type genesisV3JSON struct {
	Accounts []accountInfo    `json:"accounts"`
	Message  string           `json:"message"`
	Chain    json.RawMessage  `json:"chain"`
	NID      *common.HexInt64 `json:"nid"`
	raw      []byte
	txHash   []byte
}

func (g *genesisV3JSON) calcHash() ([]byte, error) {
	bs, err := SerializeJSON(g.raw, nil, nil)
	if err != nil {
		return nil, err
	}
	bs = append([]byte("genesis_tx."), bs...)
	return crypto.SHA3Sum256(bs), nil
}

func (g *genesisV3JSON) updateTxHash() error {
	if g.txHash == nil {
		h, err := g.calcHash()
		if err != nil {
			return err
		}
		g.txHash = h
	}
	return nil
}

type genesisV3 struct {
	*genesisV3JSON
	hash []byte
	nid  int
}

func (g *genesisV3) From() module.Address {
	return nil
}

func (g *genesisV3) Version() int {
	return module.TransactionVersion3
}

func (g *genesisV3) Bytes() []byte {
	return g.genesisV3JSON.raw
}

func (g *genesisV3) Group() module.TransactionGroup {
	return module.TransactionGroupNormal
}

func (g *genesisV3) Hash() []byte {
	if g.hash == nil {
		g.hash = crypto.SHA3Sum256(g.Bytes())
	}
	return g.hash
}

func (g *genesisV3) ID() []byte {
	g.updateTxHash()
	return g.txHash
}

func (g *genesisV3) ToJSON(version int) (interface{}, error) {
	var jso map[string]interface{}
	if err := json.Unmarshal(g.raw, &jso); err != nil {
		return nil, err
	}
	return jso, nil
}

func (g *genesisV3) Verify(ts int64) error {
	acs := map[string]*accountInfo{}
	for _, ac := range g.genesisV3JSON.Accounts {
		acs[ac.Name] = &ac
	}
	if _, ok := acs["treasury"]; !ok {
		return InvalidGenesisError.New("NoTreasuryAccount")
	}
	if _, ok := acs["god"]; !ok {
		return InvalidGenesisError.New("NoGodAccount")
	}
	return nil
}

func (g *genesisV3) PreValidate(wc state.WorldContext, update bool) error {
	if wc.BlockHeight() != 0 {
		return errors.ErrInvalidState
	}
	return nil
}

func (g *genesisV3) GetHandler(contract.ContractManager) (TransactionHandler, error) {
	return g, nil
}

func NIDForGenesisID(txid []byte) int {
	return int(txid[2]) | int(txid[1])<<8 | int(txid[0])<<16
}

func (g *genesisV3) NID() int {
	if g.nid == 0 {
		if g.genesisV3JSON.NID == nil {
			g.nid = NIDForGenesisID(g.ID())
		} else {
			g.nid = int(g.genesisV3JSON.NID.Value)
		}
	}
	return g.nid
}

func (g *genesisV3) ValidateNetwork(nid int) bool {
	return g.NID() == nid
}

func (g *genesisV3) Prepare(ctx contract.Context) (state.WorldContext, error) {
	lq := []state.LockRequest{
		{state.WorldIDStr, state.AccountWriteLock},
	}
	return ctx.GetFuture(lq), nil
}

func (g *genesisV3) Execute(ctx contract.Context) (txresult.Receipt, error) {
	r := txresult.NewReceipt(common.NewContractAddress(state.SystemID))
	as := ctx.GetAccountState(state.SystemID)

	var totalSupply big.Int
	for i := range g.Accounts {
		info := g.Accounts[i]
		if info.Balance == nil {
			continue
		}
		addr := scoredb.NewVarDB(as, info.Name)
		addr.Set(&info.Address)
		ac := ctx.GetAccountState(info.Address.ID())
		ac.SetBalance(&info.Balance.Int)
		totalSupply.Add(&totalSupply, &info.Balance.Int)
	}

	nid := g.NID()
	nidVar := scoredb.NewVarDB(as, state.VarNetwork)
	nidVar.Set(nid)

	ts := scoredb.NewVarDB(as, state.VarTotalSupply)
	if err := ts.Set(&totalSupply); err != nil {
		ctx.Logger().Errorf("Fail to store total supply err=%+v\n", err)
		return nil, err
	}

	if err := g.deployPreInstall(ctx, r); err != nil {
		ctx.Logger().Errorf("Fail to install scores err=%+v\n", err)
		return nil, err
	}
	r.SetResult(module.StatusSuccess, big.NewInt(0), big.NewInt(0), nil)
	return r, nil
}

const (
	contentIdHash = "hash:"
	contentIdCid  = "cid:"
)

func (g *genesisV3) deployChainScore(ctx contract.Context, receipt txresult.Receipt) error {
	sas := ctx.GetAccountState(state.SystemID)
	sas.InitContractAccount(nil)
	sas.DeployContract(nil, "system", state.CTAppSystem,
		nil, nil)
	if err := sas.AcceptContract(nil, nil); err != nil {
		return err
	}
	chainScore, err := contract.GetSystemScore(contract.CID_CHAIN,
		common.NewContractAddress(state.SystemID), contract.NewCallContext(ctx, receipt, false), ctx.Logger())
	if err != nil {
		return err
	}
	if err := contract.CheckMethod(chainScore); err != nil {
		return err
	}
	sas.SetAPIInfo(chainScore.GetAPI())
	if err := chainScore.Install(g.Chain); err != nil {
		return err
	}
	return nil
}

func (g *genesisV3) deployPreInstall(ctx contract.Context, receipt txresult.Receipt) error {
	if err := g.deployChainScore(ctx, receipt); err != nil {
		return InvalidGenesisError.Wrapf(err, "FAIL to deploy ChainScore err=%+v\n", err)
	}
	for _, acc := range g.Accounts {
		if acc.Score == nil {
			continue
		}
		score := acc.Score
		cc := contract.NewCallContext(ctx, receipt, false)
		if score.Content != "" {
			if strings.HasPrefix(score.Content, "0x") {
				score.Content = strings.TrimPrefix(score.Content, "0x")
			}
			data, _ := hex.DecodeString(score.Content)
			handler := contract.NewDeployHandlerForPreInstall(score.Owner,
				&acc.Address, score.ContentType, data, score.Params, ctx.Logger())
			status, _, _, _ := cc.Call(handler)
			if status != module.StatusSuccess {
				return InvalidGenesisError.Errorf("FAIL to install pre-installed score."+
					"status : %d, addr : %v\n", status, acc.Address)
			}
			cc.Dispose()
		} else if score.ContentID != "" {
			if strings.HasPrefix(score.ContentID, contentIdHash) == true {
				contentHash := strings.TrimPrefix(score.ContentID, contentIdHash)
				content, err := ctx.GetPreInstalledScore(contentHash)
				if err != nil {
					return InvalidGenesisError.Wrapf(err,
						"Fail to get PreInstalledScore for ID=%s", contentHash)
				}
				handler := contract.NewDeployHandlerForPreInstall(score.Owner,
					&acc.Address, score.ContentType, content, score.Params, ctx.Logger())
				status, _, _, _ := cc.Call(handler)
				if status != module.StatusSuccess {
					return InvalidGenesisError.Errorf("FAIL to install pre-installed score."+
						"status : %d, addr : %v\n", status, acc.Address)
				}
				cc.Dispose()
			} else if strings.HasPrefix(score.ContentID, contentIdCid) == true {
				// TODO implement for contentCid
				return errors.UnsupportedError.New("CID prefix is't Unsupported")
			} else {
				return InvalidGenesisError.Errorf("SCORE<%s> Invalid contentId=%q", &acc.Address, score.ContentID)
			}
		} else {
			return InvalidGenesisError.Errorf("There is no content for score %s", &acc.Address)
		}
	}
	return nil
}

func (g *genesisV3) Dispose() {
}

func (g *genesisV3) Query(wc state.WorldContext) (module.Status, interface{}) {
	return module.StatusSuccess, nil
}

func (g *genesisV3) Timestamp() int64 {
	return 0
}

func (g *genesisV3) MarshalJSON() ([]byte, error) {
	return g.raw, nil
}

func (g *genesisV3) Nonce() *big.Int {
	return nil
}

func newGenesisV3(js []byte) (Transaction, error) {
	genjs := new(genesisV3JSON)
	if err := json.Unmarshal(js, genjs); err != nil {
		return nil, errors.IllegalArgumentError.Wrapf(err, "Invalid json for genesis(%s)", string(js))
	}
	if len(genjs.Accounts) != 0 {
		genjs.raw = js
		tx := &genesisV3{genesisV3JSON: genjs}
		if err := tx.updateTxHash(); err != nil {
			return nil, InvalidGenesisError.Wrap(err, "FailToMakeTxHash")
		}
		return tx, nil
	}
	return nil, errors.IllegalArgumentError.New("NoAccounts")
}
