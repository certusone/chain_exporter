package main

import (
	"fmt"
	"github.com/certusone/chain_exporter/types"
	"github.com/go-pg/pg"
	"github.com/go-pg/pg/orm"
	"github.com/pkg/errors"
	"github.com/tendermint/tendermint/rpc/client"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"
)

type (
	Monitor struct {
		db      *pg.DB
		clients map[string]*client.HTTP
	}
)

func main() {
	if os.Getenv("GAIA_URLS") == "" {
		panic(errors.New("GAIA_URLS needs to be set"))
	}
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
	if os.Getenv("PERIOD") == "" {
		panic(errors.New("PERIOD needs to be set"))
	}
	if _, err := strconv.Atoi(os.Getenv("PERIOD")); err != nil {
		panic(errors.New("PERIOD needs to be a number"))
	}

	// Setup the RPC clients
	clients := make(map[string]*client.HTTP)
	for _, item := range strings.Split(os.Getenv("GAIA_URLS"), ",") {
		tClient := client.NewHTTP(item, "/websocket")

		hostname, err := url.Parse(item)
		if err != nil {
			panic(err)
		}
		clients[hostname.Host] = tClient
	}

	// Connect to the postgres datastore
	db := pg.Connect(&pg.Options{
		Addr:     os.Getenv("DB_HOST"),
		Database: os.Getenv("DB_NAME"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PW"),
	})
	defer db.Close()

	// Setup the database and ignore errors if the schema already exists
	err := CreateSchema(db)
	if err != nil {
		panic(err)
	}

	// Setup monitor
	monitor := &Monitor{db, clients}
	// Parse query period
	period, _ := strconv.Atoi(os.Getenv("PERIOD"))

	// Allow graceful closing of the process
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt)

	// Start the periodic syncing
	for {
		select {
		case <-time.Tick(time.Duration(period) * time.Second):
			monitor.Sync()
		case <-signalCh:
			return
		}
	}
}

// CreateSchema sets up the database using the ORM
func CreateSchema(db *pg.DB) error {
	for _, model := range []interface{}{(*types.PeerInfo)(nil)} {
		err := db.CreateTable(model, &orm.CreateTableOptions{IfNotExists: true})
		if err != nil {
			return err
		}
	}
	return nil
}

// Sync queries and stores the netdata for each node listed
func (m *Monitor) Sync() {
	for name := range m.clients {
		go func(n string, client *client.HTTP) {
			err := m.CaptureNetData(client, n)
			if err != nil {
				fmt.Printf("error parsing netData for %s: %v\n", name, err)
				return
			}
			fmt.Printf("parsed netData for %s\n", name)

		}(name, m.clients[name])
	}
}

// CaptureNetData queries a node's net_info and stores the information for each peer in the db
func (m *Monitor) CaptureNetData(client *client.HTTP, name string) error {
	// Get Data
	netInfo, err := client.NetInfo()
	if err != nil {
		return err
	}

	// Use one timestamp to allow grouping
	timestamp := time.Now()
	for _, peer := range netInfo.Peers {
		// Aggregate data
		data := &types.PeerInfo{}
		data.Timestamp = timestamp
		data.Node = name

		data.Channels = peer.Channels.String()
		data.PeerID = string(peer.ID)
		data.ListenAddr = peer.ListenAddr
		data.Network = peer.Network
		data.Version = peer.Version
		data.Moniker = peer.Moniker
		data.IsOutbound = peer.IsOutbound

		data.SendData = peer.ConnectionStatus.SendMonitor
		data.RecvData = peer.ConnectionStatus.RecvMonitor
		data.ChannelData = peer.ConnectionStatus.Channels

		// Store data in postgres
		_, err = m.db.Model(data).Insert()
		if err != nil {
			fmt.Printf("error inserting netData: %v\n", err)
		}
	}

	return nil
}
