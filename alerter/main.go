package main

import (
	"github.com/certusone/chain_exporter/types"
	"github.com/getsentry/raven-go"
	"github.com/go-pg/pg"
	"github.com/pkg/errors"
	"os"
	"strconv"
	"time"
)

type (
	Monitor struct {
		db *pg.DB
	}
)

func main() {
	if os.Getenv("DB_HOST") == "" {
		panic(errors.New("DB_HOST needs to be set"))
	}
	if os.Getenv("DB_USER") == "" {
		panic(errors.New("DB_USER needs to be set"))
	}
	if os.Getenv("DB_PW") == "" {
		panic(errors.New("DB_PW needs to be set"))
	}
	if os.Getenv("RAVEN_DSN") == "" {
		panic(errors.New("RAVEN_DSN needs to be set"))
	}

	raven.SetDSN(os.Getenv("RAVEN_DSN"))

	db := pg.Connect(&pg.Options{
		Addr:     os.Getenv("DB_HOST"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PW"),
	})
	defer db.Close()

	monitor := &Monitor{db}
	for {
		select {
		case <-time.Tick(time.Second):
			err := monitor.sync()
			if err != nil {
				panic(err)
			}
		}
	}
}

func (m *Monitor) sync() error {
	// Alert on block misses
	var misses []*types.MissInfo
	err := m.db.Model(&types.MissInfo{}).Where("alerted = FALSE").Select(&misses)
	if err != nil {
		return err
	}
	for _, miss := range misses {
		raven.CaptureError(errors.New("Missed block"), map[string]string{"height": strconv.FormatInt(miss.Height, 10), "time": miss.Time.String(), "address": miss.Address})
		miss.Alerted = true
		_, err = m.db.Model(miss).Where("id = ?", miss.ID).Update()
		if err != nil {
			return err
		}
	}

	// Alert on proposals
	var proposals []*types.Proposal
	err = m.db.Model(&types.Proposal{}).Where("alerted = FALSE").Select(&proposals)
	if err != nil {
		return err
	}
	for _, proposal := range proposals {
		if proposal.ProposalStatus == "Passed" || proposal.ProposalStatus == "Rejected" {
			proposal.Alerted = true
			_, err = m.db.Model(proposal).Where("id = ?", proposal.ID).Update()
			if err != nil {
				return err
			}
			continue
		}

		raven.CaptureMessage("New governance proposal: "+proposal.Title+"\nDescription: "+proposal.Description+"\nStartHeight: "+proposal.VotingStartBlock, map[string]string{"height": strconv.FormatInt(proposal.Height, 10), "type": proposal.Type})

		proposal.Alerted = true
		_, err = m.db.Model(proposal).Where("id = ?", proposal.ID).Update()
		if err != nil {
			return err
		}
	}

	return nil
}
