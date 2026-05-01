package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	SourceReal      = "real"
	SourceYangJiBao = "yangjibao"
	SourceXiaoBei   = "xiaobei"
)

type RealCredential struct {
	UserID       string `json:"user_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

type YangJiBaoCredential struct {
	Token string `json:"token"`
}

type XiaoBeiCredential struct {
	AccessToken string `json:"access_token"`
	UnionID     string `json:"union_id"`
}

type NormalizedAccount struct {
	Source            string
	ExternalAccountID string
	Name              string
}

type NormalizedHolding struct {
	Source            string
	ExternalAccountID string
	FundCode          string
	FundName          string
	Share             float64
	CostNav           float64
	Amount            float64
	OperationDate     string
	EstimatedShare    bool
}

type SyncInput struct {
	Source   string
	Accounts []NormalizedAccount
	Holdings []NormalizedHolding
}

func NewDeviceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("fundsync-%d", time.Now().UnixNano())
	}
	return "fundsync-" + hex.EncodeToString(b[:])
}
