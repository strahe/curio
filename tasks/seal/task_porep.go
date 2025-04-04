package seal

import (
	"bytes"
	"context"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/chain/types"
)

type PoRepAPI interface {
	ChainHead(context.Context) (*types.TipSet, error)
	StateGetRandomnessFromBeacon(context.Context, crypto.DomainSeparationTag, abi.ChainEpoch, []byte, types.TipSetKey) (abi.Randomness, error)
}

type PoRepTask struct {
	db          *harmonydb.DB
	api         PoRepAPI
	sp          *SealPoller
	sc          *ffi.SealCalls
	paramsReady func() (bool, error)

	max int
}

func NewPoRepTask(db *harmonydb.DB, api PoRepAPI, sp *SealPoller, sc *ffi.SealCalls, paramck func() (bool, error), maxPoRep int) *PoRepTask {
	return &PoRepTask{
		db:          db,
		api:         api,
		sp:          sp,
		sc:          sc,
		paramsReady: paramck,
		max:         maxPoRep,
	}
}

func (p *PoRepTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sectorParamsArr []struct {
		SpID         int64                   `db:"sp_id"`
		SectorNumber int64                   `db:"sector_number"`
		RegSealProof abi.RegisteredSealProof `db:"reg_seal_proof"`
		TicketEpoch  abi.ChainEpoch          `db:"ticket_epoch"`
		TicketValue  []byte                  `db:"ticket_value"`
		SeedEpoch    abi.ChainEpoch          `db:"seed_epoch"`
		SealedCID    string                  `db:"tree_r_cid"`
		UnsealedCID  string                  `db:"tree_d_cid"`
	}

	err = p.db.Select(ctx, &sectorParamsArr, `
		SELECT sp_id, sector_number, reg_seal_proof, ticket_epoch, ticket_value, seed_epoch, tree_r_cid, tree_d_cid
		FROM sectors_sdr_pipeline
		WHERE task_id_porep = $1`, taskID)
	if err != nil {
		return false, err
	}
	if len(sectorParamsArr) != 1 {
		return false, xerrors.Errorf("expected 1 sector params, got %d", len(sectorParamsArr))
	}
	sectorParams := sectorParamsArr[0]

	sealed, err := cid.Parse(sectorParams.SealedCID)
	if err != nil {
		return false, xerrors.Errorf("failed to parse sealed cid: %w", err)
	}

	unsealed, err := cid.Parse(sectorParams.UnsealedCID)
	if err != nil {
		return false, xerrors.Errorf("failed to parse unsealed cid: %w", err)
	}

	ts, err := p.api.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("failed to get chain head: %w", err)
	}

	maddr, err := address.NewIDAddress(uint64(sectorParams.SpID))
	if err != nil {
		return false, xerrors.Errorf("failed to create miner address: %w", err)
	}

	buf := new(bytes.Buffer)
	if err := maddr.MarshalCBOR(buf); err != nil {
		return false, xerrors.Errorf("failed to marshal miner address: %w", err)
	}

	rand, err := p.api.StateGetRandomnessFromBeacon(ctx, crypto.DomainSeparationTag_InteractiveSealChallengeSeed, sectorParams.SeedEpoch, buf.Bytes(), ts.Key())
	if err != nil {
		return false, xerrors.Errorf("failed to get randomness for computing seal proof: %w", err)
	}

	sr := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sectorParams.SpID),
			Number: abi.SectorNumber(sectorParams.SectorNumber),
		},
		ProofType: sectorParams.RegSealProof,
	}

	// COMPUTE THE PROOF!

	proof, err := p.sc.PoRepSnark(ctx, sr, sealed, unsealed, sectorParams.TicketValue, abi.InteractiveSealRandomness(rand))
	if err != nil {
		//end, rerr := p.recoverErrors(ctx, sectorParams.SpID, sectorParams.SectorNumber, err)
		//if rerr != nil {
		//	return false, xerrors.Errorf("recover errors: %w", rerr)
		//}
		//if end {
		//	// done, but the error handling has stored a different than success state
		//	return true, nil
		//}

		return false, xerrors.Errorf("failed to compute seal proof: %w", err)
	}

	// store success!
	n, err := p.db.Exec(ctx, `UPDATE sectors_sdr_pipeline
		SET after_porep = TRUE, seed_value = $3, porep_proof = $4, task_id_porep = NULL
		WHERE sp_id = $1 AND sector_number = $2`,
		sectorParams.SpID, sectorParams.SectorNumber, []byte(rand), proof)
	if err != nil {
		return false, xerrors.Errorf("store sdr success: updating pipeline: %w", err)
	}
	if n != 1 {
		return false, xerrors.Errorf("store sdr success: updated %d rows", n)
	}

	return true, nil
}

func (p *PoRepTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	rdy, err := p.paramsReady()
	if err != nil {
		return nil, xerrors.Errorf("failed to setup params: %w", err)
	}
	if !rdy {
		log.Infow("PoRepTask.CanAccept() params not ready, not scheduling")
		return nil, nil
	}
	// todo sort by priority

	id := ids[0]
	return &id, nil
}

func (p *PoRepTask) TypeDetails() harmonytask.TaskTypeDetails {
	gpu := 1.0
	if IsDevnet {
		gpu = 0
	}
	res := harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(p.max),
		Name: "PoRep",
		Cost: resources.Resources{
			Cpu:       1,
			Gpu:       gpu,
			Ram:       50 << 30, // todo correct value
			MachineID: 0,
		},
		MaxFailures: 10,
		RetryWait: func(retries int) time.Duration {
			return min(time.Second<<retries, 2*time.Minute)
		},
		Follows: nil,
	}

	if IsDevnet {
		res.Cost.Ram = 1 << 30
	}

	return res
}

func (p *PoRepTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := p.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (p *PoRepTask) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id,sector_number FROM sectors_sdr_pipeline WHERE task_id_porep = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&PoRepTask{})

func (p *PoRepTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	p.sp.pollers[pollerPoRep].Set(taskFunc)
}

var _ harmonytask.TaskInterface = &PoRepTask{}
