package main

import (
	"fmt"
	"github.com/certusone/chain_exporter/types"
	"github.com/getsentry/raven-go"
	"github.com/go-pg/pg"
	"github.com/pkg/errors"
	"os"
	"os/signal"
	"strconv"
	"time"
)

type (
	Monitor struct {
		db      *pg.DB
		address string
	}
)

func main() {
	if os.Getenv("DB_HOST") == "" {
		panic(errors.New("DB_HOST needs to be set"))
	}
	if os.Getenv("DB_NAME") == "" {
		panic(errors.New("DB_NAME needs to be set"))
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
	if os.Getenv("ADDRESS") == "" {
		panic(errors.New("ADDRESS needs to be set"))
	}

	// Set Raven URL for alerts
	raven.SetDSN(os.Getenv("RAVEN_DSN"))

	// Connect to the postgres datastore
	db := pg.Connect(&pg.Options{
		Addr:     os.Getenv("DB_HOST"),
		Database: os.Getenv("DB_NAME"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PW"),
	})
	defer db.Close()

	// Start the monitor
	monitor := &Monitor{db, os.Getenv("ADDRESS")}

	go func() {
		for {
			select {
			// Check for alert conditions every second
			case <-time.Tick(time.Second):
				fmt.Println("start - alerting on misses")
				err := monitor.AlertMisses()
				if err != nil {
					fmt.Printf("error - alerting on misses: %v\n", err)
				}
				fmt.Println("finish - alerting on misses")
			}
		}
	}()
	go func() {
		for {
			select {
			case <-time.Tick(time.Second):
				fmt.Println("start - alerting on governance")
				err := monitor.AlertGovernance()
				if err != nil {
					fmt.Printf("error - alerting on governance: %v\n", err)
				}
				fmt.Println("finish - alerting on governance")
			}
		}
	}()

	// Allow graceful closing of the process
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt)
	<-signalCh
}

// AlertMisses queries misses from the database and sends the relevant alert to sentry
func (m *Monitor) AlertMisses() error {
	// Query block misses from the DB
	var misses []*types.MissInfo
	err := m.db.Model(&types.MissInfo{}).Where("alerted = FALSE and address = ?", m.address).Select(&misses)
	if err != nil {
		return err
	}

	// Iterate misses and send alerts
	for _, miss := range misses {
		raven.CaptureMessage("Missed block", map[string]string{"height": strconv.FormatInt(miss.Height, 10), "time": miss.Time.String(), "address": miss.Address})

		// Mark miss as alerted in the db
		miss.Alerted = true
		_, err = m.db.Model(miss).Where("id = ?", miss.ID).Update()
		if err != nil {
			return err
		}

		fmt.Printf("alerted on miss #height: %d\n", miss.Height)
	}

	return nil
}

// AlertGovernance queries active governance proposals from the database and sends the relevant alert to sentry
func (m *Monitor) AlertGovernance() error {
	// Query proposals from the DB
	var proposals []*types.Proposal
	err := m.db.Model(&types.Proposal{}).
		Where("alerted = FALSE and proposal_status = ?", "Active").
		Select(&proposals)
	if err != nil {
		return err
	}

	// Send alerts for every proposal
	for _, proposal := range proposals {
		raven.CaptureMessage(fmt.Sprintf("New governance proposal: %s\nDescription: %s\nStartHeight: %s", proposal.Title, proposal.Description, proposal.VotingStartBlock),
			map[string]string{
				"height": strconv.FormatInt(proposal.Height, 10),
				"type":   proposal.Type,
			})

		// Mark proposal as alerted in the db
		proposal.Alerted = true
		_, err = m.db.Model(proposal).Where("id = ?", proposal.ID).Update()
		if err != nil {
			return err
		}

		fmt.Printf("alerted on proposal #%s\n", proposal.ID)
	}

	return nil
}
