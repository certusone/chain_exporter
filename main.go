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
	"os/signal"
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

	// Setup the database and ignore errors if the schema already exists
	err := CreateSchema(db)
	if err != nil {
		panic(err)
	}

	// Configure resty
	resty.SetTimeout(5 * time.Second)

	// Setup the monitor
	monitor := &Monitor{tClient, db}

	// Start the syncing task
	go func() {
		for {
			fmt.Println("start - sync blockchain")
			err := monitor.Sync()
			if err != nil {
				fmt.Printf("error - sync blockchain: %v\n", err)
			}
			fmt.Println("finish - sync blockchain")
			time.Sleep(time.Second)
		}
	}()

	// Allow graceful closing of the governance loop
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt)

	for {
		select {
		case <-time.Tick(10 * time.Second):
			fmt.Println("start - sync governance proposals")
			err := monitor.GetGovernance()
			if err != nil {
				fmt.Printf("error - sync governance proposals: %v\n", err)
				continue
			}
			fmt.Println("finish - sync governance proposals")
		case <-signalCh:
			return
		}
	}
}

// CreateSchema sets up the database using the ORM
func CreateSchema(db *pg.DB) error {
	for _, model := range []interface{}{(*ctypes.BlockInfo)(nil), (*ctypes.EvidenceInfo)(nil), (*ctypes.MissInfo)(nil), (*ctypes.Proposal)(nil)} {
		err := db.CreateTable(model, &orm.CreateTableOptions{IfNotExists: true})
		if err != nil {
			return err
		}
	}
	return nil
}

// Sync syncs the blockchain and missed blocks from a node
func (m *Monitor) Sync() error {

	// Check current height in db
	var blocks []ctypes.BlockInfo
	err := m.db.Model(&blocks).Order("height DESC").Limit(1).Select()
	if err != nil {
		return err
	}
	bestHeight := int64(1)
	if len(blocks) > 0 {
		bestHeight = blocks[0].Height
	}

	// Query the node for its height
	status, err := m.client.Status()
	if err != nil {
		return err
	}
	maxHeight := status.SyncInfo.LatestBlockHeight

	// Ingest all blocks up to the best height
	for i := bestHeight + 1; i <= maxHeight; i++ {
		err = m.IngestPrevBlock(i)
		if err != nil {
			return err
		}
		fmt.Printf("synced block %d/%d \n", i, maxHeight)
	}
	return nil
}

// IngestPrevBlock queries the block at the given height-1 from the node and ingests its metadata (blockinfo,evidence)
// into the database. It also queries the next block to access the commits and stores the missed signatures.
func (m *Monitor) IngestPrevBlock(height int64) error {
	prevHeight := height - 1

	// Get validator set for the block
	validators, err := m.client.Validators(&prevHeight)
	if err != nil {
		return err
	}

	// Query the previous block
	block, err := m.client.Block(&prevHeight)
	if err != nil {
		return err
	}

	// Query the next block to access the commits
	nextBlock, err := m.client.Block(&height)
	if err != nil {
		return err
	}

	// Parse blockinfo
	blockInfo := new(ctypes.BlockInfo)
	blockInfo.ID = block.BlockMeta.BlockID.String()
	blockInfo.Height = height
	blockInfo.Time = block.BlockMeta.Header.Time
	blockInfo.Proposer = block.Block.ProposerAddress.String()

	// Identify missed validators
	missedValidators := make([]*ctypes.MissInfo, 0)

	for i, validator := range validators.Validators {
		if nextBlock.Block.LastCommit.Precommits[i] == nil {
			missed := &ctypes.MissInfo{
				Height:   block.BlockMeta.Header.Height,
				Address:  validator.Address.String(),
				Alerted:  false,
				Time:     block.BlockMeta.Header.Time,
				Proposer: block.BlockMeta.Header.ProposerAddress.String(),
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
		// Insert blockinfo
		err = tx.Insert(blockInfo)
		if err != nil {
			return err
		}

		// Insert evidence
		if len(evidenceInfo) > 0 {
			err = tx.Insert(&evidenceInfo)
			if err != nil {
				return err
			}
		}

		// Insert missed signatures
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

// GetGovernance queries the governance proposals from the lcd and stores them in the db
func (m *Monitor) GetGovernance() error {
	// Query lcd
	resp, err := resty.R().Get(os.Getenv("LCD_URL") + "/gov/proposals")
	if err != nil {
		return err
	}

	// Parse proposals
	var proposals []*ctypes.Proposal
	err = json.Unmarshal(resp.Body(), &proposals)
	if err != nil {
		return err
	}

	// Copy proposal data into the database model
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

	// Store proposals in the db
	_, err = m.db.Model(&proposals).OnConflict("(id) DO UPDATE").Set("proposal_status = EXCLUDED.proposal_status").Insert()
	return err
}
