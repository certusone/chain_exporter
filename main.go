package main

import (
	"encoding/json"
	"fmt"
	ctypes "github.com/certusone/chain_exporter/types"
	"github.com/go-pg/pg"
	"github.com/go-pg/pg/orm"
	"github.com/pkg/errors"
	"github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/types"
	"gopkg.in/resty.v1"
	"os"
	"time"
)

type (
	Monitor struct {
		client *client.HTTP
		db     *pg.DB
	}
)

func main() {
	if os.Getenv("GAIA_URL") == "" {
		panic(errors.New("GAIA_URL needs to be set"))
	}
	if os.Getenv("DB_HOST") == "" {
		panic(errors.New("DB_HOST needs to be set"))
	}
	if os.Getenv("DB_USER") == "" {
		panic(errors.New("DB_USER needs to be set"))
	}
	if os.Getenv("DB_PW") == "" {
		panic(errors.New("DB_PW needs to be set"))
	}
	if os.Getenv("LCD_URL") == "" {
		panic(errors.New("LCD_URL needs to be set"))
	}

	tClient := client.NewHTTP(os.Getenv("GAIA_URL"), "/websocket")

	db := pg.Connect(&pg.Options{
		Addr:     os.Getenv("DB_HOST"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PW"),
	})
	defer db.Close()

	err := createSchema(db)
	if err != nil {
		//panic(err)
	}
	monitor := &Monitor{tClient, db}
	go func() {
		for {
			err = monitor.sync()
			if err != nil {
				fmt.Printf("error syncing: %v\n", err)
			}
			time.Sleep(time.Second)
		}
	}()

	for {
		select {
		case <-time.Tick(10 * time.Second):
			err := monitor.getGovernance()
			if err != nil {
				fmt.Printf("error parsing governance: %v\n", err)
			}
		}
	}
}

func createSchema(db *pg.DB) error {
	for _, model := range []interface{}{(*ctypes.BlockInfo)(nil), (*ctypes.EvidenceInfo)(nil), (*ctypes.MissInfo)(nil), (*ctypes.Proposal)(nil)} {
		err := db.CreateTable(model, &orm.CreateTableOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Monitor) sync() error {
	var blocks []ctypes.BlockInfo
	err := m.db.Model(&blocks).Order("height DESC").Limit(1).Select()
	if err != nil {
		return err
	}
	bestHeight := int64(1)
	if len(blocks) > 0 {
		bestHeight = blocks[0].Height
	}

	status, err := m.client.Status()
	if err != nil {
		return err
	}
	maxHeight := status.SyncInfo.LatestBlockHeight

	for i := bestHeight + 1; i <= maxHeight; i++ {
		err = m.ingestBlock(i)
		if err != nil {
			return err
		}
		fmt.Printf("synced block %d/%d \n", i, maxHeight)
	}
	return nil
}

func (m *Monitor) ingestBlock(height int64) error {
	prevHeight := height - 1

	// Get Data
	validators, err := m.client.Validators(&prevHeight)
	if err != nil {
		return err
	}

	block, err := m.client.Block(&prevHeight)
	if err != nil {
		return err
	}

	nextBlock, err := m.client.Block(&height)
	if err != nil {
		return err
	}

	blockInfo := new(ctypes.BlockInfo)
	blockInfo.ID = nextBlock.BlockMeta.Header.LastBlockID.String()
	blockInfo.Height = height
	blockInfo.Time = nextBlock.BlockMeta.Header.Time
	blockInfo.Proposer = block.Block.ProposerAddress.String()

	// Identify missed validators
	missedValidators := make([]*ctypes.MissInfo, 0)

	// Parse
	for i, validator := range validators.Validators {
		if nextBlock.Block.LastCommit.Precommits[i] == nil {
			missed := &ctypes.MissInfo{
				Height:  block.BlockMeta.Header.Height,
				Address: validator.Address.String(),
				Alerted: false,
				Time:    block.BlockMeta.Header.Time,
			}
			missedValidators = append(missedValidators, missed)
			continue
		}
	}

	// Collect evidence
	evidenceInfo := make([]*ctypes.EvidenceInfo, 0)
	for _, evidence := range nextBlock.Block.Evidence.Evidence {
		evInfo := &ctypes.EvidenceInfo{}
		evInfo.Address = types.Address(evidence.Address()).String()
		evInfo.Height = evidence.Height()
		evidenceInfo = append(evidenceInfo, evInfo)
	}

	// Insert in DB
	err = m.db.RunInTransaction(func(tx *pg.Tx) error {
		err = tx.Insert(blockInfo)
		if err != nil {
			return err
		}
		if len(evidenceInfo) > 0 {
			err = tx.Insert(&evidenceInfo)
			if err != nil {
				return err
			}
		}
		if len(missedValidators) > 0 {
			err = tx.Insert(&missedValidators)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

func (m *Monitor) getGovernance() error {
	resp, err := resty.R().Get(os.Getenv("LCD_URL") + "/gov/proposals")
	if err != nil {
		return err
	}

	var proposals []*ctypes.Proposal
	err = json.Unmarshal(resp.Body(), &proposals)
	if err != nil {
		return err
	}

	for _, proposal := range proposals {
		proposal.ID = proposal.Details.ProposalID
		proposal.Height = proposal.Details.SubmitBlock
		proposal.Alerted = false
		proposal.Description = proposal.Details.Description
		proposal.ProposalStatus = proposal.Details.ProposalStatus
		proposal.ProposalType = proposal.Details.ProposalType
		proposal.Title = proposal.Details.Title
		proposal.VotingStartBlock = proposal.Details.VotingStartBlock
	}

	_, err = m.db.Model(&proposals).OnConflict("DO NOTHING").Insert()
	return err
}
