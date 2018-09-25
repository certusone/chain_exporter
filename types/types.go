package types

import (
	"time"
)

type (
	BlockInfo struct {
		ID       string
		Height   int64
		Proposer string
		Time     time.Time
	}

	EvidenceInfo struct {
		Address string
		Height  int64
	}

	MissInfo struct {
		ID       int64
		Address  string
		Height   int64
		Alerted  bool `sql:",default:false,notnull"`
		Proposer string
		Time     time.Time
	}

	Proposal struct {
		ID               string
		Type             string `json:"type"`
		Height           int64
		Alerted          bool `sql:",default:false,notnull"`
		Title            string
		Description      string
		ProposalType     string
		ProposalStatus   string
		VotingStartBlock string
		Details          struct {
			ProposalID       string `json:"proposal_id"`
			Title            string `json:"title"`
			Description      string `json:"description"`
			ProposalType     string `json:"proposal_type"`
			ProposalStatus   string `json:"proposal_status"`
			SubmitBlock      int64  `json:"submit_block,string"`
			VotingStartBlock string `json:"voting_start_block"`
		} `json:"value"`
	}
)
