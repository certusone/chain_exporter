package main

import (
	"fmt"
	"github.com/go-pg/pg"
	"github.com/go-pg/pg/orm"
	"github.com/pkg/errors"
	"github.com/tendermint/tendermint/libs/flowrate"
	"github.com/tendermint/tendermint/p2p/conn"
	"github.com/tendermint/tendermint/rpc/client"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type (
	Monitor struct {
		db      *pg.DB
		clients map[string]*client.HTTP
	}

	PeerInfo struct {
		ID        int64
		Timestamp time.Time
		Node      string

		PeerID     string `json:"id"`
		ListenAddr string `json:"listen_addr"`
		Network    string `json:"network"`
		Version    string `json:"version"`
		Channels   string `json:"channels"`
		Moniker    string `json:"moniker"`
		IsOutbound bool   `json:"is_outbound"`

		SendData    flowrate.Status
		RecvData    flowrate.Status
		ChannelData []conn.ChannelStatus
	}
)

func main() {
	if os.Getenv("GAIA_URLS") == "" {
		panic(errors.New("GAIA_URLS needs to be set"))
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
	if os.Getenv("PERIOD") == "" {
		panic(errors.New("PERIOD needs to be set"))
	}
	if _, err := strconv.Atoi(os.Getenv("PERIOD")); err != nil {
		panic(errors.New("PERIOD needs to be a number"))
	}

	clients := make(map[string]*client.HTTP)
	for _, item := range strings.Split(os.Getenv("GAIA_URLS"), ",") {
		tClient := client.NewHTTP(item, "/websocket")

		hostname, err := url.Parse(item)
		if err != nil {
			panic(err)
		}
		clients[hostname.Host] = tClient
	}

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
	monitor := &Monitor{db, clients}
	period, _ := strconv.Atoi(os.Getenv("PERIOD"))
	for {
		select {
		case <-time.Tick(time.Duration(period) * time.Second):
			err := monitor.sync()
			if err != nil {
				fmt.Printf("error parsing governance: %v\n", err)
			}
		}
	}
}

func createSchema(db *pg.DB) error {
	for _, model := range []interface{}{(*PeerInfo)(nil)} {
		err := db.CreateTable(model, &orm.CreateTableOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Monitor) sync() error {
	for name, cl := range m.clients {
		go func() {
			err := m.captureNetData(cl, name)
			if err != nil {
				fmt.Printf("error parsing netData: %v\n", err)
			}
		}()
	}
	return nil
}

func (m *Monitor) captureNetData(client *client.HTTP, name string) error {
	// Get Data
	netInfo, err := client.NetInfo()
	if err != nil {
		return err
	}

	timestamp := time.Now()
	for _, peer := range netInfo.Peers {
		data := &PeerInfo{}
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

		_, err = m.db.Model(data).Insert()
		if err != nil {
			fmt.Printf("error inserting netData: %v\n", err)
		}
	}

	return nil
}
